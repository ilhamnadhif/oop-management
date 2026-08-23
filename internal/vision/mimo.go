package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxImageDataURLBytes  = 4 << 20
	defaultRequestTimeout = 25 * time.Second
	retryDelay            = 100 * time.Millisecond
)

// Request is one image to read and the instructions for reading it. The prompts
// belong to the caller: this package knows how to send a picture, not what is
// in it.
type Request struct {
	ImageDataURL string
	SystemPrompt string
	UserPrompt   string
	// MaxTokens bounds the completion. A caller reading a dense table needs far
	// more room than one reading a receipt, so the budget is set per call rather
	// than fixed here.
	MaxTokens int
}

// Client calls Xiaomi MiMo's OpenAI-compatible chat completions API.
type Client struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

// NewMiMoClient creates a client. Empty baseURL and model values use the
// official defaults. HTTP is accepted only for loopback hosts so local tests do
// not weaken production transport security.
func NewMiMoClient(apiKey, baseURL, model string, client *http.Client) (*Client, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, ErrUnavailable
	}

	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	endpoint, err := chatCompletionsEndpoint(baseURL)
	if err != nil {
		return nil, ErrUnavailable
	}

	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultModel
	}

	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimeout}
	}
	clientCopy := *client
	if clientCopy.Timeout <= 0 {
		clientCopy.Timeout = defaultRequestTimeout
	}
	// Never forward the non-standard api-key header to a redirected host.
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &Client{
		apiKey:   apiKey,
		model:    model,
		endpoint: endpoint,
		client:   &clientCopy,
	}, nil
}

type chatRequest struct {
	Model               string           `json:"model"`
	Messages            []requestMessage `json:"messages"`
	ResponseFormat      responseFormat   `json:"response_format"`
	Thinking            thinkingConfig   `json:"thinking"`
	MaxCompletionTokens int              `json:"max_completion_tokens"`
	Stream              bool             `json:"stream"`
}

type requestMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type contentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *imageURLPart `json:"image_url,omitempty"`
}

type imageURLPart struct {
	URL string `json:"url"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type thinkingConfig struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Read sends one image and returns the JSON the model wrote, unvalidated beyond
// being a single non-empty completion of a bounded size. What that JSON has to
// contain is the caller's business.
func (c *Client) Read(ctx context.Context, request Request) ([]byte, error) {
	if c == nil || c.client == nil || c.apiKey == "" || c.endpoint == "" || c.model == "" {
		return nil, ErrUnavailable
	}
	if err := ValidateImageDataURL(request.ImageDataURL); err != nil {
		return nil, ErrInvalidInput
	}
	if request.MaxTokens <= 0 {
		return nil, ErrInvalidInput
	}
	// The timeout covers the whole read, including a retry and its backoff. A
	// per-request timeout alone could let two slow attempts run past the web
	// server's response deadline.
	readContext, cancel := context.WithTimeout(ctx, c.client.Timeout)
	defer cancel()

	payload, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []requestMessage{
			{Role: "system", Content: request.SystemPrompt},
			{
				Role: "user",
				Content: []contentPart{
					{Type: "image_url", ImageURL: &imageURLPart{URL: request.ImageDataURL}},
					{Type: "text", Text: request.UserPrompt},
				},
			},
		},
		ResponseFormat:      responseFormat{Type: "json_object"},
		Thinking:            thinkingConfig{Type: "disabled"},
		MaxCompletionTokens: request.MaxTokens,
		Stream:              false,
	})
	if err != nil {
		return nil, ErrInvalidInput
	}

	for attempt := 0; attempt < 2; attempt++ {
		responseBody, statusCode, err := c.request(readContext, payload)
		if err != nil {
			return nil, err
		}
		if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
			return completionContent(responseBody)
		}

		if isRetryableStatus(statusCode) && attempt == 0 {
			if err := waitForRetry(readContext); err != nil {
				return nil, err
			}
			continue
		}
		return nil, classifyStatus(statusCode)
	}

	return nil, ErrUnavailable
}

func (c *Client) request(ctx context.Context, payload []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("api-key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, classifyTransportError(ctx, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, resp.StatusCode, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil || len(body) > MaxResponseBytes {
		return nil, resp.StatusCode, Reasoned(ErrInvalidResponse, "body-too-large")
	}
	return body, resp.StatusCode, nil
}

// completionContent unwraps the OpenAI-compatible envelope. A completion that
// stopped for any reason other than finishing is refused rather than parsed:
// a table cut off at the token limit reads as a shorter table, not as an error.
func completionContent(body []byte) ([]byte, error) {
	var envelope chatResponse
	if err := DecodeLooseJSON(bytes.NewReader(body), &envelope); err != nil || len(envelope.Choices) == 0 {
		return nil, Reasoned(ErrInvalidResponse, "envelope")
	}
	choice := envelope.Choices[0]
	if choice.FinishReason != "stop" {
		// "length" here means the answer was cut off at the token budget. It is
		// the likeliest failure on a dense page, and it must never be parsed:
		// a truncated table reads as a shorter table.
		return nil, Reasoned(ErrInvalidResponse, "finish-"+finishKeyword(choice.FinishReason))
	}
	content := strings.TrimSpace(choice.Message.Content)
	if content == "" {
		return nil, Reasoned(ErrInvalidResponse, "empty-content")
	}
	if len(content) > MaxResponseBytes {
		return nil, Reasoned(ErrInvalidResponse, "content-too-large")
	}
	return []byte(content), nil
}

// finishKeyword maps the provider's finish_reason onto a word this package
// chose, so an unrecognised value cannot smuggle text into a log line.
func finishKeyword(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "length":
		return "length"
	case "content_filter":
		return "filtered"
	case "tool_calls", "function_call":
		return "tool-call"
	case "":
		return "missing"
	default:
		return "other"
	}
}

func chatCompletionsEndpoint(baseURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || !u.IsAbs() || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrUnavailable
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return "", ErrUnavailable
	}

	u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	u.RawPath = ""
	return u.String(), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ValidateImageDataURL checks the shape and decodability of an image data URL
// before any of it is sent anywhere.
func ValidateImageDataURL(value string) error {
	if value == "" || len(value) > maxImageDataURLBytes {
		return ErrInvalidInput
	}
	comma := strings.IndexByte(value, ',')
	if comma < 0 || comma == len(value)-1 {
		return ErrInvalidInput
	}
	prefix := strings.ToLower(value[:comma])
	switch prefix {
	case "data:image/jpeg;base64", "data:image/png;base64", "data:image/webp;base64":
	default:
		return ErrInvalidInput
	}

	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(value[comma+1:]))
	decoded, err := io.Copy(io.Discard, decoder)
	if err != nil || decoded == 0 {
		return ErrInvalidInput
	}
	return nil
}

func isRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode == http.StatusInternalServerError || statusCode == http.StatusServiceUnavailable
}

func waitForRetry(ctx context.Context) error {
	timer := time.NewTimer(retryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		if err := contextError(ctx, ctx.Err()); err != nil {
			return err
		}
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func classifyStatus(statusCode int) error {
	kind := ErrUpstream
	switch statusCode {
	case http.StatusTooManyRequests:
		kind = ErrRateLimited
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusNotFound, 421,
		http.StatusInternalServerError, http.StatusServiceUnavailable:
		kind = ErrUnavailable
	}
	return &UpstreamError{StatusCode: statusCode, kind: kind}
}

func classifyTransportError(ctx context.Context, err error) error {
	if translated := contextError(ctx, err); translated != nil {
		return translated
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return ErrTimeout
	}
	return ErrUnavailable
}
