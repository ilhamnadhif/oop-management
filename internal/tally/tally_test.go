package tally

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScanReadsTheRowsOffThePage(t *testing.T) {
	t.Parallel()

	server := completionServer(t, func() string {
		return `{"rows":[
			{"no":1,"project":" PCPM ","supplier":"HPP","quary":"Q1","kategori":"Tanah","lokasi":"Segmen 1a","layer":"L2","nopol":" b 1234 ab ","tt":0.2},
			{"no":2,"project":"PCPM","supplier":"HPP","quary":"Q1","kategori":"Tanah","lokasi":"Segmen 1a","layer":"L2","nopol":"B 5678 CD","tt":0}
		],"warnings":[" tulisan baris 2 samar "]}`
	})
	defer server.Close()

	sheet, err := mustScanner(t, server).Scan(context.Background(), testImageDataURL())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(sheet.Rows) != 2 {
		t.Fatalf("rows = %#v", sheet.Rows)
	}
	if sheet.Rows[0].Project != "PCPM" || sheet.Rows[0].Nopol != "b 1234 ab" || sheet.Rows[0].TT != 0.2 {
		t.Fatalf("first row not trimmed as read: %#v", sheet.Rows[0])
	}
	if len(sheet.Warnings) != 1 || sheet.Warnings[0] != "tulisan baris 2 samar" {
		t.Fatalf("warnings = %#v", sheet.Warnings)
	}
}

// A sheet with no rows on it is not a failure of the transport, and saying so
// separately is what lets the page tell the user to retake the photo.
func TestScanReportsAnEmptySheet(t *testing.T) {
	t.Parallel()

	server := completionServer(t, func() string { return `{"rows":[],"warnings":[]}` })
	defer server.Close()

	_, err := mustScanner(t, server).Scan(context.Background(), testImageDataURL())
	if !errors.Is(err, ErrNoRows) {
		t.Fatalf("Scan() error = %v, want %v", err, ErrNoRows)
	}
}

// The shape of the answer is refused outright; the contents are not. A page
// claiming more lines than a page holds is a misread photograph.
func TestScanRejectsAnUnusableAnswer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		reason  string
	}{
		{name: "unknown field", reason: "json-shape",
			content: `{"rows":[{"no":1,"nopol":"B 1 A","tt":0,"catatan":"x"}],"warnings":[]}`},
		{name: "too many rows", reason: "too-many-rows", content: tooManyRows()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := completionServer(t, func() string { return test.content })
			defer server.Close()

			_, err := mustScanner(t, server).Scan(context.Background(), testImageDataURL())
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("Scan() error = %v, want %v", err, ErrInvalidResponse)
			}
			// The keyword is what makes a failure diagnosable from a log without
			// any of the sheet reaching it.
			if got := Reason(err); got != test.reason {
				t.Fatalf("Reason() = %q, want %q", got, test.reason)
			}
		})
	}
}

// A completion cut off at the token budget reads as a shorter table, so it is
// refused rather than parsed - and it says so.
func TestScanRefusesATruncatedCompletion(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"finish_reason": "length", "message": map[string]string{"content": `{"rows":[`}},
			},
		}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	defer server.Close()

	_, err := mustScanner(t, server).Scan(context.Background(), testImageDataURL())
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Scan() error = %v, want %v", err, ErrInvalidResponse)
	}
	if got := Reason(err); got != "finish-length" {
		t.Fatalf("Reason() = %q, want %q", got, "finish-length")
	}
}

// One unusable cell is one line to look at. Refusing the page over it would
// throw away every line that was read correctly.
func TestScanMarksTheRowWithAnUnusableCell(t *testing.T) {
	t.Parallel()

	server := completionServer(t, func() string {
		return `{"rows":[
			{"no":1,"project":"P","supplier":"S","quary":"Q","kategori":"K","lokasi":"L","layer":"Y","nopol":"B 1 A","tt":0},
			{"no":2,"project":"P","supplier":"S","quary":"Q","kategori":"K","lokasi":"L","layer":"Y","nopol":"B 2 B","tt":-1},
			{"no":3,"project":"P","supplier":"S","quary":"Q","kategori":"K","lokasi":"L","layer":"Y","nopol":"B 3 C","tt":0.2}
		],"warnings":[]}`
	})
	defer server.Close()

	sheet, err := mustScanner(t, server).Scan(context.Background(), testImageDataURL())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(sheet.Rows) != 3 {
		t.Fatalf("rows = %#v", sheet.Rows)
	}
	if sheet.Rows[0].Alasan != "" {
		t.Fatalf("a good row was marked: %#v", sheet.Rows[0])
	}
	if sheet.Rows[1].Alasan == "" || sheet.Rows[1].TT != 0 {
		t.Fatalf("a negative top-up height was accepted: %#v", sheet.Rows[1])
	}
	if sheet.Rows[2].Alasan != "" {
		t.Fatalf("a good row was marked: %#v", sheet.Rows[2])
	}
}

