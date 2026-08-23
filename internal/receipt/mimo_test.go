package receipt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"opp-management/internal/vision"
	"time"
	"unicode/utf8"
)

// The endpoint, model and timeout are the transport's to get right and are
// tested there. What this package owns is that its constructor refuses the same
// configurations rather than handing back a scanner that fails later.
func TestNewMiMoScannerRefusesUnsafeConfiguration(t *testing.T) {
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
		{name: "https accepted", key: "secret", baseURL: "https://example.com/v1"},
		{name: "loopback http accepted", key: "secret", baseURL: "http://127.0.0.1:8080/v1"},
		{name: "localhost http accepted", key: "secret", baseURL: "http://localhost:8080/v1/"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			scanner, err := NewMiMoScanner(test.key, test.baseURL, "", nil)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewMiMoScanner() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil && scanner == nil {
				t.Fatal("a valid configuration produced no scanner")
			}
		})
	}
}

// typeField reads the "type" member the request uses for response_format and
// thinking. The wire shape is asserted here rather than through the transport's
// own structs: this test is about what a receipt scan puts on the wire.
type typeField struct {
	Type string `json:"type"`
}

type testContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

func TestMiMoScannerScanSendsExpectedRequestAndValidatesResult(t *testing.T) {
	t.Parallel()

	image := testImageDataURL()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("api-key"); got != "top-secret" {
			t.Errorf("api-key = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}

		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
			ResponseFormat      typeField `json:"response_format"`
			Thinking            typeField `json:"thinking"`
			MaxCompletionTokens int       `json:"max_completion_tokens"`
			Stream              bool      `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if payload.Model != "mimo-v2.5-test" {
			t.Errorf("model = %q", payload.Model)
		}
		if payload.ResponseFormat.Type != "json_object" {
			t.Errorf("response_format = %#v", payload.ResponseFormat)
		}
		if payload.Thinking.Type != "disabled" {
			t.Errorf("thinking = %#v", payload.Thinking)
		}
		if payload.MaxCompletionTokens != 4096 {
			t.Errorf("max_completion_tokens = %d", payload.MaxCompletionTokens)
		}
		if payload.Stream {
			t.Error("stream must be false")
		}
		if len(payload.Messages) != 2 || payload.Messages[0].Role != "system" || payload.Messages[1].Role != "user" {
			t.Errorf("messages roles = %#v", payload.Messages)
			return
		}
		var system string
		if err := json.Unmarshal(payload.Messages[0].Content, &system); err != nil || !strings.Contains(system, "DATA TIDAK TERPERCAYA") {
			t.Errorf("system prompt missing injection guard: %v, %q", err, system)
		}
		var parts []testContentPart
		if err := json.Unmarshal(payload.Messages[1].Content, &parts); err != nil {
			t.Errorf("decode content parts: %v", err)
			return
		}
		if len(parts) != 2 || parts[0].Type != "image_url" || parts[0].ImageURL == nil || parts[0].ImageURL.URL != image || parts[1].Type != "text" {
			t.Errorf("content parts = %#v", parts)
		}

		writeCompletion(t, w, `{"items":[{"nama_produk":"  Kopi Arabika  ","satuan":"","volume":2,"harga":1250.4},{"nama_produk":"Gula","satuan":"kg","volume":1.5,"harga":20000}],"total_terbaca":32500.4,"warnings":[" cek harga ","","`+strings.Repeat("x", 300)+`"]}`)
	}))
	defer server.Close()

	scanner, err := NewMiMoScanner(" top-secret ", server.URL+"/v1/", "mimo-v2.5-test", server.Client())
	if err != nil {
		t.Fatalf("NewMiMoScanner: %v", err)
	}
	result, err := scanner.Scan(context.Background(), image)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %#v", result.Items)
	}
	if result.Items[0] != (Item{NamaProduk: "Kopi Arabika", Satuan: "pcs", Volume: 2, Harga: 1250}) {
		t.Errorf("first item = %#v", result.Items[0])
	}
	if result.Items[1] != (Item{NamaProduk: "Gula", Satuan: "kg", Volume: 1.5, Harga: 20000}) {
		t.Errorf("second item = %#v", result.Items[1])
	}
	if result.TotalTerbaca == nil || *result.TotalTerbaca != 32500 {
		t.Errorf("total_terbaca = %v", result.TotalTerbaca)
	}
	if len(result.Warnings) != 2 || result.Warnings[0] != "cek harga" || utf8.RuneCountInString(result.Warnings[1]) != maxWarningRunes {
		t.Errorf("warnings = %#v", result.Warnings)
	}
}

func TestMiMoScannerAddsPrioritizedTotalMismatchWarning(t *testing.T) {
	t.Parallel()

	warnings := make([]string, maxWarnings)
	for index := range warnings {
		warnings[index] = fmt.Sprintf("Peringatan model %d", index+1)
	}
	content, err := json.Marshal(map[string]any{
		"items": []map[string]any{
			{"nama_produk": "Kopi", "satuan": "pcs", "volume": 2, "harga": 10000},
			{"nama_produk": "Gula", "satuan": "kg", "volume": 1, "harga": 5000},
		},
		"total_terbaca": 30000,
		"warnings":      warnings,
	})
	if err != nil {
		t.Fatal(err)
	}

	server := completionServer(t, func() string { return string(content) })
	defer server.Close()
	result, err := mustScanner(t, server).Scan(context.Background(), testImageDataURL())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Warnings) != maxWarnings {
		t.Fatalf("warnings = %#v; want exactly %d capped warnings", result.Warnings, maxWarnings)
	}
	if got := result.Warnings[0]; !strings.Contains(got, "Total struk terbaca Rp30000") || !strings.Contains(got, "jumlah rincian Rp25000") {
		t.Fatalf("first warning = %q; total mismatch warning must be clear and prioritized", got)
	}
	if utf8.RuneCountInString(result.Warnings[0]) > maxWarningRunes {
		t.Fatalf("warning length = %d, want <= %d", utf8.RuneCountInString(result.Warnings[0]), maxWarningRunes)
	}
}

func TestMiMoScannerTotalReconciliationMateriality(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		total       int
		wantWarning bool
	}{
		{name: "exact match", total: 10000},
		{name: "below minimum and ratio", total: 10099},
		{name: "at one percent", total: 10102, wantWarning: true},
		{name: "material mismatch", total: 12000, wantWarning: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			content := fmt.Sprintf(`{"items":[{"nama_produk":"Kopi","satuan":"pcs","volume":1,"harga":10000}],"total_terbaca":%d}`, test.total)
			server := completionServer(t, func() string { return content })
			defer server.Close()
			result, err := mustScanner(t, server).Scan(context.Background(), testImageDataURL())
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			gotWarning := len(result.Warnings) > 0
			if gotWarning != test.wantWarning {
				t.Fatalf("warnings = %#v, wantWarning = %t", result.Warnings, test.wantWarning)
			}
		})
	}
}

func TestMiMoScannerStrictResultValidation(t *testing.T) {
	t.Parallel()

	fiftyOneItems := make([]map[string]any, 51)
	for index := range fiftyOneItems {
		fiftyOneItems[index] = map[string]any{"nama_produk": fmt.Sprintf("Item %d", index), "satuan": "pcs", "volume": 1, "harga": 1000}
	}
	fiftyOneJSON, err := json.Marshal(map[string]any{"items": fiftyOneItems})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		content string
		wantErr error
	}{
		{name: "no items", content: `{"items":[]}`, wantErr: ErrNoItems},
		{name: "too many items", content: string(fiftyOneJSON), wantErr: ErrInvalidResponse},
		{name: "blank name", content: `{"items":[{"nama_produk":" ","satuan":"pcs","volume":1,"harga":1}]}`, wantErr: ErrInvalidResponse},
		{name: "missing price", content: `{"items":[{"nama_produk":"Kopi","satuan":"pcs","volume":1}]}`, wantErr: ErrInvalidResponse},
		{name: "zero volume", content: `{"items":[{"nama_produk":"Kopi","satuan":"pcs","volume":0,"harga":1}]}`, wantErr: ErrInvalidResponse},
		{name: "unsafe volume", content: `{"items":[{"nama_produk":"Kopi","satuan":"pcs","volume":10000000000000000,"harga":1}]}`, wantErr: ErrInvalidResponse},
		{name: "negative price", content: `{"items":[{"nama_produk":"Kopi","satuan":"pcs","volume":1,"harga":-1}]}`, wantErr: ErrInvalidResponse},
		{name: "unsafe subtotal", content: `{"items":[{"nama_produk":"Kopi","satuan":"pcs","volume":1000000,"harga":1000000000000}]}`, wantErr: ErrInvalidResponse},
		{name: "unknown item field", content: `{"items":[{"nama_produk":"Kopi","satuan":"pcs","volume":1,"harga":1,"subtotal":1}]}`, wantErr: ErrInvalidResponse},
		{name: "trailing JSON", content: `{"items":[{"nama_produk":"Kopi","satuan":"pcs","volume":1,"harga":1}]} {}`, wantErr: ErrInvalidResponse},
		{name: "invalid total", content: `{"items":[{"nama_produk":"Kopi","satuan":"pcs","volume":1,"harga":1}],"total_terbaca":-1}`, wantErr: ErrInvalidResponse},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := completionServer(t, func() string { return test.content })
			defer server.Close()
			scanner := mustScanner(t, server)
			_, err := scanner.Scan(context.Background(), testImageDataURL())
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Scan() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestMiMoScannerRetriesOnlyRetryableStatuses(t *testing.T) {
	statuses := []struct {
		name         string
		status       int
		wantErr      error
		wantRequests int32
	}{
		{name: "rate limit", status: http.StatusTooManyRequests, wantErr: ErrRateLimited, wantRequests: 2},
		{name: "server error", status: http.StatusInternalServerError, wantErr: ErrUnavailable, wantRequests: 2},
		{name: "service unavailable", status: http.StatusServiceUnavailable, wantErr: ErrUnavailable, wantRequests: 2},
		{name: "unauthorized", status: http.StatusUnauthorized, wantErr: ErrUnavailable, wantRequests: 1},
		{name: "bad request", status: http.StatusBadRequest, wantErr: ErrUpstream, wantRequests: 1},
	}

	for _, test := range statuses {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				http.Error(w, "sensitive upstream body", test.status)
			}))
			defer server.Close()

			scanner := mustScanner(t, server)
			_, err := scanner.Scan(context.Background(), testImageDataURL())
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Scan() error = %v, want %v", err, test.wantErr)
			}
			if strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("error leaked upstream body: %v", err)
			}
			if got := requests.Load(); got != test.wantRequests {
				t.Fatalf("requests = %d, want %d", got, test.wantRequests)
			}
			var upstreamError *UpstreamError
			if !errors.As(err, &upstreamError) || upstreamError.StatusCode != test.status {
				t.Fatalf("UpstreamError = %#v", upstreamError)
			}
		})
	}
}

func TestMiMoScannerRetriesThenSucceeds(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		writeCompletion(t, w, `{"items":[{"nama_produk":"Kopi","satuan":"pcs","volume":1,"harga":1000}]}`)
	}))
	defer server.Close()

	result, err := mustScanner(t, server).Scan(context.Background(), testImageDataURL())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if requests.Load() != 2 || len(result.Items) != 1 {
		t.Fatalf("requests = %d, result = %#v", requests.Load(), result)
	}
}

func TestMiMoScannerTimeoutIsSafe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(250 * time.Millisecond):
		}
	}))
	defer server.Close()

	client := server.Client()
	client.Timeout = 30 * time.Millisecond
	scanner, err := NewMiMoScanner("secret", server.URL, DefaultModel, client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = scanner.Scan(context.Background(), testImageDataURL())
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Scan() error = %v, want ErrTimeout", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("timeout error leaked configuration: %v", err)
	}
}

func TestMiMoScannerTimeoutCoversRetryBackoff(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		time.Sleep(20 * time.Millisecond)
		http.Error(w, "retry later", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := server.Client()
	client.Timeout = 60 * time.Millisecond
	scanner, err := NewMiMoScanner("secret", server.URL, DefaultModel, client)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = scanner.Scan(context.Background(), testImageDataURL())
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Scan() error = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("Scan() took %v; timeout did not cover retry backoff", elapsed)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1 before the overall timeout", got)
	}
}

func TestMiMoScannerRejectsOversizedResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", vision.MaxResponseBytes+1)))
	}))
	defer server.Close()

	_, err := mustScanner(t, server).Scan(context.Background(), testImageDataURL())
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Scan() error = %v, want ErrInvalidResponse", err)
	}
}

func TestMiMoScannerRejectsMalformedCompletionEnvelope(t *testing.T) {
	t.Parallel()

	responses := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{`},
		{name: "trailing JSON", body: `{"choices":[]} {}`},
		{name: "missing choices", body: `{"id":"completion-id"}`},
		{name: "empty choices", body: `{"choices":[]}`},
		{name: "missing finish reason", body: `{"choices":[{"message":{"content":"{\\"items\\":[]}"}}]}`},
		{name: "null finish reason", body: `{"choices":[{"finish_reason":null,"message":{"content":"{\\"items\\":[]}"}}]}`},
		{name: "length finish reason", body: `{"choices":[{"finish_reason":"length","message":{"content":"{\\"items\\":[]}"}}]}`},
		{name: "content filter finish reason", body: `{"choices":[{"finish_reason":"content_filter","message":{"content":"{\\"items\\":[]}"}}]}`},
		{name: "empty content", body: `{"choices":[{"finish_reason":"stop","message":{"content":""}}]}`},
	}

	for _, response := range responses {
		t.Run(response.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(response.body))
			}))
			defer server.Close()

			_, err := mustScanner(t, server).Scan(context.Background(), testImageDataURL())
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("Scan() error = %v, want ErrInvalidResponse", err)
			}
		})
	}
}

