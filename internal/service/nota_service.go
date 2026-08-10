package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
)

// NotaKategori is one expense category and the sub categories it allows. The
// pair is a closed list: an expense filed under a category nobody recognises
// cannot be reported on later.
type NotaKategori struct {
	Nama string
	Sub  []string
}

const (
	NotaKategoriOperasional = "Operasional"
	NotaKategoriUmumADM     = "Umum ADM"

	// NotaSubPerjalananDinas is the one sub category that asks a further
	// question: a trip is either fuel or a ticket, and the two are budgeted
	// separately.
	NotaSubPerjalananDinas = "Perjalanan Dinas"

	// notaMaxItems bounds one nota. A note longer than this is a spreadsheet
	// import, not something typed into a form, and it would write a hundred
	// detail rows on a single submission.
	notaMaxItems = 50
)

var NotaKategoriOptions = []NotaKategori{
	{Nama: NotaKategoriOperasional, Sub: []string{"QHSE", "Material Bantu"}},
	{Nama: NotaKategoriUmumADM, Sub: []string{"Konsumsi", "ATK", NotaSubPerjalananDinas, "Lain-lain"}},
}

var NotaJenisPerjalananOptions = []string{"BBM", "Tiket"}

// ErrNotaNotFound and ErrNotaAlreadyPaid separate "no such nota" from "that one
// is already settled", so finance is told which of the two happened.
var (
	ErrNotaNotFound    = fmt.Errorf("nota not found")
	ErrNotaAlreadyPaid = fmt.Errorf("nota already paid")
)

// NotaMetode pairs the stored value with the words the form shows.
type NotaMetode struct {
	Value string
	Label string
}

var NotaMetodeOptions = []NotaMetode{
	{Value: model.NotaMetodeCA, Label: "CA (Cash Advance)"},
	{Value: model.NotaMetodeReimburse, Label: "Reimburse"},
}

// StatusFor is the payment status a method implies. The form displays it and
// never submits it: the status follows from how the money moved, so letting a
// browser send it would let a reimbursement be filed as already paid.
func StatusFor(metode string) string {
	if strings.EqualFold(strings.TrimSpace(metode), model.NotaMetodeCA) {
		return model.NotaStatusSudahDibayar
	}
	return model.NotaStatusBelumDibayar
}

type NotaService struct {
	store    repository.Store
	location *time.Location
	now      NowFunc
	mu       sync.Mutex

	optionsMu sync.Mutex
	options   NotaOptions
	optionsAt time.Time
}

// NotaOptions is what the PIC picker suggests: the people who have filed a
// nota before. It is a suggestion, not a closed list, because a new name has
// to be typeable the first time that person spends anything.
type NotaOptions struct {
	PIC []string
}

func NewNotaService(store repository.Store, location *time.Location, now NowFunc) *NotaService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &NotaService{store: store, location: location, now: now}
}

func (s *NotaService) Options(ctx context.Context) (NotaOptions, error) {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	if s.optionsAt.After(time.Time{}) && s.now().Sub(s.optionsAt) < optionsCacheTTL {
		return s.options, nil
	}
	notas, err := s.store.ListNota(ctx)
	if err != nil {
		return NotaOptions{}, fmt.Errorf("read nota options: %w", err)
	}
	s.options = NotaOptions{
		PIC: distinctValues(nil, notas, func(n model.Nota) string { return n.PIC }),
	}
	s.optionsAt = s.now()
	return s.options, nil
}

func (s *NotaService) invalidateOptions() {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	s.optionsAt = time.Time{}
}

// Today is the date the form preselects.
func (s *NotaService) Today() string {
	return s.now().In(s.location).Format("2006-01-02")
}

// NextID previews the identifier the next nota would receive. Create assigns
// the authoritative one under the lock, so a stale preview costs nothing.
func (s *NotaService) NextID(ctx context.Context) (string, error) {
	prefix := s.idPrefix()
	highest, err := s.store.MaxNotaSequence(ctx, prefix)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%04d", prefix, highest+1), nil
}

// idPrefix numbers notes within a day, so the identifier says when the expense
// was filed without anyone having to open the row.
func (s *NotaService) idPrefix() string {
	return "NTA-" + s.now().In(s.location).Format("20060102") + "-"
}

