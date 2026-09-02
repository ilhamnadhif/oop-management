package handler

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opp-management/internal/model"
)

// logoUpload is a small transparent PNG, the shape a real logo arrives in.
func logoUpload(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 48, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 48; x++ {
			alpha := uint8(255)
			if x < 8 {
				alpha = 0
			}
			img.Set(x, y, color.RGBA{R: 200, G: 30, B: 45, A: alpha})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode logo: %v", err)
	}
	return buffer.Bytes()
}

// postProjectSettings submits the general settings form, with any files given.
func postProjectSettings(t *testing.T, client *http.Client, testServer *httptest.Server, project model.Project, fields map[string]string, files map[string][]byte) *http.Response {
	t.Helper()
	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings?project="+project.Nama)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	values := map[string]string{
		"csrf_token":   csrfFromForm(t, page),
		"aksi":         "simpan",
		"project_id":   project.ProjectID,
		"project_nama": project.Nama,
		"nama":         project.Nama,
		"status":       model.StatusAktif,
	}
	for key, value := range fields {
		values[key] = value
	}
	for key, value := range values {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write field %s: %v", key, err)
		}
	}
	for field, content := range files {
		part, err := writer.CreateFormFile(field, field+".png")
		if err != nil {
			t.Fatalf("create file %s: %v", field, err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatalf("write file %s: %v", field, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	response, err := client.Post(testServer.URL+"/project/settings", writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatalf("post settings: %v", err)
	}
	return response
}

func firstProject(t *testing.T, store interface{ ProjectList() []model.Project }) model.Project {
	t.Helper()
	list := store.ProjectList()
	if len(list) == 0 {
		t.Fatal("no project to configure")
	}
	return list[0]
}

// A project that has uploaded nothing keeps the app's own artwork, which is
// what every deployment looked like before this setting existed.
func TestPagesFallBackToTheAppArtwork(t *testing.T) {
	testServer := newTestServer(t)
	page := fetchDashboard(t, loggedInClient(t, testServer), testServer)

	for _, want := range []string{`href="/static/img/favicon.ico"`, `/static/img/opp-logo`} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page is missing %q", want)
		}
	}
	if strings.Contains(page, brandLogoPath) {
		t.Fatal("a project with no mark still asks for one")
	}
}

// An uploaded system logo replaces the artwork in the shell, and is served
// from a URL rather than inlined into every page.
func TestAnUploadedLogoIsUsedAcrossTheShell(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	project := firstProject(t, store)

	response := postProjectSettings(t, client, testServer, project, nil,
		map[string][]byte{"logo_sistem": logoUpload(t)})
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	page := fetchDashboard(t, client, testServer)
	if !strings.Contains(page, brandLogoPath+"?v=") {
		t.Fatalf("the shell does not use the uploaded logo:\n%s", firstLines(page))
	}
	if strings.Contains(page, "data:image/png;base64") {
		t.Fatal("the logo was inlined into the page rather than served")
	}

	// The URL actually serves the image, as a PNG: a logo re-encoded as JPEG
	// would have lost its transparent ground.
	logo, err := client.Get(testServer.URL + brandLogoPath)
	if err != nil {
		t.Fatalf("get logo: %v", err)
	}
	defer logo.Body.Close()
	if logo.StatusCode != http.StatusOK {
		t.Fatalf("logo status = %d", logo.StatusCode)
	}
	if got := logo.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type = %q, want image/png", got)
	}
	body := readBodyBytes(t, logo)
	if _, format, err := image.Decode(bytes.NewReader(body)); err != nil || format != "png" {
		t.Fatalf("served %q: %v", format, err)
	}
}

// Every stored mark shows a preview on the settings screen. The export mark is
// drawn into the report rather than onto a page, but a column that showed
// nothing read as an upload that had failed.
func TestEveryStoredMarkHasAPreview(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	project := firstProject(t, store)

	postProjectSettings(t, client, testServer, project, nil, map[string][]byte{
		"logo_sistem": logoUpload(t),
		"logo_export": logoUpload(t),
		"favicon":     logoUpload(t),
	}).Body.Close()

	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings?project="+project.Nama)
	for _, path := range []string{brandLogoPath, brandExportLogoPath, brandFaviconPath} {
		if !strings.Contains(page, path+"?v=") {
			t.Fatalf("the settings screen shows no preview for %s", path)
		}
		served, err := client.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		status := served.StatusCode
		served.Body.Close()
		if status != http.StatusOK {
			t.Fatalf("%s status = %d", path, status)
		}
	}
	// The letterhead mark is not part of the app shell, only of its own preview.
	dashboard := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	if strings.Contains(dashboard, brandExportLogoPath) {
		t.Fatal("the export mark leaked into the app shell")
	}
}