func TestMiMoScannerRejectsInvalidInputWithoutCallingUpstream(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	scanner := mustScanner(t, server)

	invalidValues := []string{"", "https://example.com/receipt.jpg", "data:text/plain;base64,SGVsbG8=", "data:image/jpeg;base64,%%%"}
	for _, value := range invalidValues {
		if _, err := scanner.Scan(context.Background(), value); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("Scan(%q) error = %v, want ErrInvalidInput", value, err)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("upstream requests = %d, want 0", got)
	}
}

func TestMiMoScannerDoesNotFollowRedirect(t *testing.T) {
	t.Parallel()

	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client := source.Client()
	if client.CheckRedirect != nil {
		t.Fatal("test client unexpectedly has CheckRedirect")
	}
	scanner, err := NewMiMoScanner("secret", source.URL, DefaultModel, client)
	if err != nil {
		t.Fatal(err)
	}
	if client.CheckRedirect != nil {
		t.Fatal("constructor mutated caller's HTTP client")
	}
	_, err = scanner.Scan(context.Background(), testImageDataURL())
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("Scan() error = %v, want ErrUpstream", err)
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("redirect target requests = %d, want 0", got)
	}
}

func TestMiMoScannerPreservesContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(250 * time.Millisecond):
		}
	}))
	defer server.Close()
	scanner := mustScanner(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := scanner.Scan(ctx, testImageDataURL())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan() error = %v, want context.Canceled", err)
	}
}

func completionServer(t *testing.T, content func() string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeCompletion(t, w, content())
	}))
}

func writeCompletion(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"id":      "completion-id",
		"object":  "chat.completion",
		"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content, "reasoning_content": "ignored metadata"}}},
		"usage":   map[string]any{"completion_tokens": 42},
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		t.Errorf("encode completion: %v", err)
	}
}

func mustScanner(t *testing.T, server *httptest.Server) *MiMoScanner {
	t.Helper()
	scanner, err := NewMiMoScanner("secret", server.URL, DefaultModel, server.Client())
	if err != nil {
		t.Fatalf("NewMiMoScanner: %v", err)
	}
	return scanner
}

func testImageDataURL() string {
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("receipt image bytes"))
}
