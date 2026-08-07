package handler

import (
	"net/http"
	"strings"
	"testing"
)

func fetchPage(t *testing.T, url string) string {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get %s: status %d", url, response.StatusCode)
	}
	return body
}

// tagAt returns the opening tag that encloses the given marker, so attribute
// assertions cannot accidentally match a different element on the page.
func tagAt(t *testing.T, page, marker string) string {
	t.Helper()
	index := strings.Index(page, marker)
	if index < 0 {
		t.Fatalf("marker %q not found", marker)
	}
	start := strings.LastIndex(page[:index], "<")
	end := strings.Index(page[start:], ">")
	if start < 0 || end < 0 {
		t.Fatalf("malformed tag around %q", marker)
	}
	return page[start : start+end]
}

func TestLoginPageUXAffordances(t *testing.T) {
	testServer := newTestServer(t)
	page := fetchPage(t, testServer.URL+"/login")

	for _, fragment := range []string{
		`placeholder="123456 atau nama@perusahaan.com"`,
		`placeholder="Masukkan password"`,
		`id="rememberMe"`,
		`data-password-toggle="password"`,
		`data-submit-button`,
		`class="spinner"`,
		`/static/js/auth.js`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("login page missing %q", fragment)
		}
	}
}

// A button inside a form defaults to type="submit". If an eye toggle ever loses
// its explicit type, pressing Enter would reveal the password instead of
// submitting the form.
func TestPasswordTogglesAreNotSubmitButtons(t *testing.T) {
	testServer := newTestServer(t)

	for _, page := range []string{
		fetchPage(t, testServer.URL+"/login"),
		fetchPage(t, testServer.URL+"/register"),
	} {
		count := strings.Count(page, "data-password-toggle=")
		if count == 0 {
			t.Fatal("page has no password toggle")
		}
		rest := page
		for i := 0; i < count; i++ {
			index := strings.Index(rest, "data-password-toggle=")
			tag := tagAt(t, rest, "data-password-toggle=")
			if !strings.Contains(tag, `type="button"`) {
				t.Fatalf("password toggle must be type=button, got: %s", tag)
			}
			rest = rest[index+len("data-password-toggle="):]
		}

		// The real submit button must stay a submit button so Enter keeps working.
		if !strings.Contains(tagAt(t, page, "data-submit-button"), `type="submit"`) {
			t.Fatal("submit button is no longer type=submit")
		}
	}
}

func TestRegisterPageUXAffordances(t *testing.T) {
	testServer := newTestServer(t)
	page := fetchPage(t, testServer.URL+"/register")

	for _, fragment := range []string{
		`placeholder="Budi Santoso"`,
		`placeholder="123456"`,
		`placeholder="nama@perusahaan.com"`,
		`placeholder="Minimal 8 karakter"`,
		`placeholder="Ulangi password"`,
		`data-confirm-for="register-password"`,
		`data-digits-only`,
		`inputmode="numeric"`,
		`data-submit-button`,
		`/static/js/auth.js`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("register page missing %q", fragment)
		}
	}

	// The confirmation field must never be posted; only the real password is.
	if strings.Contains(tagAt(t, page, `id="register-password-confirm"`), "name=") {
		t.Fatal("password confirmation field must not have a name attribute")
	}
}

func TestAuthScriptIsServed(t *testing.T) {
	testServer := newTestServer(t)
	body := fetchPage(t, testServer.URL+"/static/js/auth.js")
	if len(body) == 0 {
		t.Fatal("auth.js is empty")
	}
}