// The favicon is its own upload, so a square mark can be given to the browser
// tab without squashing the sidebar logo into a square.
func TestTheFaviconIsItsOwnUpload(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	project := firstProject(t, store)

	postProjectSettings(t, client, testServer, project, nil,
		map[string][]byte{"favicon": logoUpload(t)}).Body.Close()

	page := fetchDashboard(t, client, testServer)
	if !strings.Contains(page, brandFaviconPath+"?v=") {
		t.Fatal("the page does not use the uploaded favicon")
	}
	// Only the favicon was uploaded, so the logo still falls back.
	if strings.Contains(page, brandLogoPath) {
		t.Fatal("uploading a favicon also changed the logo")
	}
}

// Saving the settings without choosing a file leaves the stored marks alone.
// Somebody editing the working hours has not uploaded a logo, and their save
// must not wipe one.
func TestSavingWithoutAFileKeepsTheStoredMark(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	project := firstProject(t, store)

	postProjectSettings(t, client, testServer, project, nil,
		map[string][]byte{"logo_sistem": logoUpload(t)}).Body.Close()
	postProjectSettings(t, client, testServer, project,
		map[string]string{"work_start": "07:30"}, nil).Body.Close()

	if mark := firstProject(t, store).Settings.LogoSistem; mark == "" {
		t.Fatal("a save that uploaded nothing wiped the stored logo")
	}
}

// Removing a mark is its own tick, so "no file chosen" and "go back to the
// app's artwork" are never confused.
func TestAMarkIsRemovedByItsOwnTick(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	project := firstProject(t, store)

	postProjectSettings(t, client, testServer, project, nil,
		map[string][]byte{"logo_sistem": logoUpload(t)}).Body.Close()
	postProjectSettings(t, client, testServer, project,
		map[string]string{"hapus_logo_sistem": "1"}, nil).Body.Close()

	if mark := firstProject(t, store).Settings.LogoSistem; mark != "" {
		t.Fatal("the tick did not put the app's own artwork back")
	}
	if page := fetchDashboard(t, client, testServer); strings.Contains(page, brandLogoPath) {
		t.Fatal("the shell still asks for a mark that was removed")
	}
}

// A file that is not an image is refused, and the page says which of the three
// uploads it was.
func TestAnUploadThatIsNotAnImageIsRefused(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	project := firstProject(t, store)

	response := postProjectSettings(t, client, testServer, project, nil,
		map[string][]byte{"logo_export": []byte("bukan gambar")})
	defer response.Body.Close()
	body := readBody(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}
	if !strings.Contains(body, "Logo export harus berupa gambar") {
		t.Fatalf("the page does not say which upload failed:\n%s", firstLines(body))
	}
	if firstProject(t, store).Settings.LogoExport != "" {
		t.Fatal("a refused upload was stored anyway")
	}
}

// The company name is the project's, and it reaches the places outside the
// export letterhead too.
func TestTheCompanyNameReachesTheShell(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	project := firstProject(t, store)

	saved := postProjectSettings(t, client, testServer, project,
		map[string]string{"company": "PT Contoh Sejahtera"}, nil)
	savedBody := readBody(t, saved)
	saved.Body.Close()
	if saved.StatusCode != http.StatusOK {
		t.Fatalf("save status = %d:\n%s", saved.StatusCode, firstLines(savedBody))
	}

	page := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	for _, want := range []string{
		"<title>Dashboard · PT Contoh Sejahtera</title>",
		"PT Contoh Sejahtera</p>",
		"alt=\"Logo PT Contoh Sejahtera\"",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the shell is missing %q", want)
		}
	}
}

