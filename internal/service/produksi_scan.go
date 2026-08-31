package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/tally"
)

// ErrScanDuplicate means this exact image has already been turned into rows.
var ErrScanDuplicate = errors.New("lembar ini sudah pernah discan")

// The reasons a row cannot be stored. They are shown to the person holding the
// paper, so each says what to do about it rather than what went wrong.
const (
	alasanNopolKosong = "Nopol wajib diisi"
	alasanNopolFormat = "Format nopol harus seperti B 1234 ABC"
	alasanNopol       = "Nopol belum terdaftar di Unit DT"
	alasanTT          = "TT tidak boleh minus"
)

// scanPhotoMaxChars keeps the archived sheet inside one spreadsheet cell.
const scanPhotoMaxChars = 45000

// ScanRow is one line of a read sheet after the register and the option lists
// have had their say. Alasan is empty when the row can be stored, and holds the
// reason it cannot when it holds anything.
//
// The same shape travels out to the browser and back again. What comes back is
// re-judged from scratch: it is a convenience for the page, never a claim the
// server acts on.
type ScanRow struct {
	Nomor    int     `json:"no"`
	Project  string  `json:"project"`
	Supplier string  `json:"supplier"`
	Quary    string  `json:"quary"`
	Kategori string  `json:"kategori"`
	Lokasi   string  `json:"lokasi"`
	Layer    string  `json:"layer"`
	Nopol    string  `json:"nopol"`
	TT       float64 `json:"tt"`
	Alasan   string  `json:"alasan,omitempty"`
}

// Storable reports whether this row would be written.
func (r ScanRow) Storable() bool { return r.Alasan == "" }

// ScanPreview is one sheet judged but not yet written.
type ScanPreview struct {
	Rows     []ScanRow `json:"rows"`
	Siap     int       `json:"siap"`
	Ditolak  int       `json:"ditolak"`
	Warnings []string  `json:"warnings,omitempty"`
}

// ScanResult is what a commit did.
type ScanResult struct {
	Tersimpan int
	Dilewati  []ScanRow
}

// PrepareScan judges a read sheet without writing anything: it looks each
// plate up in the register and adopts the spelling the option lists already
// use. The date is not among its concerns - it is typed once when the sheet is
// confirmed.
func (s *ProduksiService) PrepareScan(ctx context.Context, sheet tally.Sheet) (ScanPreview, error) {
	rows, err := s.judgeScanRows(ctx, sheet.Rows)
	if err != nil {
		return ScanPreview{}, err
	}
	preview := ScanPreview{Rows: rows, Warnings: sheet.Warnings}
	for _, row := range rows {
		if row.Storable() {
			preview.Siap++
			continue
		}
		preview.Ditolak++
	}
	return preview, nil
}

// judgeScanRows is the whole judgement, and both the preview and the commit go
// through it. Two paths would eventually disagree, and the one that disagreed
// silently would be the one that writes.
func (s *ProduksiService) judgeScanRows(ctx context.Context, rows []tally.Row) ([]ScanRow, error) {
	options, err := s.Options(ctx)
	if err != nil {
		return nil, err
	}
	units, err := s.Units(ctx)
	if err != nil {
		return nil, err
	}
	registered := make(map[string]model.UnitDT, len(units))
	for _, unit := range units {
		if key, err := NormalizeNopol(unit.Nopol); err == nil {
			registered[key] = unit
		}
	}

	judged := make([]ScanRow, 0, len(rows))
	for _, row := range orderedBySheet(rows) {
		judged = append(judged, judgeScanRow(row, options, registered))
	}
	return judged, nil
}

// orderedBySheet puts the lines back the way the page has them. The No column
// is what says which line is which, and a reading handed back out of order
// would be filed out of order: the produksi ids are dealt down the list, so the
// sequence would stop following the paper.
//
// Numbering that is missing or repeated says nothing about the order, so in
// that case the order the page was read in stands. Shuffling by a key that does
// not mean anything is worse than leaving it alone.
func orderedBySheet(rows []tally.Row) []tally.Row {
	seen := make(map[int]bool, len(rows))
	for _, row := range rows {
		if row.Nomor <= 0 || seen[row.Nomor] {
			return rows
		}
		seen[row.Nomor] = true
	}

	ordered := make([]tally.Row, len(rows))
	copy(ordered, rows)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Nomor < ordered[j].Nomor })
	return ordered
}

