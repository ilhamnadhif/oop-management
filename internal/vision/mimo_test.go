package vision

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The base URL decides where an API key is sent, so what it accepts is a
// security boundary rather than a convenience.
func TestNewMiMoClientConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		key     string
		baseURL string
		wantErr error
	}{
		{name: "missing key", baseURL: DefaultBaseURL, wantErr: ErrUnavailable},
		{name: "invalid url", key: "secret", baseURL: "://bad", wantErr: ErrUnavailable},
		{name: "insecure remote host", key: "secret", baseURL: "http://example.com/v1", wantErr: ErrUnavailable},
		{name: "userinfo rejected", key: "secret", baseURL: "https://secret@example.com/v1", wantErr: ErrUnavailable},
		{name: "query rejected", key: "secret", baseURL: "https://example.com/v1?target=other", wantErr: ErrUnavailable},
		{name: "fragment rejected", key: "secret", baseURL: "https://example.com/v1#part", wantErr: ErrUnavailable},
		{name: "https accepted", key: "secret", baseURL: "https://example.com/v1"},
		{name: "loopback http accepted", key: "secret", baseURL: "http://127.0.0.1:8080/v1"},
		{name: "localhost http accepted", key: "secret", baseURL: "http://localhost:8080/v1/"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, err := NewMiMoClient(test.key, test.baseURL, "", nil)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewMiMoClient() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr != nil {
				return
			}
			if client.model != DefaultModel {
				t.Fatalf("model = %q, want %q", client.model, DefaultModel)
			}
			if !strings.HasSuffix(client.endpoint, "/v1/chat/completions") {
				t.Fatalf("endpoint = %q", client.endpoint)
			}
			if client.client.Timeout != defaultRequestTimeout {
				t.Fatalf("timeout = %v, want %v", client.client.Timeout, defaultRequestTimeout)
			}
		})
	}
}

// A completion budget of zero would be sent upstream and come back truncated,
// which reads as a shorter document rather than as a failure.
func TestReadRefusesAnEmptyTokenBudget(t *testing.T) {
	t.Parallel()

	client, err := NewMiMoClient("secret", "https://example.com/v1", "", nil)
	if err != nil {
		t.Fatalf("NewMiMoClient: %v", err)
	}
	_, err = client.Read(t.Context(), Request{
		ImageDataURL: "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQH/wAALCAABAAEBAREA/8QAFAABAAAAAAAAAAAAAAAAAAAACf/EABQQAQAAAAAAAAAAAAAAAAAAAAD/2gAIAQEAAD8AKp//2Q==",
		SystemPrompt: "x",
		UserPrompt:   "y",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Read() error = %v, want %v", err, ErrInvalidInput)
	}
}

// The image is validated before anything is sent, so a malformed data URL never
// reaches the provider.
func TestValidateImageDataURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "empty", value: ""},
		{name: "no comma", value: "data:image/jpeg;base64"},
		{name: "empty payload", value: "data:image/jpeg;base64,"},
		{name: "wrong media type", value: "data:application/pdf;base64,QQ=="},
		{name: "not base64", value: "data:image/jpeg;base64,!!!!"},
		{name: "jpeg", value: "data:image/jpeg;base64,QUJD", valid: true},
		{name: "png", value: "data:image/png;base64,QUJD", valid: true},
		{name: "webp", value: "data:image/webp;base64,QUJD", valid: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateImageDataURL(test.value)
			if test.valid && err != nil {
				t.Fatalf("ValidateImageDataURL(%q) = %v, want nil", test.value, err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("ValidateImageDataURL(%q) = %v, want %v", test.value, err, ErrInvalidInput)
			}
		})
	}
}

// Model-authored JSON is read strictly: a field nobody asked for means the model
// invented structure, and reading around it stores a value that was never
// checked.
func TestDecodeStrictJSONRejectsUnknownFieldsAndTrailingValues(t *testing.T) {
	t.Parallel()

	type payload struct {
		Name string `json:"name"`
	}
	tests := []struct {
		name  string
		body  string
		valid bool
	}{
		{name: "clean", body: `{"name":"a"}`, valid: true},
		{name: "unknown field", body: `{"name":"a","extra":1}`},
		{name: "trailing value", body: `{"name":"a"} {"name":"b"}`},
		{name: "not json", body: `nope`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var into payload
			err := DecodeStrictJSON(strings.NewReader(test.body), &into)
			if test.valid != (err == nil) {
				t.Fatalf("DecodeStrictJSON(%q) = %v", test.body, err)
			}
		})
	}
}