// The export letterhead uses the project's own mark when it has one.
func TestTheExportLetterheadUsesTheProjectsMark(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	project := firstProject(t, store)
	seedUnit(t, store)

	postProjectSettings(t, client, testServer, project, nil,
		map[string][]byte{"logo_export": logoUpload(t)}).Body.Close()

	// The mark is drawn into the file, so what is checked here is that the
	// report still builds with it rather than falling over on a decode.
	download, err := client.Get(testServer.URL + "/unit/export/download?format=pdf")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer download.Body.Close()
	if download.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", download.StatusCode)
	}
	if !bytes.HasPrefix(readBodyBytes(t, download), []byte("%PDF-")) {
		t.Fatal("the report is not a pdf")
	}
}

// The signed-out pages are the deployment's own front door. Nobody has said
// which project they belong to yet, so a project's mark and name have no
// business there - and uploading one must not change them.
func TestSignedOutPagesKeepTheAppsOwnBranding(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	project := firstProject(t, store)

	postProjectSettings(t, client, testServer, project,
		map[string]string{"company": "PT Contoh Sejahtera"},
		map[string][]byte{"logo_sistem": logoUpload(t), "favicon": logoUpload(t)}).Body.Close()

	for _, path := range []string{"/login", "/register"} {
		page := fetchPage(t, testServer.URL+path)
		for _, want := range []string{
			"· Orecon Putra Perkasa</title>",
			`href="/static/img/favicon.ico"`,
			`src="/static/img/opp-logo.png"`,
		} {
			if !strings.Contains(page, want) {
				t.Fatalf("%s is missing %q", path, want)
			}
		}
		for _, gone := range []string{brandLogoPath, brandFaviconPath, "PT Contoh Sejahtera"} {
			if strings.Contains(page, gone) {
				t.Fatalf("%s carries %q, which belongs to a project", path, gone)
			}
		}
	}
}

// icoUpload wraps a PNG in a Windows icon file, which is the form a favicon
// usually arrives in.
func icoUpload(t *testing.T) []byte {
	t.Helper()
	payload := logoUpload(t)
	var buffer bytes.Buffer
	writeU16 := func(v uint16) { buffer.Write([]byte{byte(v), byte(v >> 8)}) }
	writeU32 := func(v uint32) {
		buffer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
	}
	writeU16(0)
	writeU16(1)
	writeU16(1)
	buffer.Write([]byte{32, 32, 0, 0})
	writeU16(1)
	writeU16(32)
	writeU32(uint32(len(payload)))
	writeU32(22)
	buffer.Write(payload)
	return buffer.Bytes()
}

// The favicon field takes a .ico, which is what most organisations already
// have, and serves it back under the type a browser expects.
func TestTheFaviconTakesAnIcoFile(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	project := firstProject(t, store)

	response := postProjectSettings(t, client, testServer, project, nil,
		map[string][]byte{"favicon": icoUpload(t)})
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want the icon accepted", response.StatusCode)
	}

	served, err := client.Get(testServer.URL + brandFaviconPath)
	if err != nil {
		t.Fatalf("get favicon: %v", err)
	}
	defer served.Body.Close()
	if served.StatusCode != http.StatusOK {
		t.Fatalf("favicon status = %d", served.StatusCode)
	}
	if got := served.Header.Get("Content-Type"); got != "image/x-icon" {
		t.Fatalf("content type = %q, want image/x-icon", got)
	}
	if !bytes.Equal(readBodyBytes(t, served), icoUpload(t)) {
		t.Fatal("the icon was not served back byte for byte")
	}
}

// An icon is only taken on the favicon field: the sidebar and the letterhead
// draw their mark, and nothing here can decode one to draw.
func TestOnlyTheFaviconTakesAnIcoFile(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	project := firstProject(t, store)

	response := postProjectSettings(t, client, testServer, project, nil,
		map[string][]byte{"logo_sistem": icoUpload(t)})
	defer response.Body.Close()
	body := readBody(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want an icon refused as a system logo", response.StatusCode)
	}
	if !strings.Contains(body, "Logo sistem harus berupa gambar PNG atau JPG") {
		t.Fatalf("the page does not say what the field takes:\n%s", firstLines(body))
	}
	// The favicon field says what it does take, which is one format more.
	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings?project="+project.Nama)
	if !strings.Contains(page, "PNG, JPG, atau ICO") {
		t.Fatal("the favicon field does not say it takes an icon")
	}
}
