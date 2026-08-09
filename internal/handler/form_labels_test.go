package handler

import (
	"strings"
	"testing"
)

// labelBodies returns every label that names a field, so each can be checked on
// its own.
func labelBodies(t *testing.T, page string) map[string]string {
	t.Helper()
	labels := map[string]string{}
	rest := page
	for {
		start := strings.Index(rest, `<label for="`)
		if start < 0 {
			return labels
		}
		rest = rest[start:]
		end := strings.Index(rest, "</label>")
		if end < 0 {
			t.Fatal("a label is never closed")
		}
		body := rest[:end]
		name := body[len(`<label for="`):]
		name = name[:strings.Index(name, `"`)]
		labels[name] = body
		rest = rest[end:]
	}
}

// Every field label carries an icon of what it asks for, so a long form can be
// scanned by shape rather than read line by line.
func TestFormLabelsCarryAnIcon(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	pages := map[string]string{
		"/nota":            fetchAuthedPage(t, client, testServer.URL+"/nota"),
		"/produksi":        fetchAuthedPage(t, client, testServer.URL+"/produksi"),
		"/unit-dt":         fetchAuthedPage(t, client, testServer.URL+"/unit-dt"),
		"/unit-a2b":        fetchAuthedPage(t, client, testServer.URL+"/unit-a2b"),
		"/nota/overview":   fetchAuthedPage(t, client, testServer.URL+"/nota/overview"),
		"/produksi/export": fetchAuthedPage(t, client, testServer.URL+"/produksi/export"),
		"/register":        fetchPage(t, testServer.URL+"/register"),
		"/login":           fetchPage(t, testServer.URL+"/login"),
	}
	for path, page := range pages {
		labels := labelBodies(t, page)
		if len(labels) == 0 {
			t.Fatalf("%s has no field labels", path)
		}
		for field, body := range labels {
			// The remember-me row is a checkbox beside its own words, not a
			// field caption.
			if strings.Contains(body, `class="checkbox-row"`) {
				continue
			}
			if !strings.Contains(body, `class="icon"`) {
				t.Fatalf("%s: the label for %q carries no icon: %s", path, field, body)
			}
		}
	}
}

// A hint that only restates the field is noise beside an icon that already says
// it; the ones that state a rule stay, as plain text rather than a tinted chip.
func TestLabelsDropTheRestatingHints(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	for path, gone := range map[string][]string{
		"/nota":     {"(otomatis)", "(penanggung jawab)"},
		"/unit-dt":  {"(otomatis)"},
		"/unit-a2b": {"(otomatis)"},
		"/produksi": {"(hanya unit terdaftar)"},
	} {
		page := fetchAuthedPage(t, client, testServer.URL+path)
		for _, hint := range gone {
			if strings.Contains(page, hint) {
				t.Fatalf("%s still shows the hint %q", path, hint)
			}
		}
	}

	// The rules survive.
	produksi := fetchAuthedPage(t, client, testServer.URL+"/produksi")
	if !strings.Contains(produksi, "(opsional)") {
		t.Fatal("TT no longer says it is optional")
	}

	css := stylesheet(t)
	if !strings.Contains(css, ".stack-form > .panel label .hint:last-child,") {
		t.Fatal("a hint inside a label is still styled as a standalone block")
	}
}

func TestLabelIconsAreMuted(t *testing.T) {
	css := stylesheet(t)
	if !strings.Contains(css, "label .icon { width: 15px; height: 15px; flex-shrink: 0; color: var(--muted); }") {
		t.Fatal("label icons would compete with the labels themselves")
	}
}