// NotaFilter narrows an export. Every field may be empty, and an empty field
// means that axis does not narrow anything. It is a struct rather than three
// string arguments because two of them are dates and one is not, and at a call
// site they would be indistinguishable.
type NotaFilter struct {
	From string
	To   string
	// Metode is a payment method, so a report can cover reimbursements alone
	// without the cash advances they are settled against.
	Metode string
}

// RowsBetween returns the notes the filter selects, each with its lines
// attached, oldest first. Date bounds are inclusive. It also returns the filter
// as it was actually applied, so the page can echo what the file will contain
// rather than what was typed. The photos are left unread: this answers "how
// many rows will the file have", and loading three base64 images per nota to
// count them is wasted work.
func (s *NotaService) RowsBetween(ctx context.Context, filter NotaFilter) ([]model.Nota, NotaFilter, error) {
	return s.rowsBetween(ctx, filter, false)
}

// ExportRowsBetween is the same selection with the receipt and payment photos
// attached, for the report that prints them.
func (s *NotaService) ExportRowsBetween(ctx context.Context, filter NotaFilter) ([]model.Nota, NotaFilter, error) {
	return s.rowsBetween(ctx, filter, true)
}

func (s *NotaService) rowsBetween(ctx context.Context, filter NotaFilter, withAttachments bool) ([]model.Nota, NotaFilter, error) {
	filter.From = strings.TrimSpace(filter.From)
	filter.To = strings.TrimSpace(filter.To)
	if filter.From != "" {
		if _, err := time.Parse("2006-01-02", filter.From); err != nil {
			return nil, NotaFilter{}, fmt.Errorf("%w: tanggal awal tidak valid", ErrValidation)
		}
	}
	if filter.To != "" {
		if _, err := time.Parse("2006-01-02", filter.To); err != nil {
			return nil, NotaFilter{}, fmt.Errorf("%w: tanggal akhir tidak valid", ErrValidation)
		}
	}
	// A reversed range would quietly export nothing, which reads as "no data".
	if filter.From != "" && filter.To != "" && filter.From > filter.To {
		filter.From, filter.To = filter.To, filter.From
	}
	metode, err := notaMetodeFilter(filter.Metode)
	if err != nil {
		return nil, NotaFilter{}, err
	}
	filter.Metode = metode

	read := s.store.ListNota
	if withAttachments {
		read = s.store.ListNotaWithAttachments
	}
	headers, err := read(ctx)
	if err != nil {
		return nil, NotaFilter{}, fmt.Errorf("read nota: %w", err)
	}
	items, err := s.store.ListNotaItems(ctx)
	if err != nil {
		return nil, NotaFilter{}, fmt.Errorf("read nota items: %w", err)
	}
	byNota := make(map[string][]model.NotaItem, len(headers))
	for _, item := range items {
		byNota[item.NotaID] = append(byNota[item.NotaID], item)
	}

	rows := make([]model.Nota, 0, len(headers))
	for _, nota := range headers {
		if filter.From != "" && nota.Tanggal < filter.From {
			continue
		}
		if filter.To != "" && nota.Tanggal > filter.To {
			continue
		}
		// Stored values are canonical, but a row typed straight into the sheet
		// may not be, and a case difference is not a different method.
		if filter.Metode != "" && !strings.EqualFold(strings.TrimSpace(nota.MetodePembayaran), filter.Metode) {
			continue
		}
		lines := byNota[nota.NotaID]
		sort.Slice(lines, func(i, j int) bool { return lines[i].Baris < lines[j].Baris })
		nota.Items = lines
		rows = append(rows, nota)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Tanggal != rows[j].Tanggal {
			return rows[i].Tanggal < rows[j].Tanggal
		}
		return rows[i].NotaID < rows[j].NotaID
	})
	return rows, filter, nil
}

// notaMetodeFilter reads the method a report is narrowed to. Empty means every
// method. An unrecognised one is refused rather than ignored: a filter that
// silently did nothing would hand over the whole set under a heading saying it
// covered one method.
func notaMetodeFilter(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	for _, option := range NotaMetodeOptions {
		if strings.EqualFold(trimmed, option.Value) {
			return option.Value, nil
		}
	}
	return "", fmt.Errorf("%w: metode pembayaran tidak dikenal", ErrValidation)
}

