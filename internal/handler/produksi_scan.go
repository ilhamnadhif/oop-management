package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/service"
	"opp-management/internal/tally"
)

// maxScanRowsPosted bounds what a commit will read back from the browser. It is
// the same page limit the scanner works to, so a request claiming more rows than
// a sheet holds is refused before any of it is judged.
const maxScanRowsPosted = tally.MaxRows

// defaultScanBudget is what the reader gets when nobody configured one. It is
// well past the server's own write deadline, which is why the handler lifts
// that deadline for itself rather than the deadline being raised for every page.
const defaultScanBudget = 150 * time.Second

// scanBudget is how long a sheet read may take, end to end.
func (s *Server) scanBudget() time.Duration {
	if s.scanTimeout > 0 {
		return s.scanTimeout
	}
	return defaultScanBudget
}

// handleProduksiScan reads one photographed tally sheet and returns what it
// appears to say, judged against the register but written nowhere. Nothing
// reaches the repository until the person confirms it and the commit re-judges
// the whole thing.
func (s *Server) handleProduksiScan(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	release, ok := s.beginScanRequest(w, r, "Scan lembar produksi")
	if !ok {
		return
	}
	defer release()

	// A page of ruled lines keeps the model writing for longer than any other
	// request here, and the server's write deadline is set for pages that
	// answer at once. Lifting it for this request alone leaves that limit in
	// place everywhere else. Failing to lift it is not fatal: the read simply
	// ends the way it did before, reported as a timeout.
	budget := s.scanBudget()
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(budget + 30*time.Second)); err != nil {
		log.Printf("produksi scan write deadline: %v", err)
	}
	if err := controller.SetReadDeadline(time.Now().Add(budget)); err != nil {
		log.Printf("produksi scan read deadline: %v", err)
	}

	raw, message, ok := s.readSheetPhoto(w, r)
	if !ok {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"ok": false, "error": message})
		return
	}
	// The raw picture goes to the model, not the shrunk archive copy: a tally
	// grid is dense, and the resize that fits a spreadsheet cell is enough to
	// make handwriting unreadable.
	imageDataURL, err := photo.RawDataURL(raw)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"ok": false, "error": "Gunakan foto berformat JPEG, PNG, atau WebP yang valid."})
		return
	}

	sheet, err := s.tallyScanner.Scan(r.Context(), imageDataURL)
	if err != nil {
		status, message := tallyScanError(err)
		// The keyword comes from a closed set written in the scanner packages, so
		// a failure can be diagnosed without an image, a prompt, a provider
		// response, or a credential ever reaching the log.
		log.Printf("produksi scan failed (status=%d, kind=%T, reason=%q)", status, err, tally.Reason(err))
		writeJSON(w, status, map[string]interface{}{"ok": false, "error": message})
		return
	}

	preview, err := s.produksi.PrepareScan(r.Context(), sheet)
	if err != nil {
		log.Printf("prepare produksi scan: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "error": "Hasil scan belum dapat diperiksa. Silakan coba lagi."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"rows":     preview.Rows,
		"siap":     preview.Siap,
		"ditolak":  preview.Ditolak,
		"warnings": preview.Warnings,
	})
}