// The register keeps its dimensions in centimetres, so a top-up height reads in
// tens rather than in fractions of a metre. The entry form accepts any
// non-negative number, and a reader that refused what the form accepts would
// throw away real loads.
func TestScanKeepsTopUpHeightsTheFormWouldAccept(t *testing.T) {
	t.Parallel()

	server := completionServer(t, func() string {
		return `{"rows":[
			{"no":1,"project":"","supplier":"","quary":"","kategori":"","lokasi":"","layer":"","nopol":"AB 8698 GD","tt":14},
			{"no":2,"project":"","supplier":"","quary":"","kategori":"","lokasi":"","layer":"","nopol":"AD 8590 FG","tt":33}
		],"warnings":[]}`
	})
	defer server.Close()

	sheet, err := mustScanner(t, server).Scan(context.Background(), testImageDataURL())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(sheet.Rows) != 2 {
		t.Fatalf("rows = %#v", sheet.Rows)
	}
	if sheet.Rows[0].TT != 14 || sheet.Rows[1].TT != 33 {
		t.Fatalf("top-up heights = %v, %v", sheet.Rows[0].TT, sheet.Rows[1].TT)
	}
	for _, row := range sheet.Rows {
		if row.Alasan != "" {
			t.Fatalf("a readable row was marked: %#v", row)
		}
	}
	// Every option column was blank on this sheet, and blank is what it says.
	// Filling one in would be inventing a project nobody wrote down.
	if sheet.Rows[0].Project != "" || sheet.Rows[0].Lokasi != "" {
		t.Fatalf("a blank column was filled in: %#v", sheet.Rows[0])
	}
}

// Every word on the paper is data. The prompt has to say so, because the paper
// is the one thing in this request an outsider can write on.
func TestScanSendsTheInjectionGuardAndRoomForAFullSheet(t *testing.T) {
	t.Parallel()

	var system string
	var maxTokens int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
			MaxCompletionTokens int `json:"max_completion_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		maxTokens = payload.MaxCompletionTokens
		if len(payload.Messages) > 0 {
			_ = json.Unmarshal(payload.Messages[0].Content, &system)
		}
		writeCompletion(t, w, `{"rows":[{"no":1,"project":"P","supplier":"S","quary":"Q","kategori":"K","lokasi":"L","layer":"Y","nopol":"B 1 A","tt":0}],"warnings":[]}`)
	}))
	defer server.Close()

	if _, err := mustScanner(t, server).Scan(context.Background(), testImageDataURL()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !strings.Contains(system, "DATA TIDAK TERPERCAYA") {
		t.Fatalf("system prompt missing injection guard: %q", system)
	}
	// A full sheet is two hundred rows of ten columns. A receipt-sized budget
	// would return a truncated table that reads as a shorter one.
	if maxTokens < 16000 {
		t.Fatalf("max_completion_tokens = %d, too small for a full sheet", maxTokens)
	}
}

func tooManyRows() string {
	rows := make([]string, 0, MaxRows+1)
	for index := 0; index <= MaxRows; index++ {
		rows = append(rows, fmt.Sprintf(
			`{"no":%d,"project":"P","supplier":"S","quary":"Q","kategori":"K","lokasi":"L","layer":"Y","nopol":"B 1 A","tt":0}`, index+1))
	}
	return `{"rows":[` + strings.Join(rows, ",") + `],"warnings":[]}`
}

func completionServer(t *testing.T, content func() string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeCompletion(t, w, content())
	}))
}

func writeCompletion(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"choices": []map[string]interface{}{
			{"finish_reason": "stop", "message": map[string]string{"content": content}},
		},
	}); err != nil {
		t.Errorf("encode completion: %v", err)
	}
}

func mustScanner(t *testing.T, server *httptest.Server) *MiMoScanner {
	t.Helper()
	scanner, err := NewMiMoScanner("secret", server.URL+"/v1", "mimo-v2.5-test", server.Client())
	if err != nil {
		t.Fatalf("NewMiMoScanner: %v", err)
	}
	return scanner
}

func testImageDataURL() string {
	return "data:image/jpeg;base64,QUJD"
}