// NotaMetodeLabel is the wording the form and the letterhead use for a stored
// method value.
func NotaMetodeLabel(value string) string {
	for _, option := range NotaMetodeOptions {
		if strings.EqualFold(strings.TrimSpace(value), option.Value) {
			return option.Label
		}
	}
	return strings.TrimSpace(value)
}

// Outstanding lists the reimbursements the company still owes, oldest first,
// optionally narrowed to a transaction number. Cash advances never appear:
// that money left the company before the nota was filed.
func (s *NotaService) Outstanding(ctx context.Context, query string) ([]model.Nota, error) {
	all, err := s.store.ListNota(ctx)
	if err != nil {
		return nil, fmt.Errorf("read nota: %w", err)
	}
	needle := strings.ToUpper(strings.TrimSpace(query))
	rows := make([]model.Nota, 0, len(all))
	for _, nota := range all {
		if nota.MetodePembayaran != model.NotaMetodeReimburse {
			continue
		}
		if nota.StatusPembayaran != model.NotaStatusBelumDibayar {
			continue
		}
		// A partial number is enough: finance reads these off a printed nota
		// and typing the whole identifier is what people get wrong.
		if needle != "" && !strings.Contains(strings.ToUpper(nota.NotaID), needle) {
			continue
		}
		rows = append(rows, nota)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Tanggal != rows[j].Tanggal {
			return rows[i].Tanggal < rows[j].Tanggal
		}
		return rows[i].NotaID < rows[j].NotaID
	})
	return rows, nil
}

// Settle records that the company has paid a reimbursement. The proof of
// payment is required: a status flipped without evidence is what reconciliation
// exists to prevent.
func (s *NotaService) Settle(ctx context.Context, user *model.User, notaID, buktiBayar string) (*model.Nota, error) {
	if user == nil {
		return nil, fmt.Errorf("%w: pengguna tidak dikenal", ErrValidation)
	}
	notaID = strings.ToUpper(strings.TrimSpace(notaID))
	if notaID == "" {
		return nil, fmt.Errorf("%w: nota wajib dipilih", ErrValidation)
	}
	buktiBayar = strings.TrimSpace(buktiBayar)
	if buktiBayar == "" {
		return nil, fmt.Errorf("%w: bukti bayar wajib diunggah", ErrValidation)
	}
	if err := photo.ValidateDataURL(buktiBayar); err != nil {
		return nil, fmt.Errorf("%w: bukti bayar tidak valid", ErrInvalidPhoto)
	}

	// Serialise the read and the write, so two people settling the same nota
	// cannot both record a payment against it.
	s.mu.Lock()
	defer s.mu.Unlock()
	nota, rowNumber, err := s.store.FindNotaRow(ctx, notaID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotaNotFound
		}
		return nil, fmt.Errorf("find nota: %w", err)
	}
	if nota.MetodePembayaran != model.NotaMetodeReimburse {
		return nil, fmt.Errorf("%w: hanya nota reimburse yang direkonsiliasi", ErrValidation)
	}
	if nota.StatusPembayaran != model.NotaStatusBelumDibayar {
		return nil, ErrNotaAlreadyPaid
	}

	now := s.now().In(s.location)
	paidAt := now
	nota.StatusPembayaran = model.NotaStatusSudahDibayar
	nota.BuktiBayar = buktiBayar
	nota.DibayarPada = &paidAt
	nota.DirekonsiliasiOleh = user.NamaLengkap
	nota.DirekonsiliasiOlehID = user.UserID
	nota.UpdatedAt = now
	if err := s.store.SettleNota(ctx, rowNumber, nota); err != nil {
		return nil, fmt.Errorf("settle nota: %w", err)
	}
	return nota, nil
}

type NotaItemInput struct {
	NamaProduk string
	Satuan     string
	Volume     string
	Harga      string
}

// IsBlank reports a row nobody filled in. The form always renders at least one
// empty line, and an untouched line is not an error.
func (i NotaItemInput) IsBlank() bool {
	return strings.TrimSpace(i.NamaProduk) == "" && strings.TrimSpace(i.Satuan) == "" &&
		strings.TrimSpace(i.Volume) == "" && strings.TrimSpace(i.Harga) == ""
}

