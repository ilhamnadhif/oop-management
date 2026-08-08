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
		{"/static/fonts/inter-latin-variable.woff2", "font/woff2"},
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

// The CSP allows same-origin assets only, so Inter has to be self-hosted and
// declared in the stylesheet. A Google Fonts link would be blocked silently and
// every page would quietly fall back to the system font.
func TestStylesheetSelfHostsInter(t *testing.T) {
	testServer := newTestServer(t)
	stylesheet := fetchPage(t, testServer.URL+"/static/css/style.css")

	for _, fragment := range []string{
		"@font-face",
		`font-family: "Inter"`,
		`url("/static/fonts/inter-latin-variable.woff2")`,
		"font-weight: 100 900",
		"font-display: swap",
	} {
		if !strings.Contains(stylesheet, fragment) {
			t.Fatalf("stylesheet missing %q", fragment)
		}
	}
	if strings.Contains(stylesheet, "fonts.googleapis.com") || strings.Contains(stylesheet, "fonts.gstatic.com") {
		t.Fatal("stylesheet points at Google Fonts, which the CSP blocks")
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