// The envelope is read loosely: the provider may legitimately add metadata to
// its own wrapper, and refusing that would break the scan for a field nobody
// reads.
func TestDecodeLooseJSONAllowsUnknownEnvelopeFields(t *testing.T) {
	t.Parallel()

	var into struct {
		Name string `json:"name"`
	}
	if err := DecodeLooseJSON(strings.NewReader(`{"name":"a","usage":{"tokens":9}}`), &into); err != nil {
		t.Fatalf("DecodeLooseJSON: %v", err)
	}
	if into.Name != "a" {
		t.Fatalf("name = %q", into.Name)
	}
}

// The cap on a response belongs to the caller. A receipt answers in kilobytes;
// a page of ruled lines answers in far more, and a provider that echoes any of
// the request back sends the picture with it. One number fixed here would
// either starve the second caller or hand the first a limit it never needed.
func TestReadHonoursThePerRequestResponseCap(t *testing.T) {
	t.Parallel()

	// A completion whose envelope lands just over the default cap.
	filler := strings.Repeat("x", DefaultMaxResponseBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"finish_reason": "stop", "message": map[string]string{"content": `{"filler":"` + filler + `"}`}},
			},
		}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	defer server.Close()

	client, err := NewMiMoClient("secret", server.URL+"/v1", "m", server.Client())
	if err != nil {
		t.Fatalf("NewMiMoClient: %v", err)
	}
	request := Request{
		ImageDataURL: "data:image/jpeg;base64,QUJD",
		SystemPrompt: "s", UserPrompt: "u", MaxTokens: 100,
	}

	if _, err := client.Read(t.Context(), request); Reason(err) != "body-too-large" {
		t.Fatalf("default cap: Reason() = %q, want body-too-large", Reason(err))
	}

	request.MaxResponseBytes = 8 * DefaultMaxResponseBytes
	content, err := client.Read(t.Context(), request)
	if err != nil {
		t.Fatalf("raised cap: %v", err)
	}
	if len(content) < DefaultMaxResponseBytes {
		t.Fatalf("raised cap returned %d bytes", len(content))
	}
}

// A connection that fails part-way through is not an oversized answer. Naming
// them the same sent one diagnosis chasing a limit that was never the problem.
func TestReadNamesABrokenReadApartFromAnOversizedOne(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "4096")
		// Fewer bytes than promised, then the connection goes away: the client
		// reads an unexpected EOF part-way through the envelope.
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"{`))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		if hijacker, ok := w.(http.Hijacker); ok {
			conn, _, err := hijacker.Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}
	}))
	defer server.Close()

	client, err := NewMiMoClient("secret", server.URL+"/v1", "m", server.Client())
	if err != nil {
		t.Fatalf("NewMiMoClient: %v", err)
	}
	_, err = client.Read(t.Context(), Request{
		ImageDataURL: "data:image/jpeg;base64,QUJD",
		SystemPrompt: "s", UserPrompt: "u", MaxTokens: 100,
	})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Read() error = %v, want %v", err, ErrInvalidResponse)
	}
	if reason := Reason(err); reason != "body-read" {
		t.Fatalf("Reason() = %q, want body-read", reason)
	}
}

// A deadline that fires while the answer is still arriving is a timeout, not a
// broken answer. Reading a dense page takes far longer than reading a receipt,
// so this is the failure a sheet hits first, and it has to say so.
func TestReadReportsADeadlineDuringTheBodyAsATimeout(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// The envelope opens and then stalls, the way a model still writing its
		// answer does.
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"`))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	httpClient := server.Client()
	httpClient.Timeout = 150 * time.Millisecond
	client, err := NewMiMoClient("secret", server.URL+"/v1", "m", httpClient)
	if err != nil {
		t.Fatalf("NewMiMoClient: %v", err)
	}
	_, err = client.Read(t.Context(), Request{
		ImageDataURL: "data:image/jpeg;base64,QUJD",
		SystemPrompt: "s", UserPrompt: "u", MaxTokens: 100,
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Read() error = %v (reason %q), want %v", err, Reason(err), ErrTimeout)
	}
}
