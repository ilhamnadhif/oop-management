package handler

import (
	"golang.org/x/crypto/bcrypt"

	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

func TestWebRegisterLoginAndAttendanceFlow(t *testing.T) {
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, location)
	nowFunc := func() time.Time { return now }
	auth := service.NewAuthService(store, location, nowFunc).WithHashCost(bcrypt.MinCost)
	attendance := service.NewAttendanceService(store, location, nowFunc)
	webSessions := session.NewManager(24*time.Hour, false)
	server, err := NewServer(auth, attendance, service.NewUnitDTService(store, location, nowFunc), service.NewProduksiService(store, location, nowFunc), service.NewOverviewService(store, location, nowFunc), service.NewUnitA2BService(store, location, nowFunc), service.NewNotaService(store, location, nowFunc), service.NewLeaveService(store, location, nowFunc), service.NewUnitOverviewService(store, location, nowFunc), webSessions, location, nowFunc, 2*1024*1024, photo.MaxOutputChars, Branding{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}

	registerResponse, err := client.PostForm(testServer.URL+"/register", urlValues(map[string]string{
		"tanggal_gabung":  "2026-08-07",
		"nama_lengkap":    "Budi Santoso",
		"nrp":             "123456",
		"jabatan":         "Produksi",
		"email":           "budi@example.com",
		"password":        "rahasia123",
		"status_pengguna": model.StatusAktif,
	}))
	if err != nil {
		t.Fatalf("register request: %v", err)
	}
	registerResponse.Body.Close()
	if registerResponse.StatusCode != http.StatusOK {
		t.Fatalf("register final status: %d", registerResponse.StatusCode)
	}

	loginResponse, err := client.PostForm(testServer.URL+"/login", urlValues(map[string]string{
		"identifier": "123456",
		"password":   "rahasia123",
	}))
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	loginBody := readBody(t, loginResponse)
	if loginResponse.StatusCode != http.StatusOK || !strings.Contains(loginBody, "Budi Santoso") {
		t.Fatalf("unexpected login/dashboard response: status=%d body=%s", loginResponse.StatusCode, loginBody)
	}
	// Clocking in is done on the attendance page, which carries the token.
	attendancePage, err := client.Get(testServer.URL + "/absensi")
	if err != nil {
		t.Fatalf("attendance request: %v", err)
	}
	csrf := csrfFromBody(t, readBody(t, attendancePage))

	photoBytes := testJPEG(t)
	clockInResponse := doAttendanceRequest(t, client, testServer.URL+"/absensi/clock-in", csrf, photoBytes, "-6.2", "106.8")
	if clockInResponse.StatusCode != http.StatusOK {
		t.Fatalf("clock in status: %d body=%s", clockInResponse.StatusCode, readBody(t, clockInResponse))
	}
	var clockInPayload map[string]interface{}
	if err := json.Unmarshal([]byte(readBody(t, clockInResponse)), &clockInPayload); err != nil || clockInPayload["ok"] != true {
		t.Fatalf("unexpected clock in payload: %v", clockInPayload)
	}
	registeredUser, err := store.FindUserByIdentifier(context.Background(), "123456")
	if err != nil {
		t.Fatalf("find registered user: %v", err)
	}
	storedAttendance, _, err := store.FindAttendanceByUserDate(context.Background(), registeredUser.UserID, "2026-08-07")
	if err != nil {
		t.Fatalf("find stored attendance: %v", err)
	}
	if storedAttendance == nil || !strings.HasPrefix(storedAttendance.ClockInPhoto, photo.DataURLPrefix) {
		t.Fatalf("clock in photo was not stored as data URL: %+v", storedAttendance)
	}
	if err := photo.ValidateDataURL(storedAttendance.ClockInPhoto); err != nil {
		t.Fatalf("stored clock in photo is not decodable: %v", err)
	}

	duplicateResponse := doAttendanceRequest(t, client, testServer.URL+"/absensi/clock-in", csrf, photoBytes, "-6.2", "106.8")
	if duplicateResponse.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate clock in status: %d body=%s", duplicateResponse.StatusCode, readBody(t, duplicateResponse))
	}

	now = time.Date(2026, 8, 7, 17, 5, 0, 0, location)
	clockOutResponse := doAttendanceRequest(t, client, testServer.URL+"/absensi/clock-out", csrf, photoBytes, "-6.21", "106.81")
	if clockOutResponse.StatusCode != http.StatusOK {
		t.Fatalf("clock out status: %d body=%s", clockOutResponse.StatusCode, readBody(t, clockOutResponse))
	}

	dashboardResponse, err := client.Get(testServer.URL + "/absensi")
	if err != nil {
		t.Fatalf("dashboard request: %v", err)
	}
	dashboardBody := readBody(t, dashboardResponse)
	if dashboardResponse.StatusCode != http.StatusOK || !strings.Contains(dashboardBody, "Sudah Clock Out") || !strings.Contains(dashboardBody, "545 menit") {
		t.Fatalf("unexpected completed dashboard: status=%d body contains status=%v duration=%v", dashboardResponse.StatusCode, strings.Contains(dashboardBody, "Sudah Clock Out"), strings.Contains(dashboardBody, "545 menit"))
	}

	logoutResponse, err := client.PostForm(testServer.URL+"/logout", urlValues(map[string]string{"csrf_token": csrfFromBody(t, dashboardBody)}))
	if err != nil {
		t.Fatalf("logout request: %v", err)
	}
	logoutBody := readBody(t, logoutResponse)
	if logoutResponse.StatusCode != http.StatusOK || !strings.Contains(logoutBody, "Selamat datang") {
		t.Fatalf("unexpected logout response: status=%d body=%s", logoutResponse.StatusCode, logoutBody)
	}
}