// handleProduksiScanCommit stores the rows the person confirmed. It is a form
// post rather than JSON because its answer is a page: the operator is told what
// was stored and what still needs a unit registering.
func (s *Server) handleProduksiScanCommit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// The page guard is GET-only, so this loads the user itself the way every
	// other form post here does.
	sessionValue, ok := s.currentSession(r)
	if !ok {
		redirect(w, r, "/login")
		return
	}
	user, err := s.auth.LoadUser(r.Context(), sessionValue.UserID)
	if err != nil || user.StatusPengguna != model.StatusAktif {
		s.sessions.Delete(r, w)
		redirect(w, r, "/login")
		return
	}
	if !s.allowed(w, user, sessionValue, "produksi-input") {
		return
	}
	// The token travels in the body, because a form submit cannot set a header.
	// So the body is read first - bounded by the size limit inside - and only
	// then is the token checked. ValidCSRF refuses to read a multipart body
	// itself for exactly that reason, which is why the check is written out.
	raw, message, ok := s.readSheetPhoto(w, r)
	if !ok {
		s.renderProduksi(w, r, user, sessionValue, ProduksiFormData{}, message, "", http.StatusUnprocessableEntity)
		return
	}
	if !s.sessions.ValidCSRFToken(r.FormValue("csrf_token"), sessionValue) {
		s.renderProduksi(w, r, user, sessionValue, ProduksiFormData{}, "CSRF token tidak valid", "", http.StatusForbidden)
		return
	}
	rows, rowsErr := decodeScanRows(r.FormValue("rows"))
	if rowsErr != nil {
		s.renderProduksi(w, r, user, sessionValue, ProduksiFormData{}, rowsErr.Error(), "", http.StatusUnprocessableEntity)
		return
	}

	result, err := s.produksi.CommitScan(r.Context(), user, service.ScanCommit{
		Rows: rows, Foto: raw,
		// Typed on the confirmation dialog, not read off the paper.
		Tanggal:  strings.TrimSpace(r.FormValue("tanggal")),
		Supplier: strings.TrimSpace(r.FormValue("supplier")),
	})
	if err != nil {
		status := http.StatusUnprocessableEntity
		text := "Gagal menyimpan hasil scan. Silakan coba lagi."
		switch {
		case errors.Is(err, service.ErrScanDuplicate):
			status = http.StatusConflict
			text = strings.TrimSpace(err.Error())
		case errors.Is(err, service.ErrValidation):
			text = strings.TrimPrefix(err.Error(), "validation error: ")
		default:
			log.Printf("commit produksi scan: %v", err)
			status = http.StatusBadGateway
		}
		s.renderProduksi(w, r, user, sessionValue, ProduksiFormData{}, text, "", status)
		return
	}

	s.renderProduksi(w, r, user, sessionValue, ProduksiFormData{}, "", scanResultMessage(result), http.StatusOK)
}

// scanResultMessage says what was stored and, when something was left behind,
// which plates have to be registered before those rows can be filed.
func scanResultMessage(result service.ScanResult) string {
	message := fmt.Sprintf("%d baris produksi tersimpan dari lembar ini.", result.Tersimpan)
	if len(result.Dilewati) == 0 {
		return message
	}

	seen := map[string]bool{}
	plates := make([]string, 0, len(result.Dilewati))
	for _, row := range result.Dilewati {
		plate := strings.TrimSpace(row.Nopol)
		if plate == "" || seen[plate] {
			continue
		}
		seen[plate] = true
		plates = append(plates, plate)
	}
	message += fmt.Sprintf(" %d baris dilewati.", len(result.Dilewati))
	if len(plates) > 0 {
		message += " Daftarkan dulu di Unit DT: " + strings.Join(plates, ", ") + "."
	}
	return message
}

