package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// fakeGrid answers the few Sheets calls the photo copy makes, over sheets
// held as rows of strings.
type fakeGrid struct {
	mu     sync.Mutex
	sheets map[string][][]string
}

var a1 = regexp.MustCompile(`^'?(.*?)'?!([A-Z]+)(\d*)(?::([A-Z]+)(\d*))?$`)

func column(letters string) int {
	n := 0
	for _, c := range letters {
		n = n*26 + int(c-'A'+1)
	}
	return n - 1
}

func (g *fakeGrid) parse(t *testing.T, raw string) (string, int, int, int, int) {
	t.Helper()
	m := a1.FindStringSubmatch(strings.ReplaceAll(raw, "''", "'"))
	if m == nil {
		t.Fatalf("range %q", raw)
	}
	fromRow, toRow := 1, 1<<20
	if m[3] != "" {
		fromRow, _ = strconv.Atoi(m[3])
		toRow = fromRow
	}
	toCol := m[2]
	if m[4] != "" {
		toCol = m[4]
	}
	if m[5] != "" {
		toRow, _ = strconv.Atoi(m[5])
	}
	return m[1], column(m[2]), column(toCol), fromRow, toRow
}

func (g *fakeGrid) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		path, _ := url.PathUnescape(r.URL.EscapedPath())
		switch {
		case r.Method == http.MethodGet && strings.Contains(path, "/values/"):
			name, fromCol, toCol, fromRow, toRow := g.parse(t, path[strings.Index(path, "/values/")+len("/values/"):])
			var out [][]interface{}
			for i, row := range g.sheets[name] {
				if i+1 < fromRow || i+1 > toRow {
					continue
				}
				var cells []interface{}
				for c := fromCol; c <= toCol && c < len(row); c++ {
					cells = append(cells, row[c])
				}
				out = append(out, cells)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"values": out})
		case r.Method == http.MethodPost && strings.HasSuffix(path, "values:batchUpdate"):
			var request sheets.BatchUpdateValuesRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode: %v", err)
			}
			for _, data := range request.Data {
				name, fromCol, _, fromRow, _ := g.parse(t, data.Range)
				for len(g.sheets[name]) < fromRow {
					g.sheets[name] = append(g.sheets[name], nil)
				}
				row := g.sheets[name][fromRow-1]
				for c, value := range data.Values[0] {
					for len(row) <= fromCol+c {
						row = append(row, "")
					}
					row[fromCol+c] = value.(string)
				}
				g.sheets[name][fromRow-1] = row
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		default:
			t.Fatalf("unexpected %s %s", r.Method, path)
		}
	}
}

// The photos of the older sheet land on the A2B Baru rows with the same
// transaction number, in the two photo columns, and a row that has one keeps it.
func TestPhotosAreCopiedByTransactionNumber(t *testing.T) {
	grid := &fakeGrid{sheets: map[string][][]string{
		fuelKeluarSheet: {
			{"fuel_out_id"},
			{"FUELOUT-1", "", "", "", "", "", "", "", "", "data:image/jpeg;base64,AAA"},
			{"FUELOUT-2", "", "", "", "", "", "", "", "", "data:image/jpeg;base64,BBB"},
			{"FUELOUT-3", "", "", "", "", "", "", "", "", "data:image/jpeg;base64,CCC"},
		},
		a2bBaruFuelKeluarSheet: {
			a2bBaruFuelKeluarHeaders,
			{"LAMA-0001"},
			{"FUELOUT-2"},
			{"FUELOUT-3", "", "", "", "", "", "", "", "", "", "", "", "", "", "ya", "data:image/jpeg;base64,KEEP"},
			{"FUELOUT-1"},
		},
	}}
	server := httptest.NewServer(grid.handler(t))
	defer server.Close()
	service, err := sheets.NewService(context.Background(), option.WithEndpoint(server.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	store := NewGoogleSheetsRepository(service, "sheet", time.UTC)

	photos, err := store.FuelKeluarPhotosByID(context.Background())
	if err != nil || len(photos) != 3 || photos["FUELOUT-2"] != "data:image/jpeg;base64,BBB" {
		t.Fatalf("photos = %v, %v", photos, err)
	}
	written, err := store.FillA2BBaruFuelKeluarPhotos(context.Background(), photos)
	if err != nil || written != 2 {
		t.Fatalf("written %d, %v", written, err)
	}
	rows := grid.sheets[a2bBaruFuelKeluarSheet]
	cell := func(row, col int) string {
		if col < len(rows[row]) {
			return rows[row][col]
		}
		return ""
	}
	if cell(1, 15) != "" || cell(2, 14) != "ya" || cell(2, 15) != "data:image/jpeg;base64,BBB" ||
		cell(3, 15) != "data:image/jpeg;base64,KEEP" || cell(4, 15) != "data:image/jpeg;base64,AAA" {
		t.Fatalf("rows = %q", rows)
	}
	fills, err := store.ListA2BBaruFuelKeluar(context.Background())
	if err != nil || len(fills) != 4 || fills[0].HasFoto || !fills[1].HasFoto || !fills[3].HasFoto {
		t.Fatalf("listed = %+v, %v", fills, err)
	}
}
