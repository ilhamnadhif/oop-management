package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"opp-management/internal/export"
	"opp-management/internal/photo"
	"opp-management/internal/receipt"
	"opp-management/internal/repository"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

type fakeReceiptScanner struct {
	mu      sync.Mutex
	result  receipt.Result
	err     error
	scanFn  func(context.Context, string) (receipt.Result, error)
	calls   int
	dataURL string
}

func (f *fakeReceiptScanner) Scan(ctx context.Context, imageDataURL string) (receipt.Result, error) {
	f.mu.Lock()
	f.calls++
	f.dataURL = imageDataURL
	result, err, scanFn := f.result, f.err, f.scanFn
	f.mu.Unlock()
	if scanFn != nil {
		return scanFn(ctx, imageDataURL)
	}
	return result, err
}

func (f *fakeReceiptScanner) snapshot() (int, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.dataURL
}

func newReceiptScanServer(t *testing.T, scanner receipt.Scanner) (*httptest.Server, *repository.TestRepository) {
	t.Helper()
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, location)
	nowFunc := func() time.Time { return now }
	server, err := NewServer(
		service.NewAuthService(store, location, nowFunc).WithHashCost(bcrypt.MinCost),
		service.NewAttendanceService(store, location, nowFunc),
		service.NewUnitDTService(store, location, nowFunc),
		service.NewProduksiService(store, location, nowFunc),
		service.NewOverviewService(store, location, nowFunc),
		service.NewUnitA2BService(store, location, nowFunc),
		service.NewNotaService(store, location, nowFunc),
		service.NewLeaveService(store, location, nowFunc),
		service.NewUnitOverviewService(store, location, nowFunc),
		service.NewFuelMasukService(store, location, nowFunc),
		service.NewFuelKeluarService(store, location, nowFunc),
		session.NewManager(24*time.Hour, false),
		location, nowFunc, 2*1024*1024, photo.MaxOutputChars,
		Branding{Company: "PT Orecon Putra Perkasa", Signatory: export.Signatory{Title: "Direktur"}},
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server.WithReceiptScanner(scanner)
	testServer := httptest.NewServer(server.Handler())
	t.Cleanup(testServer.Close)
	return testServer, store
}