// beginScanRequest performs every check that must happen before a single byte of
// image is decoded: session, authorisation, CSRF, configuration, rate, and
// concurrency. It returns holding a concurrency slot, which the returned
// function releases.
func (s *Server) beginScanRequest(w http.ResponseWriter, r *http.Request, label string) (func(), bool) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"ok": false, "error": "method not allowed"})
		return nil, false
	}
	sessionValue, ok := s.currentSession(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "error": "Sesi tidak valid. Silakan masuk kembali."})
		return nil, false
	}
	user, err := s.auth.LoadUser(r.Context(), sessionValue.UserID)
	if err != nil || user.StatusPengguna != model.StatusAktif {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "error": "Sesi tidak valid. Silakan masuk kembali."})
		return nil, false
	}
	if !CanAccess(user.Jabatan, "produksi-input") {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"ok": false, "error": "Jabatan Anda tidak berhak mengakses input Produksi."})
		return nil, false
	}
	if !s.sessions.ValidCSRF(r, sessionValue) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"ok": false, "error": "CSRF token tidak valid"})
		return nil, false
	}
	if s.tallyScanner == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"ok": false, "error": label + " belum dikonfigurasi. Anda tetap dapat mengisi form secara manual."})
		return nil, false
	}
	if allowed, retryAfter := s.allowAIScan(sessionValue.UserID); !allowed {
		writeScanLimit(w, retryAfter, "Batas scan tercapai. Silakan coba lagi sebentar.")
		return nil, false
	}
	select {
	case s.scanSlots <- struct{}{}:
	default:
		writeScanLimit(w, time.Second, "Layanan scan sedang memproses permintaan lain. Silakan coba lagi.")
		return nil, false
	}
	return func() { <-s.scanSlots }, true
}

// readSheetPhoto reads exactly one image out of the request, and says in plain
// words what was wrong when it cannot.
func (s *Server) readSheetPhoto(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	maxPhotoBytes := min(s.maxUploadBytes, photo.MaxInputBytes)
	// The rows travel in a form field beside the photo, so the body allowance is
	// the picture plus room for a page of them.
	maxBody := maxPhotoBytes + 512*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		return nil, "Foto lembar tidak valid atau terlalu besar.", false
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	files := r.MultipartForm.File["lembar"]
	fileCount := 0
	for _, headers := range r.MultipartForm.File {
		fileCount += len(headers)
	}
	if len(files) != 1 || fileCount != 1 {
		return nil, "Pilih tepat satu foto lembar.", false
	}
	file, err := files[0].Open()
	if err != nil {
		return nil, "Foto lembar tidak dapat dibaca.", false
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maxPhotoBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, "Foto lembar tidak dapat dibaca.", false
	}
	if len(raw) == 0 || int64(len(raw)) > maxPhotoBytes {
		return nil, fmt.Sprintf("Ukuran foto lembar maksimal %d MB.", maxPhotoBytes/(1024*1024)), false
	}
	return raw, "", true
}

// decodeScanRows reads the confirmed rows back off the form. They are bounded
// and strictly decoded, and then judged again by the service: what comes back
// from a browser is a convenience, never a claim.
func decodeScanRows(payload string) ([]service.ScanRow, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, errors.New("Tidak ada baris untuk disimpan.")
	}
	var rows []service.ScanRow
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rows); err != nil {
		return nil, errors.New("Hasil scan tidak dapat dibaca. Silakan ulangi scan.")
	}
	if len(rows) == 0 {
		return nil, errors.New("Tidak ada baris untuk disimpan.")
	}
	if len(rows) > maxScanRowsPosted {
		return nil, errors.New("Terlalu banyak baris dalam satu lembar.")
	}
	return rows, nil
}

func tallyScanError(err error) (int, string) {
	switch {
	case errors.Is(err, tally.ErrNoRows):
		return http.StatusUnprocessableEntity, "Tidak ada baris yang dapat dibaca dari foto ini. Coba foto yang lebih jelas atau isi manual."
	case errors.Is(err, tally.ErrTimeout):
		return http.StatusGatewayTimeout, "Scan lembar terlalu lama. Silakan coba lagi."
	case errors.Is(err, tally.ErrRateLimited), errors.Is(err, tally.ErrUnavailable):
		return http.StatusServiceUnavailable, "Layanan scan sedang sibuk. Silakan coba lagi sebentar lagi."
	case errors.Is(err, tally.ErrInvalidResponse):
		return http.StatusBadGateway, "Hasil scan belum dapat dibaca dengan aman. Silakan coba lagi atau isi manual."
	default:
		return http.StatusBadGateway, "Scan lembar gagal. Silakan coba lagi atau isi manual."
	}
}
