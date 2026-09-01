package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opp-management/internal/export"
	"opp-management/internal/model"
)

// saveExportConfig drives the settings form the way the export card does.
func saveExportConfig(t *testing.T, client *http.Client, testServer *httptest.Server, projectID, project string, form map[string]string) *http.Response {
	t.Helper()
	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings?project="+project)
	values := map[string]string{
		"csrf_token":   csrfFromForm(t, page),
		"aksi":         "export",
		"project_id":   projectID,
		"project_nama": project,
	}
	for key, value := range form {
		values[key] = value
	}
	response, err := client.PostForm(testServer.URL+"/project/settings", urlValues(values))
	if err != nil {
		t.Fatalf("post export settings: %v", err)
	}
	return response
}

// The settings screen is where a project says who signs its reports.
func TestExportSettingsStoresTheSignatureBlock(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	projectID := store.ProjectList()[0].ProjectID

	response := saveExportConfig(t, client, testServer, projectID, testProjectName, map[string]string{
		"export_key":    string(model.ExportProduksi),
		"export_aktif":  "1",
		"ttd_count":     "2",
		"slot1_nama":    "Budi",
		"slot1_jabatan": "Pengawas",
		"slot3_nama":    "Sari",
		"slot3_jabatan": "Manager",
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	configs := store.ExportConfigList()
	if len(configs) != 1 {
		t.Fatalf("stored %d export configs, want 1", len(configs))
	}
	config := configs[0]
	if config.ExportKey != string(model.ExportProduksi) || !config.Aktif || config.TTDCount != 2 {
		t.Fatalf("config stored wrong: %+v", config)
	}
	if config.Slots[0].Nama != "Budi" || config.Slots[0].Jabatan != "Pengawas" {
		t.Fatalf("left slot stored wrong: %+v", config.Slots[0])
	}
	if config.Slots[2].Nama != "Sari" || config.Slots[2].Jabatan != "Manager" {
		t.Fatalf("right slot stored wrong: %+v", config.Slots[2])
	}
}

// A count outside one, two or three would leave the closing block undefined.
func TestExportSettingsRefusesAnImpossibleSignatureCount(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	projectID := store.ProjectList()[0].ProjectID

	response := saveExportConfig(t, client, testServer, projectID, testProjectName, map[string]string{
		"export_key":   string(model.ExportProduksi),
		"export_aktif": "1",
		"ttd_count":    "4",
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}
	if configs := store.ExportConfigList(); len(configs) != 0 {
		t.Fatalf("stored %d configs, want the refusal to store nothing", len(configs))
	}
}

// Switching a report off has to hold at the URL too: the page hiding its
// buttons is a courtesy, not the rule.
func TestSwitchedOffExportRefusesItsDownload(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	projectID := store.ProjectList()[0].ProjectID

	response := saveExportConfig(t, client, testServer, projectID, testProjectName, map[string]string{
		"export_key": string(model.ExportUnitDT),
		"ttd_count":  "1",
	})
	response.Body.Close()

	download, err := client.Get(testServer.URL + "/unit/export/download?format=xlsx")
	if err != nil {
		t.Fatalf("download unit register: %v", err)
	}
	defer download.Body.Close()
	if download.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a report the project switched off", download.StatusCode)
	}

	page := fetchAuthedPage(t, client, testServer.URL+"/unit/export")
	if !strings.Contains(page, "dinonaktifkan untuk project ini") {
		t.Fatal("the export page still offers a report the project switched off")
	}
}

// A project that has never opened the settings screen keeps downloading
// everything, which is what it did before the setting existed.
func TestExportStaysOnUntilAProjectSwitchesItOff(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	download, err := client.Get(testServer.URL + "/unit/export/download?format=xlsx")
	if err != nil {
		t.Fatalf("download unit register: %v", err)
	}
	defer download.Body.Close()
	if download.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for an unconfigured project", download.StatusCode)
	}
}

// The positions fill from the edges, and a blank slot falls back to the
// project's own signatory rather than printing an empty column.
func TestSignatoriesFillFromTheEdges(t *testing.T) {
	fallback := export.Signatory{Name: "Default", Title: "Kepala Teknik"}
	config := model.ExportConfig{
		TTDCount: 3,
		Slots: [3]model.ExportSlot{
			{Nama: "Kiri", Jabatan: "Pengawas"},
			{},
			{Nama: "Kanan", Jabatan: "Manager"},
		},
	}

	three := signatoriesFor(config, fallback)
	if len(three) != 3 {
		t.Fatalf("printed %d signatures, want 3", len(three))
	}
	if three[0].Name != "Kiri" || three[2].Name != "Kanan" {
		t.Fatalf("edges laid out wrong: %+v", three)
	}
	if three[1] != fallback {
		t.Fatalf("blank centre = %+v, want the project's own signatory", three[1])
	}

	config.TTDCount = 2
	two := signatoriesFor(config, fallback)
	if len(two) != 2 || two[0].Name != "Kiri" || two[1].Name != "Kanan" {
		t.Fatalf("two signatures laid out wrong: %+v", two)
	}

	config.TTDCount = 1
	one := signatoriesFor(config, fallback)
	if len(one) != 1 || one[0].Name != "Kanan" {
		t.Fatalf("a single signature belongs on the right: %+v", one)
	}
}
