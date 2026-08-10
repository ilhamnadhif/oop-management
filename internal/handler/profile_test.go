package handler

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func postProfile(t *testing.T, client *http.Client, testServer *httptest.Server, csrf string, fields map[string]string, image []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if csrf != "" {
		if err := writer.WriteField("csrf_token", csrf); err != nil {
			t.Fatalf("write csrf: %v", err)
		}
	}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if len(image) > 0 {
		part, err := writer.CreateFormFile("foto_profil", "profil.jpg")
		if err != nil {
			t.Fatalf("create photo part: %v", err)
		}
		if _, err := part.Write(image); err != nil {
			t.Fatalf("write photo: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	response, err := client.Post(testServer.URL+"/profile", writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatalf("post profile: %v", err)
	}
	return response
}

// The menu entry and the dialog ride in the shell, so they are on every page
// rather than only on the one the profile lives at.
func TestProfileMenuAndDialogAppearOnEveryPage(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	for _, path := range []string{"/dashboard", "/absensi"} {
		page := fetchAuthedPage(t, client, testServer.URL+path)
		for _, fragment := range []string{
			`<dialog class="modal profile-modal" id="profileDialog"`,
			`data-open-dialog="profileDialog"`,
			`action="/profile"`,
			"Simpan data pribadi",
			// Without the script the link still has somewhere to go.
			`class="account-action" href="/profile"`,
			`src="/static/js/dialog.js"`,
		} {
			if !strings.Contains(page, fragment) {
				t.Fatalf("%s is missing %q", path, fragment)
			}
		}
		// The entry sits above the logout button, which is what was asked for.
		if strings.Index(page, `data-open-dialog="profileDialog"`) > strings.Index(page, `class="account-logout"`) {
			t.Fatalf("%s puts the profile entry below the logout button", path)
		}
	}
}

// NRP and jabatan identify the person to everyone else, so the form shows them
// without letting them be edited or posted back.
func TestProfileFormLocksNRPAndJabatan(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/profile")

	for _, fragment := range []string{
		`id="profil_nrp" type="text" value="123456" disabled`,
		`id="profil_jabatan" type="text" value="Management" disabled`,
		`id="profil_email" type="email" value="budi@example.com" disabled`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the profile form is missing %q", fragment)
		}
	}
	// A disabled field is not submitted, so nothing named nrp or jabatan can
	// reach the handler from this form at all.
	for _, name := range []string{`name="nrp"`, `name="jabatan"`, `name="email"`} {
		if strings.Contains(page, name) {
			t.Fatalf("the profile form posts %s", name)
		}
	}
}

func TestProfileSavesNameNoTelpAndBirthDate(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/profile"))

	response := postProfile(t, client, testServer, csrf, map[string]string{
		"nama_lengkap":  "Budi Santoso Wijaya",
		"no_telp":       "0812-3456-7890",
		"tanggal_lahir": "1990-05-17",
	}, nil)
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", response.StatusCode, body)
	}
	if !strings.Contains(body, "Data pribadi tersimpan.") {
		t.Fatalf("the save was not confirmed: %s", body)
	}

	stored := store.UserList()[0]
	// Punctuation people type but nobody dials is stripped, so the same number
	// is not stored two different ways.
	if stored.NamaLengkap != "Budi Santoso Wijaya" || stored.NoTelp != "081234567890" || stored.TanggalLahir != "1990-05-17" {
		t.Fatalf("stored profile is wrong: %+v", stored)
	}
	if stored.NRP != "123456" || stored.Jabatan != "Management" {
		t.Fatalf("a locked field changed: %+v", stored)
	}
}