func judgeScanRow(row tally.Row, options ProduksiOptions, registered map[string]model.UnitDT) ScanRow {
	judged := ScanRow{
		Nomor:    row.Nomor,
		Project:  settleOption(row.Project, options.Project),
		Supplier: settleOption(row.Supplier, options.Supplier),
		Quary:    settleOption(row.Quary, options.Quary),
		Kategori: settleOption(row.Kategori, options.Kategori),
		Lokasi:   settleOption(row.Lokasi, options.Lokasi),
		Layer:    settleOption(row.Layer, options.Layer),
		Nopol:    strings.TrimSpace(row.Nopol),
		TT:       row.TT,
	}
	// A cell the reader could not make sense of is that line's problem, and the
	// reason it gave is the one worth showing: it names the column.
	if reason := strings.TrimSpace(row.Alasan); reason != "" {
		judged.Alasan = reason
		return judged
	}
	// The height is corrected by hand on the dialog, so it arrives from a
	// browser and is checked here rather than taken on trust. The entry form
	// refuses a negative dimension for the same reason: it would shrink the load
	// below the bed that carried it.
	if math.IsNaN(judged.TT) || math.IsInf(judged.TT, 0) || judged.TT < 0 {
		judged.Alasan = alasanTT
		return judged
	}
	// A plate the wrong shape and a plate nobody registered are different
	// problems with different answers: one is corrected here, the other is
	// corrected in the Unit DT register. Reporting both as "not registered" sent
	// the operator looking for something that was never going to be there.
	if judged.Nopol == "" {
		judged.Alasan = alasanNopolKosong
		return judged
	}
	key, err := NormalizeNopol(judged.Nopol)
	if err != nil {
		// Left as typed, so what needs correcting is on the screen.
		judged.Alasan = alasanNopolFormat
		return judged
	}
	unit, known := registered[key]
	if !known {
		judged.Nopol = key
		judged.Alasan = alasanNopol
		return judged
	}
	// The register spells the plate. The paper only points at it, and two
	// spellings of one truck would split its production in every report.
	judged.Nopol = unit.Nopol
	return judged
}

// settleOption adopts the spelling an option list already uses.
//
// Unlike the entry form it never refuses an empty cell. A sheet may carry only
// a date, a plate and a top-up height, and that is enough to compute the load:
// the dimensions come from the register and the volume from those. Refusing the
// row would throw away a load that was actually hauled over columns nobody
// filled in, so a blank column is stored blank and shows up as blank.
func settleOption(value string, options []string) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}
	for _, option := range options {
		if strings.EqualFold(option, value) {
			return option
		}
	}
	return value
}

// ScanCommit is one confirmed sheet: the rows as the page showed them, the
// photograph they were read from, and the two things the reader is not asked
// for.
//
// Tanggal is typed rather than read. The column is too often left blank on the
// paper, and a page that arrived without one used to be discovered at the last
// step, after the reading was already paid for. One sheet is one day.
//
// Supplier is typed when the whole page belongs to one vendor, which is the
// usual case and is quicker than writing it on every line. Left empty it
// changes nothing, and whatever the paper carried per row stands.
type ScanCommit struct {
	Rows []ScanRow
	Foto []byte
	// Project is stamped by the caller from the project this request is working
	// in. The paper has a column for it, but the sheet these rows are going into
	// already settles the answer, so what the paper says is not consulted.
	Project  string
	Tanggal  string
	Supplier string
}

