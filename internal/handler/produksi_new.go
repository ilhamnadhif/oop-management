package handler

import (
	"context"
	"log"
	"net/http"
	"time"

	"opp-management/internal/export"
	"opp-management/internal/widya"
)

// WidyaFeed is what Produksi New reads the weighbridge through. The feed caches,
// so a page refreshing itself does not read the scanner on every tick.
type WidyaFeed interface {
	Latest(ctx context.Context) (widya.Snapshot, bool, error)
	MaxAge() time.Duration
}

// WithWidya enables Produksi New. Like the scanners it is optional: without an
// account the page renders and says the source has not been set up.
func (s *Server) WithWidya(feed WidyaFeed) *Server {
	s.widya = feed
	return s
}

// WidyaStat is one figure off the scanner's dashboard, already written the way
// the page shows it.
type WidyaStat struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Note  string `json:"note"`
	Icon  string `json:"-"`
	Tone  string `json:"-"`
	Value string `json:"value"`
}

// WidyaMaterial is one row of the volume table.
type WidyaMaterial struct {
	Name    string `json:"name"`
	Volume  string `json:"volume"`
	Percent string `json:"percent"`
}

// ProduksiNewView is the figures as both the page and its refresh read them,
// so the first paint and every refresh after it say the same thing.
type ProduksiNewView struct {
	Configured  bool            `json:"configured"`
	Available   bool            `json:"available"`
	UpdatedAt   string          `json:"updatedAt"`
	Error       string          `json:"error"`
	Stats       []WidyaStat     `json:"stats"`
	Materials   []WidyaMaterial `json:"materials"`
	TotalVolume string          `json:"totalVolume"`
}

type ProduksiNewPageData struct {
	ShellPageData
	View ProduksiNewView
	// RefreshMS is how often the page asks again: the feed's own interval, so
	// the page never asks more often than there is anything new to show.
	RefreshMS      int64
	RefreshSeconds int64
}

func (s *Server) handleProduksiNewOverview(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "produksi-new-overview")
	if !ok {
		return
	}
	data := ProduksiNewPageData{
		ShellPageData: s.shellData(user, sessionValue, "produksi-new-overview"),
		View:          s.produksiNewView(r.Context()),
	}
	if s.widya != nil {
		data.RefreshMS = s.widya.MaxAge().Milliseconds()
		data.RefreshSeconds = int64(s.widya.MaxAge().Seconds())
	}
	s.render(w, "produksi_new_overview", data, http.StatusOK)
}

// handleProduksiNewData is the page's refresh: the same view, as JSON.
func (s *Server) handleProduksiNewData(w http.ResponseWriter, r *http.Request) {
	s, _, _, ok := s.requireAccess(w, r, "produksi-new-overview")
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.produksiNewView(r.Context()))
}

func (s *Server) produksiNewView(ctx context.Context) ProduksiNewView {
	if s.widya == nil {
		return ProduksiNewView{}
	}
	view := ProduksiNewView{Configured: true}
	snapshot, has, err := s.widya.Latest(ctx)
	if err != nil {
		log.Printf("read widya: %v", err)
		view.Error = "Widya Load Scanner tidak bisa dibaca saat ini."
		if has {
			view.Error += " Angka di bawah adalah pembacaan terakhir."
		}
	}
	if !has {
		return view
	}
	view.Available = true
	view.UpdatedAt = snapshot.FetchedAt.In(s.location).Format("02 Jan 2006 15:04:05")

	stats := snapshot.Stats
	// Four figures, one row: what crossed the scanner and what it was carried
	// in. The scanner's own bookkeeping - suppliers, operators, users,
	// webhooks - is not what a production page is opened for.
	view.Stats = []WidyaStat{
		{Key: "tickets", Label: "TOTAL TIKET", Note: "Tiket timbang tercatat", Icon: "receipt", Tone: "blue", Value: formatCount(stats.Tickets)},
		{Key: "vehicles", Label: "TOTAL KENDARAAN", Note: "Kendaraan terdaftar", Icon: "truck", Tone: "green", Value: formatCount(stats.Vehicles)},
		{Key: "materials", Label: "TOTAL MATERIAL", Note: "Jenis material", Icon: "layers", Tone: "purple", Value: formatCount(stats.Materials)},
		{Key: "units", Label: "TOTAL UNIT", Note: "Unit scanner", Icon: "cube", Tone: "amber", Value: formatCount(stats.Units)},
	}

	total := 0.0
	for _, material := range snapshot.Materials {
		total += material.Volume
	}
	for _, material := range snapshot.Materials {
		row := WidyaMaterial{Name: material.Name, Volume: export.FormatMoney(material.Volume), Percent: "0"}
		if total > 0 {
			row.Percent = export.FormatFloat(material.Volume/total*100, 1)
		}
		view.Materials = append(view.Materials, row)
	}
	view.TotalVolume = export.FormatMoney(total)
	return view
}

// formatCount groups a count the way the rest of the app writes numbers.
func formatCount(value int) string { return export.FormatMoney(float64(value)) }