type NotaInput struct {
	Tanggal           string
	PIC               string
	Metode            string
	PenerimaReimburse string
	Kategori          string
	SubKategori       string
	JenisPerjalanan   string
	Items             []NotaItemInput
	FotoKwitansi      string
	BuktiTransfer     string
}

func (s *NotaService) Create(ctx context.Context, user *model.User, input NotaInput) (*model.Nota, error) {
	if user == nil {
		return nil, fmt.Errorf("%w: pengguna tidak dikenal", ErrValidation)
	}

	tanggal := strings.TrimSpace(input.Tanggal)
	if _, err := time.Parse("2006-01-02", tanggal); err != nil {
		return nil, fmt.Errorf("%w: tanggal transaksi wajib valid", ErrValidation)
	}

	options, err := s.Options(ctx)
	if err != nil {
		return nil, err
	}
	pic, err := adoptOption("PIC", input.PIC, options.PIC)
	if err != nil {
		return nil, err
	}

	metode, err := notaMetode(input.Metode)
	if err != nil {
		return nil, err
	}
	status := StatusFor(metode)

	// A reimbursement names the person owed the money; a cash advance has
	// already been paid out, so carrying a payee there would invent a debt.
	penerima := strings.TrimSpace(input.PenerimaReimburse)
	if metode == model.NotaMetodeReimburse {
		if penerima == "" {
			return nil, fmt.Errorf("%w: nama penerima reimburse wajib diisi", ErrValidation)
		}
	} else {
		penerima = ""
	}

	kategori, subKategori, err := notaKategori(input.Kategori, input.SubKategori)
	if err != nil {
		return nil, err
	}

	// Only a business trip asks how it was travelled. Storing an answer under
	// any other sub category would report fuel spending that never happened.
	jenisPerjalanan := strings.TrimSpace(input.JenisPerjalanan)
	if subKategori == NotaSubPerjalananDinas {
		jenisPerjalanan, err = matchOption("Jenis perjalanan dinas", jenisPerjalanan, NotaJenisPerjalananOptions)
		if err != nil {
			return nil, err
		}
	} else {
		jenisPerjalanan = ""
	}

	items, total, err := notaItems(input.Items)
	if err != nil {
		return nil, err
	}

	// The receipt is the evidence the expense happened, so no nota exists
	// without one.
	kwitansi := strings.TrimSpace(input.FotoKwitansi)
	if kwitansi == "" {
		return nil, fmt.Errorf("%w: foto kwitansi wajib diunggah", ErrValidation)
	}
	if err := photo.ValidateDataURL(kwitansi); err != nil {
		return nil, fmt.Errorf("%w: foto kwitansi tidak valid", ErrInvalidPhoto)
	}
	// A cash advance moved company money before the receipt existed, so it
	// carries the transfer proof as well.
	transfer := strings.TrimSpace(input.BuktiTransfer)
	if metode == model.NotaMetodeCA {
		if transfer == "" {
			return nil, fmt.Errorf("%w: bukti transfer wajib diunggah untuk CA", ErrValidation)
		}
		if err := photo.ValidateDataURL(transfer); err != nil {
			return nil, fmt.Errorf("%w: bukti transfer tidak valid", ErrInvalidPhoto)
		}
	} else {
		transfer = ""
	}

	// Serialise numbering and writing so two submissions cannot claim the same
	// identifier.
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := s.idPrefix()
	highest, err := s.store.MaxNotaSequence(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("read last nota sequence: %w", err)
	}
	notaID := fmt.Sprintf("%s%04d", prefix, highest+1)
	for i := range items {
		items[i].NotaID = notaID
	}

	now := s.now().In(s.location)
	nota := &model.Nota{
		NotaID:            notaID,
		Tanggal:           tanggal,
		PIC:               pic,
		MetodePembayaran:  metode,
		StatusPembayaran:  status,
		PenerimaReimburse: penerima,
		Kategori:          kategori,
		SubKategori:       subKategori,
		JenisPerjalanan:   jenisPerjalanan,
		Total:             total,
		FotoKwitansi:      kwitansi,
		BuktiTransfer:     transfer,
		CreatedBy:         user.NamaLengkap,
		CreatedByID:       user.UserID,
		CreatedAt:         now,
		UpdatedAt:         now,
		Items:             items,
	}
	if err := s.store.CreateNota(ctx, nota); err != nil {
		return nil, fmt.Errorf("create nota: %w", err)
	}
	// A name typed just now has to reach the next form, or the same person is
	// stored again under a second spelling.
	s.invalidateOptions()
	return nota, nil
}