func postReceiptScan(t *testing.T, client *http.Client, testServer *httptest.Server, csrf, filename string, image []byte) *http.Response {
	t.Helper()
	request, err := newReceiptScanRequest(testServer.URL, csrf, filename, image)
	if err != nil {
		t.Fatalf("new receipt scan request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("post receipt scan: %v", err)
	}
	return response
}

func newReceiptScanRequest(baseURL, csrf, filename string, image []byte) (*http.Request, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if filename != "" {
		part, err := writer.CreateFormFile("receipt", filename)
		if err != nil {
			return nil, fmt.Errorf("create receipt file: %w", err)
		}
		if _, err := part.Write(image); err != nil {
			return nil, fmt.Errorf("write receipt file: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	request, err := http.NewRequest(http.MethodPost, baseURL+"/nota/scan-receipt", &body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	return request, nil
}

func notaCSRF(t *testing.T, client *http.Client, testServer *httptest.Server) string {
	t.Helper()
	return csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/nota"))
}

func TestNotaReceiptScanReturnsEditableItemsWithoutPersisting(t *testing.T) {
	total := int64(36_500)
	scanner := &fakeReceiptScanner{result: receipt.Result{
		Items: []receipt.Item{
			{NamaProduk: "Kertas A4", Satuan: "rim", Volume: 2, Harga: 15_000},
			{NamaProduk: "Pulpen", Satuan: "pcs", Volume: 5, Harga: 1_300},
		},
		TotalTerbaca: &total,
		Warnings:     []string{"Periksa kembali harga Pulpen."},
	}}
	testServer, store := newReceiptScanServer(t, scanner)
	client := loggedInClient(t, testServer)
	csrf := notaCSRF(t, client, testServer)
	image := testJPEG(t)

	if got := len(store.NotaList()); got != 0 {
		t.Fatalf("unexpected nota before scan: %d", got)
	}
	response := postReceiptScan(t, client, testServer, csrf, "receipt.jpg", image)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("scan status: %d, body: %s", response.StatusCode, readBody(t, response))
	}
	if got := response.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("scan content type: %q", got)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("scan cache control: %q", got)
	}
	defer response.Body.Close()
	var payload struct {
		OK           bool           `json:"ok"`
		Items        []receipt.Item `json:"items"`
		TotalTerbaca *int64         `json:"total_terbaca"`
		Warnings     []string       `json:"warnings"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode scan response: %v", err)
	}
	if !payload.OK || len(payload.Items) != 2 {
		t.Fatalf("unexpected scan payload: %+v", payload)
	}
	if payload.Items[0].NamaProduk != "Kertas A4" || payload.Items[0].Harga != 15_000 || payload.Items[0].Volume != 2 {
		t.Fatalf("first editable item changed: %+v", payload.Items[0])
	}
	if payload.TotalTerbaca == nil || *payload.TotalTerbaca != total || len(payload.Warnings) != 1 {
		t.Fatalf("scan metadata changed: total=%v warnings=%v", payload.TotalTerbaca, payload.Warnings)
	}
	if got := len(store.NotaList()); got != 0 {
		t.Fatalf("scan persisted %d nota before form submission", got)
	}

	calls, dataURL := scanner.snapshot()
	if calls != 1 {
		t.Fatalf("scanner calls: %d", calls)
	}
	const prefix = "data:image/jpeg;base64,"
	if !strings.HasPrefix(dataURL, prefix) {
		t.Fatalf("scanner did not receive a JPEG data URL: %.40q", dataURL)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, prefix))
	if err != nil {
		t.Fatalf("decode scanner data URL: %v", err)
	}
	decodedImage, err := jpeg.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("scanner did not receive a valid JPEG: %v", err)
	}
	width, height := decodedImage.Bounds().Dx(), decodedImage.Bounds().Dy()
	if len(decoded) == 0 || width <= 0 || height <= 0 || width > 4096 || height > 4096 {
		t.Fatalf("scanner received unreasonable image data: bytes=%d dimensions=%dx%d", len(decoded), width, height)
	}
}

func TestNotaReceiptScanWithoutScannerKeepsManualNotaAvailable(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/nota")
	if !strings.Contains(page, `name="item_nama"`) || !strings.Contains(page, `name="foto_kwitansi"`) {
		t.Fatal("manual Nota form is unavailable without an AI scanner")
	}

	response := postReceiptScan(t, client, testServer, csrfFromForm(t, page), "receipt.jpg", testJPEG(t))
	body := readBody(t, response)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("scan status without scanner: %d, body: %s", response.StatusCode, body)
	}
	if strings.Contains(body, "MIMO_API_KEY") {
		t.Fatalf("configuration secret name leaked in response: %s", body)
	}

	page = fetchAuthedPage(t, client, testServer.URL+"/nota")
	if !strings.Contains(page, `name="item_nama"`) {
		t.Fatal("manual Nota form stopped working after unavailable scan")
	}
}

func TestNotaReceiptScanControlsPreserveManualFallback(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/nota")

	for _, fragment := range []string{
		`data-receipt-scan data-scan-enabled="false"`,
		`data-receipt-file`,
		`data-receipt-scan-button disabled`,
		`role="status" aria-live="polite" data-receipt-scan-status`,
		"Anda tetap dapat memilih foto dan mengisi item secara manual",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("disabled scan UI is missing %q", fragment)
		}
	}
	if count := strings.Count(page, `name="foto_kwitansi"`); count != 1 {
		t.Fatalf("receipt upload inputs = %d, want exactly one", count)
	}
	if scanAt, listAt := strings.Index(page, `data-receipt-scan`), strings.Index(page, `data-item-list`); scanAt < 0 || listAt < 0 || scanAt > listAt {
		t.Fatal("receipt scanner is not placed before the editable item list")
	}

	script := fetchPage(t, testServer.URL+"/static/js/nota.js")
	for _, fragment := range []string{
		`fetch("/nota/scan-receipt"`,
		`"X-CSRF-Token"`,
		"replaceRowsFromReceipt",
		"window.confirm",
		"list.replaceChildren(...rows)",
		`nama.value = String(item.nama_produk`,
		`[data-item-row] input, [data-add-item], [data-remove-item]`,
		`control.disabled = busy`,
		`Foto siap. Tekan “Scan struk dengan AI”`,
		`Foto dan daftar item yang sudah Anda isi tetap dipertahankan`,
		`warnings.join(" • ")`,
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("receipt scan script is missing %q", fragment)
		}
	}
	if strings.Contains(script, "innerHTML") {
		t.Fatal("receipt scan script inserts untrusted AI output as HTML")
	}
	if strings.Contains(script, `receiptFile.value = ""`) {
		t.Fatal("cancelled scan clears the selected receipt")
	}

	enabledServer, _ := newReceiptScanServer(t, &fakeReceiptScanner{})
	enabledClient := loggedInClient(t, enabledServer)
	enabledPage := fetchAuthedPage(t, enabledClient, enabledServer.URL+"/nota")
	if !strings.Contains(enabledPage, `data-receipt-scan data-scan-enabled="true"`) || strings.Contains(enabledPage, `data-receipt-scan-button disabled`) {
		t.Fatal("scan control is not enabled when a scanner is configured")
	}
}

func TestNotaReceiptScanRejectsBadUploadBeforeScanner(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		image    []byte
	}{
		{name: "missing file"},
		{name: "invalid image", filename: "receipt.jpg", image: []byte("MIMO_API_KEY=must-not-be-reflected")},
		{name: "oversize", filename: "receipt.jpg", image: bytes.Repeat([]byte{0x7f}, int(photo.MaxInputBytes)+1)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scanner := &fakeReceiptScanner{}
			testServer, _ := newReceiptScanServer(t, scanner)
			client := loggedInClient(t, testServer)
			response := postReceiptScan(t, client, testServer, notaCSRF(t, client, testServer), test.filename, test.image)
			body := readBody(t, response)
			if response.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status: %d, body: %s", response.StatusCode, body)
			}
			if calls, _ := scanner.snapshot(); calls != 0 {
				t.Fatalf("scanner called %d times for rejected upload", calls)
			}
			if strings.Contains(body, "must-not-be-reflected") || strings.Contains(body, "MIMO_API_KEY") {
				t.Fatalf("rejected image content leaked in response: %s", body)
			}
		})
	}
}

func TestNotaReceiptScanRequiresAuthentication(t *testing.T) {
	scanner := &fakeReceiptScanner{}
	testServer, _ := newReceiptScanServer(t, scanner)

	response := postReceiptScan(t, testServer.Client(), testServer, "", "receipt.jpg", testJPEG(t))
	body := readBody(t, response)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status: %d, body: %s", response.StatusCode, body)
	}
	if calls, _ := scanner.snapshot(); calls != 0 {
		t.Fatalf("scanner called %d times for anonymous request", calls)
	}
}

func TestNotaReceiptScanRequiresValidCSRF(t *testing.T) {
	scanner := &fakeReceiptScanner{}
	testServer, _ := newReceiptScanServer(t, scanner)
	client := loggedInClient(t, testServer)
	_ = notaCSRF(t, client, testServer)

	for _, test := range []struct {
		name string
		csrf string
	}{
		{name: "missing"},
		{name: "wrong", csrf: "not-the-session-token"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := postReceiptScan(t, client, testServer, test.csrf, "receipt.jpg", testJPEG(t))
			body := readBody(t, response)
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("status: %d, body: %s", response.StatusCode, body)
			}
		})
	}
	if calls, _ := scanner.snapshot(); calls != 0 {
		t.Fatalf("scanner called %d times with invalid CSRF", calls)
	}
}

func TestNotaReceiptScanRejectsUnauthorizedRole(t *testing.T) {
	scanner := &fakeReceiptScanner{}
	testServer, _ := newReceiptScanServer(t, scanner)
	client := loggedInClientAs(t, testServer, "Produksi")
	csrf := csrfFromBody(t, fetchAuthedPage(t, client, testServer.URL+"/absensi"))

	response := postReceiptScan(t, client, testServer, csrf, "receipt.jpg", testJPEG(t))
	body := readBody(t, response)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthorized role status: %d, body: %s", response.StatusCode, body)
	}
	if calls, _ := scanner.snapshot(); calls != 0 {
		t.Fatalf("scanner called %d times for unauthorized role", calls)
	}
}

func TestNotaReceiptScanMapsScannerErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "no items", err: receipt.ErrNoItems, status: http.StatusUnprocessableEntity},
		{name: "invalid response", err: receipt.ErrInvalidResponse, status: http.StatusBadGateway},
		{name: "unavailable", err: receipt.ErrUnavailable, status: http.StatusServiceUnavailable},
		{name: "rate limited", err: receipt.ErrRateLimited, status: http.StatusServiceUnavailable},
		{name: "upstream rejected", err: receipt.ErrUpstream, status: http.StatusBadGateway},
		{name: "timeout", err: receipt.ErrTimeout, status: http.StatusGatewayTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scanner := &fakeReceiptScanner{err: fmt.Errorf("scanner detail must stay private: %w", test.err)}
			testServer, store := newReceiptScanServer(t, scanner)
			client := loggedInClient(t, testServer)

			response := postReceiptScan(t, client, testServer, notaCSRF(t, client, testServer), "receipt.jpg", testJPEG(t))
			body := readBody(t, response)
			if response.StatusCode != test.status {
				t.Fatalf("status: %d, want %d, body: %s", response.StatusCode, test.status, body)
			}
			if strings.Contains(body, "scanner detail must stay private") {
				t.Fatalf("scanner error leaked in response: %s", body)
			}
			if calls, _ := scanner.snapshot(); calls != 1 {
				t.Fatalf("scanner calls: %d", calls)
			}
			if got := len(store.NotaList()); got != 0 {
				t.Fatalf("failed scan persisted %d nota", got)
			}
		})
	}
}

func TestNotaReceiptScanDoesNotLogOrReturnSecretAndImage(t *testing.T) {
	const secret = "mimo-api-key-super-secret"
	scanner := &fakeReceiptScanner{scanFn: func(_ context.Context, imageDataURL string) (receipt.Result, error) {
		return receipt.Result{}, fmt.Errorf("provider rejected api-key=%s image=%s", secret, imageDataURL)
	}}
	testServer, _ := newReceiptScanServer(t, scanner)
	client := loggedInClient(t, testServer)
	csrf := notaCSRF(t, client, testServer)

	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })

	response := postReceiptScan(t, client, testServer, csrf, "receipt.jpg", testJPEG(t))
	body := readBody(t, response)
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status: %d, body: %s", response.StatusCode, body)
	}
	for outputName, output := range map[string]string{"response": body, "log": logs.String()} {
		if strings.Contains(output, secret) || strings.Contains(output, "data:image/") || strings.Contains(output, "api-key=") {
			t.Fatalf("%s leaked scanner credential or receipt image: %s", outputName, output)
		}
	}
}

func TestNotaReceiptScanRateLimitsPerUserAfterAccessChecks(t *testing.T) {
	scanner := &fakeReceiptScanner{result: receipt.Result{Items: []receipt.Item{{
		NamaProduk: "Kertas A4",
		Satuan:     "rim",
		Volume:     1,
		Harga:      50_000,
	}}}}
	testServer, _ := newReceiptScanServer(t, scanner)
	client := loggedInClient(t, testServer)
	csrf := notaCSRF(t, client, testServer)
	image := testJPEG(t)

	// Rejected requests must not consume the authenticated user's scan quota.
	for i := 0; i < receiptScanRateLimit+1; i++ {
		response := postReceiptScan(t, client, testServer, "invalid-csrf", "receipt.jpg", image)
		body := readBody(t, response)
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("invalid CSRF request %d status: %d, body: %s", i+1, response.StatusCode, body)
		}
	}

	for i := 0; i < receiptScanRateLimit; i++ {
		response := postReceiptScan(t, client, testServer, csrf, "receipt.jpg", image)
		body := readBody(t, response)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("allowed scan %d status: %d, body: %s", i+1, response.StatusCode, body)
		}
	}

	response := postReceiptScan(t, client, testServer, csrf, "receipt.jpg", image)
	body := readBody(t, response)
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("rate-limited status: %d, body: %s", response.StatusCode, body)
	}
	if retryAfter, err := strconv.Atoi(response.Header.Get("Retry-After")); err != nil || retryAfter < 1 {
		t.Fatalf("invalid Retry-After header %q", response.Header.Get("Retry-After"))
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "application/json") || !strings.Contains(body, `"ok":false`) {
		t.Fatalf("rate-limit response is not JSON: content-type=%q body=%s", response.Header.Get("Content-Type"), body)
	}
	if calls, _ := scanner.snapshot(); calls != receiptScanRateLimit {
		t.Fatalf("scanner calls: %d, want %d", calls, receiptScanRateLimit)
	}
}

func TestNotaReceiptScanRejectsWhenGlobalScannerSlotsAreFull(t *testing.T) {
	entered := make(chan struct{}, receiptScanConcurrentLimit)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	scanner := &fakeReceiptScanner{scanFn: func(ctx context.Context, _ string) (receipt.Result, error) {
		select {
		case entered <- struct{}{}:
		case <-ctx.Done():
			return receipt.Result{}, ctx.Err()
		}
		select {
		case <-release:
			return receipt.Result{Items: []receipt.Item{{NamaProduk: "Pulpen", Satuan: "pcs", Volume: 1, Harga: 5_000}}}, nil
		case <-ctx.Done():
			return receipt.Result{}, ctx.Err()
		}
	}}
	testServer, _ := newReceiptScanServer(t, scanner)
	client := loggedInClient(t, testServer)
	csrf := notaCSRF(t, client, testServer)
	image := testJPEG(t)

	type requestResult struct {
		response *http.Response
		err      error
	}
	results := make(chan requestResult, receiptScanConcurrentLimit)
	for i := 0; i < receiptScanConcurrentLimit; i++ {
		request, err := newReceiptScanRequest(testServer.URL, csrf, "receipt.jpg", image)
		if err != nil {
			t.Fatalf("new concurrent request %d: %v", i+1, err)
		}
		go func(request *http.Request) {
			response, err := client.Do(request)
			results <- requestResult{response: response, err: err}
		}(request)
	}

	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for i := 0; i < receiptScanConcurrentLimit; i++ {
		select {
		case <-entered:
		case <-deadline.C:
			t.Fatalf("only %d of %d scans entered the blocking scanner", i, receiptScanConcurrentLimit)
		}
	}

	busyRequest, err := newReceiptScanRequest(testServer.URL, csrf, "receipt.jpg", image)
	if err != nil {
		t.Fatalf("new busy request: %v", err)
	}
	busyResponse, err := client.Do(busyRequest)
	if err != nil {
		t.Fatalf("busy request did not return immediately: %v", err)
	}
	busyBody := readBody(t, busyResponse)
	if busyResponse.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("busy status: %d, body: %s", busyResponse.StatusCode, busyBody)
	}
	if busyResponse.Header.Get("Retry-After") != "1" {
		t.Fatalf("busy Retry-After header: %q", busyResponse.Header.Get("Retry-After"))
	}

	releaseOnce.Do(func() { close(release) })
	for i := 0; i < receiptScanConcurrentLimit; i++ {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf("concurrent scan %d: %v", i+1, result.err)
			}
			body := readBody(t, result.response)
			if result.response.StatusCode != http.StatusOK {
				t.Fatalf("concurrent scan %d status: %d, body: %s", i+1, result.response.StatusCode, body)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("concurrent scan %d did not finish after release", i+1)
		}
	}
	if calls, _ := scanner.snapshot(); calls != receiptScanConcurrentLimit {
		t.Fatalf("scanner calls: %d, want %d", calls, receiptScanConcurrentLimit)
	}
}

func TestReceiptScanRateStateIsBoundedAndCleansExpiredWindows(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	server := &Server{
		now:          func() time.Time { return now },
		receiptRates: make(map[string]receiptScanRateEntry),
	}
	for i := 0; i < receiptScanRateMaxUsers+100; i++ {
		if allowed, _ := server.allowReceiptScan(fmt.Sprintf("user-%d", i)); !allowed {
			t.Fatalf("new user %d was unexpectedly rate limited", i)
		}
	}
	if got := len(server.receiptRates); got != receiptScanRateMaxUsers {
		t.Fatalf("rate state size: %d, want %d", got, receiptScanRateMaxUsers)
	}

	now = now.Add(2 * receiptScanRateWindow)
	if allowed, _ := server.allowReceiptScan("fresh-user"); !allowed {
		t.Fatal("fresh user was unexpectedly rate limited")
	}
	if got := len(server.receiptRates); got != 1 {
		t.Fatalf("expired rate state was not cleaned: %d entries remain", got)
	}
}

func TestNotaReceiptScanRejectsGET(t *testing.T) {
	scanner := &fakeReceiptScanner{}
	testServer, _ := newReceiptScanServer(t, scanner)

	response, err := testServer.Client().Get(testServer.URL + "/nota/scan-receipt")
	if err != nil {
		t.Fatalf("get receipt scan: %v", err)
	}
	body := readBody(t, response)
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status: %d, body: %s", response.StatusCode, body)
	}
	if response.Header.Get("Allow") != http.MethodPost {
		t.Fatalf("Allow header: %q", response.Header.Get("Allow"))
	}
	if calls, _ := scanner.snapshot(); calls != 0 {
		t.Fatalf("scanner called %d times for GET", calls)
	}
}
