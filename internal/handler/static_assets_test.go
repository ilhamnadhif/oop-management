package handler

import (
	"net/http"
	"testing"
)

// Embedded files have a zero modification time, so net/http sends no
// Last-Modified and no ETag for them. Without a validator a browser is free to
// keep serving a stale stylesheet after a deploy, which is exactly how a CSS
// change appears not to have shipped.
func TestStaticAssetsCarryAValidator(t *testing.T) {
	testServer := newTestServer(t)

	for _, path := range []string{
		"/static/css/style.css",
		"/static/js/auth.js",
		"/static/fonts/inter-latin-variable.woff2",
		"/static/img/opp-logo.png",
	} {
		response, err := http.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		response.Body.Close()

		etag := response.Header.Get("ETag")
		if etag == "" {
			t.Fatalf("%s: no ETag", path)
		}
		if got := response.Header.Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("%s: Cache-Control = %q, want no-cache", path, got)
		}

		// A matching validator must produce a cheap 304 rather than a resend.
		request, err := http.NewRequest(http.MethodGet, testServer.URL+path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		request.Header.Set("If-None-Match", etag)
		revalidated, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("revalidate %s: %v", path, err)
		}
		revalidated.Body.Close()
		if revalidated.StatusCode != http.StatusNotModified {
			t.Fatalf("%s: revalidation status %d, want 304", path, revalidated.StatusCode)
		}
	}
}

// Different files must not share a validator, or updating one would leave the
// others stale.
func TestStaticETagsDifferPerFile(t *testing.T) {
	testServer := newTestServer(t)

	seen := make(map[string]string)
	for _, path := range []string{"/static/css/style.css", "/static/js/auth.js", "/static/js/produksi.js"} {
		response, err := http.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		response.Body.Close()
		etag := response.Header.Get("ETag")
		if other, clash := seen[etag]; clash {
			t.Fatalf("%s and %s share the ETag %s", other, path, etag)
		}
		seen[etag] = path
	}
}