// CommitScan writes every storable row in one append and logs the sheet.
//
// The rows arrive from a browser, so they are judged again from the register
// rather than believed. What the browser is genuinely needed for is the photo,
// and it is taken as uploaded bytes: fingerprinting anything else would mean
// re-encoding the picture first, and two encoders of the same photograph do not
// agree on a byte.
func (s *ProduksiService) CommitScan(ctx context.Context, user *model.User, commit ScanCommit) (ScanResult, error) {
	if user == nil {
		return ScanResult{}, fmt.Errorf("%w: pengguna tidak dikenal", ErrValidation)
	}
	rows, raw := commit.Rows, commit.Foto
	if len(rows) == 0 {
		return ScanResult{}, fmt.Errorf("%w: tidak ada baris untuk disimpan", ErrValidation)
	}
	// The date is checked before the photograph is fingerprinted. A sheet
	// refused for a missing date must stay fileable once the date is supplied,
	// and a fingerprint written on the way past would have locked it out.
	tanggal := strings.TrimSpace(commit.Tanggal)
	if tanggal == "" {
		return ScanResult{}, fmt.Errorf("%w: tanggal wajib diisi", ErrValidation)
	}
	if _, err := time.Parse("2006-01-02", tanggal); err != nil {
		return ScanResult{}, fmt.Errorf("%w: tanggal wajib valid", ErrValidation)
	}

	if len(raw) == 0 {
		return ScanResult{}, fmt.Errorf("%w: foto lembar tidak terbaca", ErrValidation)
	}
	digest := sha256.Sum256(raw)
	sidik := hex.EncodeToString(digest[:])

	existing, err := s.store.FindProduksiScan(ctx, sidik)
	if err != nil {
		return ScanResult{}, fmt.Errorf("read produksi scan: %w", err)
	}
	if existing != nil {
		return ScanResult{}, fmt.Errorf("%w pada %s oleh %s", ErrScanDuplicate,
			existing.CreatedAt.In(s.location).Format("02 Jan 2006 15:04"), existing.DibuatOleh)
	}

	// The archive copy is shrunk to fit one spreadsheet cell. It is not what the
	// model read and not what was fingerprinted; it is the picture to look at
	// when a stored figure is disputed.
	archived, err := photo.Normalize(raw, scanPhotoMaxChars)
	if err != nil {
		return ScanResult{}, fmt.Errorf("%w: foto lembar tidak terbaca", ErrValidation)
	}

	read := make([]tally.Row, 0, len(rows))
	for _, row := range rows {
		read = append(read, tally.Row{
			Nomor: row.Nomor, Project: row.Project,
			Supplier: row.Supplier, Quary: row.Quary, Kategori: row.Kategori,
			Lokasi: row.Lokasi, Layer: row.Layer, Nopol: row.Nopol, TT: row.TT,
		})
	}
	judged, err := s.judgeScanRows(ctx, read)
	if err != nil {
		return ScanResult{}, err
	}
	supplier := strings.Join(strings.Fields(commit.Supplier), " ")
	if supplier != "" {
		options, err := s.Options(ctx)
		if err != nil {
			return ScanResult{}, err
		}
		supplier = settleOption(supplier, options.Supplier)
	}

	units, err := s.Units(ctx)
	if err != nil {
		return ScanResult{}, err
	}
	byNopol := make(map[string]model.UnitDT, len(units))
	for _, unit := range units {
		if key, err := NormalizeNopol(unit.Nopol); err == nil {
			byNopol[key] = unit
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().In(s.location)
	highest, err := s.store.MaxProduksiSequence(ctx, produksiIDPrefix(now.Year()))
	if err != nil {
		return ScanResult{}, fmt.Errorf("read last produksi id: %w", err)
	}

	result := ScanResult{}
	prepared := make([]*model.Produksi, 0, len(judged))
	for _, row := range judged {
		if !row.Storable() {
			result.Dilewati = append(result.Dilewati, row)
			continue
		}
		key, err := NormalizeNopol(row.Nopol)
		if err != nil {
			row.Alasan = alasanNopol
			result.Dilewati = append(result.Dilewati, row)
			continue
		}
		unit := byNopol[key]
		tf, volume, volumeOPP, deviasi := Calculate(unit.Panjang, unit.Lebar, unit.Tinggi, row.TT, unit.Keterangan)
		// A vendor typed for the sheet speaks for every line on it. Left blank,
		// whatever the paper carried per row stands.
		rowSupplier := row.Supplier
		if supplier != "" {
			rowSupplier = supplier
		}

		highest++
		prepared = append(prepared, &model.Produksi{
			ProduksiID:  fmt.Sprintf("%s%04d", produksiIDPrefix(now.Year()), highest),
			Tanggal:     tanggal,
			Project:     strings.Join(strings.Fields(commit.Project), " "),
			Supplier:    rowSupplier,
			Quary:       row.Quary,
			Kategori:    row.Kategori,
			Lokasi:      row.Lokasi,
			Layer:       row.Layer,
			UnitID:      unit.UnitID,
			Nopol:       unit.Nopol,
			Driver:      unit.Driver,
			JenisDT:     unit.Keterangan,
			Panjang:     unit.Panjang,
			Lebar:       unit.Lebar,
			Tinggi:      unit.Tinggi,
			TT:          row.TT,
			TF:          round2(tf),
			Volume:      round4(volume),
			VolumeOPP:   volumeOPP,
			Deviasi:     round4(deviasi),
			CreatedBy:   user.NamaLengkap,
			CreatedByID: user.UserID,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}

	if len(prepared) > 0 {
		if err := s.store.CreateProduksiBatch(ctx, prepared); err != nil {
			return ScanResult{}, fmt.Errorf("create produksi batch: %w", err)
		}
		result.Tersimpan = len(prepared)
	}

	// The log is written after the rows, and only for a sheet that produced
	// some. A fingerprint filed for a commit that stored nothing would lock the
	// operator out of the photograph he still has to file.
	if result.Tersimpan > 0 {
		scanHighest, err := s.store.MaxProduksiScanSequence(ctx, produksiScanIDPrefix(now))
		if err != nil {
			return ScanResult{}, fmt.Errorf("read last produksi scan id: %w", err)
		}
		scan := &model.ProduksiScan{
			ScanID:       fmt.Sprintf("%s%04d", produksiScanIDPrefix(now), scanHighest+1),
			Sidik:        sidik,
			BarisMasuk:   result.Tersimpan,
			BarisDitolak: len(result.Dilewati),
			DibuatOleh:   user.NamaLengkap,
			DibuatOlehID: user.UserID,
			CreatedAt:    now,
			Foto:         archived,
		}
		if err := s.store.CreateProduksiScan(ctx, scan); err != nil {
			return ScanResult{}, fmt.Errorf("create produksi scan: %w", err)
		}
	}

	// A value read off the paper has to be offered by the very next scan, or the
	// same spelling gets stored twice under two names.
	s.invalidateOptions()
	return result, nil
}

func produksiScanIDPrefix(now time.Time) string {
	return "SCN-" + now.Format("20060102") + "-"
}
