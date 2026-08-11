package handler

import (
	"strings"
	"testing"
)

// Every page opens with the same header: its name, where it sits, and one line
// saying what it is for.
func TestEveryPageHeaderCarriesItsLede(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	pages := map[string]string{
		"/dashboard":         "Ringkasan kehadiran Anda sendiri",
		"/absensi":           "Catat kehadiran hari ini",
		"/produksi":          "Kelola dan catat data produksi harian",
		"/produksi/overview": "Ringkasan volume, ritase",
		"/produksi/export":   "Unduh laporan produksi",
		"/nota":              "Catat nota belanja",
		"/nota/overview":     "Ringkasan pengeluaran",
		"/nota/rekonsiliasi": "Tandai reimburse yang sudah dibayar",
		"/nota/export":       "Unduh laporan nota",
		"/unit/overview":     "Ringkasan isi daftar unit",
		"/unit-dt":           "Daftarkan dump truck",
		"/unit-a2b":          "Daftarkan alat berat",
		"/a2b/overview":      "Ringkasan alat berat",
		"/a2b/hm":            "Catat pembacaan hour meter",
		"/a2b/fuel-masuk":    "Catat kiriman fuel dari vendor",
		"/a2b/fuel-keluar":   "Catat pengisian bahan bakar tiap alat berat",
		"/a2b/export":        "Unduh daftar alat berat",
		"/unit/export":       "Unduh daftar unit DT",
	}
	for path, lede := range pages {
		page := fetchAuthedPage(t, client, testServer.URL+path)
		if !strings.Contains(page, `<p class="page-lede">`) {
			t.Fatalf("%s has no lede under its title", path)
		}
		if !strings.Contains(page, lede) {
			t.Fatalf("%s does not say %q", path, lede)
		}
		// The lede belongs to the header, so it follows the breadcrumb.
		if strings.Index(page, `class="breadcrumb"`) > strings.Index(page, `class="page-lede"`) {
			t.Fatalf("%s prints its lede above the breadcrumb", path)
		}
	}
}

// Absensi keeps the header but not the red band: its clock card is already red,
// and two reds meeting leave a seam.
func TestAbsensiOpensWithoutTheRedBand(t *testing.T) {
	css := stylesheet(t)
	for _, rule := range []string{
		`.app-layout[data-page="absensi"] .app-shell::before,`,
		`.app-layout[data-page="absensi"] .app-shell > .page-title { color: var(--ink); }`,
		`.app-layout[data-page="absensi"] .page-lede { margin-bottom: 1.6rem; color: var(--muted); }`,
	} {
		if !strings.Contains(css, rule) {
			t.Fatalf("the stylesheet is missing %q", rule)
		}
	}

	// The page still carries the header itself; only the band is dropped.
	testServer := newTestServer(t)
	page := fetchAuthedPage(t, loggedInClient(t, testServer), testServer.URL+"/absensi")
	for _, fragment := range []string{`class="page-title"`, `class="breadcrumb"`, `class="page-lede"`} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the absensi page lost %q", fragment)
		}
	}
}

// The header is one rule for the whole app rather than one page's decoration.
func TestPageHeaderAppliesToEveryPage(t *testing.T) {
	css := stylesheet(t)

	for _, rule := range []string{
		".app-shell { position: relative; isolation: isolate; }",
		".app-shell::before {",
		".app-shell > .page-title { color: #fff; }",
	} {
		if !strings.Contains(css, rule) {
			t.Fatalf("the stylesheet is missing %q", rule)
		}
	}
	// The header used to be bolted to one page, with that page's name written in
	// CSS over a title made transparent. A name that lives in a stylesheet
	// cannot be renamed without editing CSS, and a screen reader reads the empty
	// original.
	for _, hack := range []string{
		`.app-layout[data-page="produksi-input"] .app-shell::before`,
		`content: "Produksi"`,
		`content: "Produksi "`,
		".page-title { color: transparent; }",
	} {
		if strings.Contains(css, hack) {
			t.Fatalf("the stylesheet still carries %q", hack)
		}
	}
	// The breadcrumb sits inside the header rather than being hidden by it.
	if strings.Contains(css, `.app-layout[data-page="produksi-input"] .breadcrumb { display: none; }`) {
		t.Fatal("the production page still hides its breadcrumb")
	}
}
