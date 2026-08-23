package vision

import (
	"errors"
	"strings"
	"testing"
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
