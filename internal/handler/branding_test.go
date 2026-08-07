package handler

import (
	"net/http"
	"strings"
	"testing"
)

// static/img was not covered by the original //go:embed patterns, and embed
// does not recurse, so a missing pattern ships pages with a broken logo.
func TestBrandAssetsAreServed(t *testing.T) {
	testServer := newTestServer(t)

	for _, asset := range []struct {
		path        string
		contentType string
	}{
		{"/static/img/opp-logo.png", "image/png"},
		{"/static/img/favicon.ico", "image/"},
	} {
		response, err := http.Get(testServer.URL + asset.path)
		if err != nil {
			t.Fatalf("get %s: %v", asset.path, err)
		}
		body := readBody(t, response)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("get %s: status %d", asset.path, response.StatusCode)
		}
		if len(body) == 0 {
			t.Fatalf("get %s: empty body", asset.path)
		}
		if got := response.Header.Get("Content-Type"); !strings.Contains(got, asset.contentType) {
			t.Fatalf("get %s: content type %q, want %q", asset.path, got, asset.contentType)
		}
	}
}

func TestPagesReferenceBrandAssets(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	pages := map[string]string{
		"login":     fetchPage(t, testServer.URL+"/login"),
		"register":  fetchPage(t, testServer.URL+"/register"),
		"dashboard": fetchDashboard(t, client, testServer),
	}

	for name, page := range pages {
		for _, fragment := range []string{
			`href="/static/img/favicon.ico"`,
			`src="/static/img/opp-logo.png"`,
		} {
			if !strings.Contains(page, fragment) {
				t.Fatalf("%s page missing %q", name, fragment)
			}
		}
		// Intrinsic dimensions keep the header from reflowing once the logo
		// finishes loading.
		if !strings.Contains(page, `width="486" height="440"`) {
			t.Fatalf("%s page logo has no intrinsic size", name)
		}
	}
}