func TestProfileRejectsBadPhoneAndFutureBirthDate(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/profile"))

	cases := map[string]struct {
		fields map[string]string
		want   string
	}{
		"letters in phone": {
			map[string]string{"nama_lengkap": "Budi", "no_telp": "0812-ABCD"},
			"nomor telepon hanya boleh angka",
		},
		"phone too short": {
			map[string]string{"nama_lengkap": "Budi", "no_telp": "0812"},
			"nomor telepon harus 8 sampai 15 digit",
		},
		"unborn": {
			// The server clock is fixed at 2026-08-07 in these tests.
			map[string]string{"nama_lengkap": "Budi", "tanggal_lahir": "2027-01-01"},
			"tanggal lahir tidak boleh di masa depan",
		},
		"empty name": {
			map[string]string{"nama_lengkap": "   "},
			"nama lengkap wajib diisi",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			response := postProfile(t, client, testServer, csrf, tc.fields, nil)
			body := readBody(t, response)
			if response.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422: %s", response.StatusCode, body)
			}
			if !strings.Contains(body, tc.want) {
				t.Fatalf("body does not say %q: %s", tc.want, body)
			}
		})
	}
	// A rejected save leaves the account exactly as it was.
	if stored := store.UserList()[0]; stored.NoTelp != "" || stored.TanggalLahir != "" {
		t.Fatalf("a rejected save was written: %+v", stored)
	}
}

// The photo is stored in a column no listing reads and served on its own, so
// the page carries a URL rather than tens of thousands of characters.
func TestProfilePhotoIsUploadedStoredAndServedSeparately(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/profile"))

	missing, err := client.Get(testServer.URL + "/profile/photo")
	if err != nil {
		t.Fatalf("get photo before upload: %v", err)
	}
	readBody(t, missing)
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("photo status before upload = %d, want 404", missing.StatusCode)
	}

	response := postProfile(t, client, testServer, csrf,
		map[string]string{"nama_lengkap": "Budi Santoso"}, testJPEG(t))
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("upload status %d: %s", response.StatusCode, body)
	}

	stored := store.UserList()[0]
	if !stored.PunyaFoto || !strings.HasPrefix(stored.FotoProfil, "data:image/jpeg;base64,") {
		t.Fatalf("the photo was not stored: punyaFoto=%v len=%d", stored.PunyaFoto, len(stored.FotoProfil))
	}

	// The page must never inline the image; it points at the endpoint instead.
	page := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	if strings.Contains(page, "data:image/jpeg;base64,") {
		t.Fatal("the page inlined the profile photo")
	}
	if !strings.Contains(page, `class="account-avatar-photo" src="/profile/photo?v=`) {
		t.Fatal("the avatar does not point at the photo endpoint")
	}

	served, err := client.Get(testServer.URL + "/profile/photo")
	if err != nil {
		t.Fatalf("get photo: %v", err)
	}
	defer served.Body.Close()
	payload, err := io.ReadAll(served.Body)
	if err != nil {
		t.Fatalf("read photo: %v", err)
	}
	if served.StatusCode != http.StatusOK || served.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("photo response = %d %q", served.StatusCode, served.Header.Get("Content-Type"))
	}
	if !bytes.HasPrefix(payload, []byte{0xFF, 0xD8}) {
		t.Fatal("the served bytes are not a JPEG")
	}

	// Removing it clears both the flag and the stored image.
	removeCSRF := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/profile"))
	removed := postProfile(t, client, testServer, removeCSRF,
		map[string]string{"nama_lengkap": "Budi Santoso", "hapus_foto": "1"}, nil)
	readBody(t, removed)
	if stored := store.UserList()[0]; stored.PunyaFoto || stored.FotoProfil != "" {
		t.Fatalf("the photo survived removal: %+v", stored)
	}
}

// Saving a phone number must not disturb a photo the save never carried.
func TestProfileSaveWithoutUploadKeepsTheStoredPhoto(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/profile"))
	readBody(t, postProfile(t, client, testServer, csrf,
		map[string]string{"nama_lengkap": "Budi Santoso"}, testJPEG(t)))
	before := store.UserList()[0].FotoProfil
	if before == "" {
		t.Fatal("the photo was not stored")
	}

	csrf = csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/profile"))
	readBody(t, postProfile(t, client, testServer, csrf,
		map[string]string{"nama_lengkap": "Budi Santoso", "no_telp": "081234567890"}, nil))

	stored := store.UserList()[0]
	if stored.FotoProfil != before || !stored.PunyaFoto {
		t.Fatal("saving a phone number disturbed the stored photo")
	}
	if stored.NoTelp != "081234567890" {
		t.Fatalf("the phone number was not saved: %+v", stored)
	}
}