func notaMetode(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	for _, option := range NotaMetodeOptions {
		if strings.EqualFold(trimmed, option.Value) {
			return option.Value, nil
		}
	}
	return "", fmt.Errorf("%w: metode pembayaran wajib dipilih", ErrValidation)
}

func notaKategori(kategori, sub string) (string, string, error) {
	trimmed := strings.TrimSpace(kategori)
	for _, option := range NotaKategoriOptions {
		if !strings.EqualFold(trimmed, option.Nama) {
			continue
		}
		matched, err := matchOption("Sub kategori", sub, option.Sub)
		if err != nil {
			return "", "", err
		}
		return option.Nama, matched, nil
	}
	return "", "", fmt.Errorf("%w: kategori biaya wajib dipilih", ErrValidation)
}

// matchOption accepts only a value from a closed list, case-insensitively, and
// returns the list's own spelling so the sheet stays consistent.
func matchOption(label, value string, allowed []string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%w: %s wajib dipilih", ErrValidation, strings.ToLower(label))
	}
	for _, option := range allowed {
		if strings.EqualFold(trimmed, option) {
			return option, nil
		}
	}
	return "", fmt.Errorf("%w: %s tidak dikenal", ErrValidation, strings.ToLower(label))
}

// groupedNumber matches money written the Indonesian way: dots every three
// digits, an optional decimal comma. Requiring full groups of three keeps a
// plain decimal such as "1.5" out of the rewrite.
var groupedNumber = regexp.MustCompile(`^-?\d{1,3}(\.\d{3})+(,\d+)?$`)

// ungroupThousands turns "1.500.000" into "1500000". The form shows the grouped
// form for readability and the script strips it before submitting; this is the
// backstop for a browser where that script never ran.
func ungroupThousands(value string) string {
	trimmed := strings.TrimSpace(value)
	if !groupedNumber.MatchString(trimmed) {
		return trimmed
	}
	return strings.ReplaceAll(trimmed, ".", "")
}

// notaItems validates the lines and totals them. Blank lines are dropped: the
// form renders spare rows, and an untouched row is not an entry.
func notaItems(inputs []NotaItemInput) ([]model.NotaItem, float64, error) {
	items := make([]model.NotaItem, 0, len(inputs))
	total := 0.0
	for _, input := range inputs {
		if input.IsBlank() {
			continue
		}
		if len(items) >= notaMaxItems {
			return nil, 0, fmt.Errorf("%w: satu nota maksimal %d item", ErrValidation, notaMaxItems)
		}
		position := len(items) + 1
		nama := strings.TrimSpace(input.NamaProduk)
		if nama == "" {
			return nil, 0, fmt.Errorf("%w: nama produk item %d wajib diisi", ErrValidation, position)
		}
		satuan := strings.TrimSpace(input.Satuan)
		if satuan == "" {
			return nil, 0, fmt.Errorf("%w: satuan item %d wajib diisi", ErrValidation, position)
		}
		volume, err := parsePositive(fmt.Sprintf("Volume item %d", position), input.Volume)
		if err != nil {
			return nil, 0, err
		}
		// A line worth nothing is allowed — a bonus item on a receipt reads as
		// zero — but a negative price would quietly reduce the total.
		harga, err := parseNonNegative(fmt.Sprintf("Harga item %d", position), ungroupThousands(input.Harga))
		if err != nil {
			return nil, 0, err
		}
		subtotal := round2(volume * harga)
		total += subtotal
		items = append(items, model.NotaItem{
			Baris:      position,
			NamaProduk: nama,
			Satuan:     satuan,
			Volume:     volume,
			Harga:      harga,
			Subtotal:   subtotal,
		})
	}
	if len(items) == 0 {
		return nil, 0, fmt.Errorf("%w: isi minimal satu item nota", ErrValidation)
	}
	return items, round2(total), nil
}