func TestLoginFailureUsesGenericMessage(t *testing.T) {
	store := repository.NewTestRepository()
	now := time.Now()
	auth := service.NewAuthService(store, time.Local, func() time.Time { return now }).WithHashCost(bcrypt.MinCost)
	attendance := service.NewAttendanceService(store, time.Local, func() time.Time { return now })
	webSessions := session.NewManager(time.Hour, false)
	server, err := NewServer(auth, attendance, service.NewUnitDTService(store, time.Local, func() time.Time { return now }), service.NewProduksiService(store, time.Local, func() time.Time { return now }), service.NewOverviewService(store, time.Local, func() time.Time { return now }), service.NewUnitA2BService(store, time.Local, func() time.Time { return now }), service.NewNotaService(store, time.Local, func() time.Time { return now }), service.NewLeaveService(store, time.Local, func() time.Time { return now }), service.NewUnitOverviewService(store, time.Local, func() time.Time { return now }), webSessions, time.Local, func() time.Time { return now }, 2*1024*1024, photo.MaxOutputChars, Branding{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if _, err := auth.Register(context.Background(), service.RegisterInput{
		TanggalGabung: "2026-08-07", NamaLengkap: "Budi", NRP: "1", Jabatan: "Produksi",
		Email: "budi@example.com", Password: "rahasia123", Status: model.StatusAktif,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	response, err := http.PostForm(testServer.URL+"/login", urlValues(map[string]string{"identifier": "1", "password": "salah123"}))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	body := readBody(t, response)
	if response.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "NRP/Email atau password salah") || strings.Contains(body, "Password salah") {
		t.Fatalf("unexpected login failure response: status=%d body=%s", response.StatusCode, body)
	}
	activities := store.Activities()
	if len(activities) != 1 || activities[0].Status != model.ActivityFailed {
		t.Fatalf("failed login was not recorded: %+v", activities)
	}
}

func doAttendanceRequest(t *testing.T, client *http.Client, endpoint, csrf string, imageBytes []byte, latitude, longitude string) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("face_photo", "selfie.jpg")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(imageBytes); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	_ = writer.WriteField("latitude", latitude)
	_ = writer.WriteField("longitude", longitude)
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		t.Fatalf("create attendance request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-CSRF-Token", csrf)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("attendance request: %v", err)
	}
	return response
}

func urlValues(values map[string]string) url.Values {
	result := url.Values{}
	for key, value := range values {
		result.Set(key, value)
	}
	return result
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	var buffer bytes.Buffer
	_, _ = buffer.ReadFrom(response.Body)
	return buffer.String()
}

func csrfFromBody(t *testing.T, body string) string {
	t.Helper()
	re := regexp.MustCompile(`data-csrf-token="([^"]+)"`)
	matches := re.FindStringSubmatch(body)
	if len(matches) != 2 {
		t.Fatalf("csrf token not found")
	}
	return matches[1]
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 220, G: uint8(x * 4), B: uint8(y * 4), A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buffer.Bytes()
}