func TestProfileRequiresASessionAndACSRFToken(t *testing.T) {
	testServer := newTestServer(t)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, path := range []string{"/profile", "/profile/photo"} {
		response, err := client.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		readBody(t, response)
		if response.StatusCode != http.StatusSeeOther && response.StatusCode != http.StatusFound {
			t.Fatalf("%s without a session = %d, want a redirect", path, response.StatusCode)
		}
	}

	authed := loggedInClient(t, testServer)
	forged := postProfile(t, authed, testServer, "not-the-token",
		map[string]string{"nama_lengkap": "Budi"}, nil)
	readBody(t, forged)
	if forged.StatusCode != http.StatusForbidden {
		t.Fatalf("a forged token was accepted: %d", forged.StatusCode)
	}
}

// The HR lists name people the viewer is not, so their faces are fetched by id
// rather than inlined, and every row keeps an avatar whether or not a photo
// exists.
func TestHROverviewListsCarryAvatars(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")

	absent := &model.User{
		UserID: "usr_avatar", NRP: "990001", NamaLengkap: "Ani Lestari", Jabatan: "Logistik",
		Email: "ani@example.test", TanggalGabung: "2025-01-01", StatusPengguna: model.StatusAktif,
		PunyaFoto: true, FotoProfil: "data:image/jpeg;base64," +
			base64.StdEncoding.EncodeToString(testJPEG(t)),
	}
	if err := store.CreateUser(t.Context(), absent); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	page := fetchAuthedPage(t, hr, testServer.URL+"/hr/overview")
	if !strings.Contains(page, `<img src="/profile/photo?user_id=usr_avatar&amp;v=`) {
		t.Fatalf("the absent list has no avatar for the seeded user: %s", page)
	}
	// The HR user has no photo, so their row falls back to the initial rather
	// than to a broken image.
	if !strings.Contains(page, `<span class="person-avatar" aria-hidden="true">B</span>`) {
		t.Fatal("a person without a photo lost their avatar placeholder")
	}
	if strings.Contains(page, "data:image/jpeg;base64,") {
		t.Fatal("the HR overview inlined a profile photo")
	}

	served, err := hr.Get(testServer.URL + "/profile/photo?user_id=usr_avatar")
	if err != nil {
		t.Fatalf("get colleague photo: %v", err)
	}
	readBody(t, served)
	if served.StatusCode != http.StatusOK {
		t.Fatalf("HR reading a colleague photo = %d, want 200", served.StatusCode)
	}
}

// Anyone who may not see the page that lists colleagues by name may not
// enumerate their faces through the photo endpoint either.
func TestProfilePhotoOfOthersNeedsHRAccess(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	// Registering the HR client first makes it the account with a photo.
	hr := loggedInClientAs(t, testServer, "HR")
	csrf := csrfFromForm(t, fetchAuthedPage(t, hr, testServer.URL+"/profile"))
	readBody(t, postProfile(t, hr, testServer, csrf,
		map[string]string{"nama_lengkap": "Budi Santoso"}, testJPEG(t)))
	target := store.UserList()[0].UserID

	outsider := loggedInClientAs(t, testServer, "Produksi")
	response, err := outsider.Get(testServer.URL + "/profile/photo?user_id=" + target)
	if err != nil {
		t.Fatalf("get another photo: %v", err)
	}
	readBody(t, response)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("an unprivileged account read another face: %d", response.StatusCode)
	}
}

// The listing reads stop one column short of the photo, so a caller that only
// listed users never drags the images along.
func TestUserListingsCarryNoPhoto(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/profile"))
	readBody(t, postProfile(t, client, testServer, csrf,
		map[string]string{"nama_lengkap": "Budi Santoso"}, testJPEG(t)))

	users, err := repository.Store(store).ListUsers(t.Context())
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	for _, user := range users {
		if user.FotoProfil != "" {
			t.Fatalf("ListUsers carried a %d character photo", len(user.FotoProfil))
		}
		if !user.PunyaFoto {
			t.Fatal("the listing lost the flag that says a photo exists")
		}
	}
}
