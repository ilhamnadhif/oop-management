package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func newNotaFixture(t *testing.T) (*NotaService, *repository.TestRepository, *model.User) {
	t.Helper()
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, location)
	service := NewNotaService(store, location, func() time.Time { return now })
	user := &model.User{UserID: "usr_1", NamaLengkap: "Budi", StatusPengguna: model.StatusAktif}
	return service, store, user
}

func reimburseInput(t *testing.T) NotaInput {
	t.Helper()
	return NotaInput{
		Tanggal:           "2026-08-09",
		PIC:               "Budi",
		Metode:            model.NotaMetodeReimburse,
		PenerimaReimburse: "Budi Santoso",
		Kategori:          NotaKategoriUmumADM,
		SubKategori:       "ATK",
		Items: []NotaItemInput{
			{NamaProduk: "Kertas A4", Satuan: "rim", Volume: "2", Harga: "55000"},
		},
		FotoKwitansi: testPhoto(t),
	}
}

func cashAdvanceInput(t *testing.T) NotaInput {
	t.Helper()
	input := reimburseInput(t)
	input.Metode = model.NotaMetodeCA
	input.PenerimaReimburse = ""
	input.BuktiTransfer = testPhoto(t)
	return input
}

// A reimbursement is money the company still owes; the status follows from the
// method rather than from anything the form sends.
func TestCreateNotaReimburseIsUnpaidAndNamesThePayee(t *testing.T) {
	service, store, user := newNotaFixture(t)

	nota, err := service.Create(context.Background(), user, reimburseInput(t))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if nota.StatusPembayaran != model.NotaStatusBelumDibayar {
		t.Fatalf("status = %q, want %q", nota.StatusPembayaran, model.NotaStatusBelumDibayar)
	}
	if nota.PenerimaReimburse != "Budi Santoso" {
		t.Fatalf("penerima = %q", nota.PenerimaReimburse)
	}
	if len(store.NotaList()) != 1 {
		t.Fatalf("stored %d notes, want 1", len(store.NotaList()))
	}
}

// A cash advance is money already handed out, so it is settled on arrival and
// carries no payee to be repaid.
func TestCreateNotaCashAdvanceIsPaidAndDropsThePayee(t *testing.T) {
	service, _, user := newNotaFixture(t)

	input := cashAdvanceInput(t)
	input.PenerimaReimburse = "Seseorang"
	nota, err := service.Create(context.Background(), user, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if nota.StatusPembayaran != model.NotaStatusSudahDibayar {
		t.Fatalf("status = %q, want %q", nota.StatusPembayaran, model.NotaStatusSudahDibayar)
	}
	if nota.PenerimaReimburse != "" {
		t.Fatalf("a cash advance recorded a payee: %q", nota.PenerimaReimburse)
	}
}

func TestCreateNotaRequiresThePayeeForReimburse(t *testing.T) {
	service, _, user := newNotaFixture(t)

	input := reimburseInput(t)
	input.PenerimaReimburse = "  "
	_, err := service.Create(context.Background(), user, input)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want a validation error", err)
	}
}

// A cash advance moved company money before any receipt existed, so the
// transfer proof is part of the record.
func TestCreateNotaRequiresTransferProofForCashAdvance(t *testing.T) {
	service, _, user := newNotaFixture(t)

	input := cashAdvanceInput(t)
	input.BuktiTransfer = ""
	_, err := service.Create(context.Background(), user, input)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want a validation error", err)
	}
}

// A reimbursement has no transfer to prove; a file sent anyway is not stored.
func TestCreateNotaDropsTransferProofOnReimburse(t *testing.T) {
	service, _, user := newNotaFixture(t)

	input := reimburseInput(t)
	input.BuktiTransfer = testPhoto(t)
	nota, err := service.Create(context.Background(), user, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if nota.BuktiTransfer != "" {
		t.Fatal("a reimbursement stored a transfer proof")
	}
}

func TestCreateNotaRequiresTheReceipt(t *testing.T) {
	service, _, user := newNotaFixture(t)

	input := reimburseInput(t)
	input.FotoKwitansi = ""
	_, err := service.Create(context.Background(), user, input)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want a validation error", err)
	}
}

// The category pair is closed. A sub category borrowed from the other category
// would file the expense where no report looks for it.
func TestCreateNotaRejectsAMismatchedSubCategory(t *testing.T) {
	service, _, user := newNotaFixture(t)

	input := reimburseInput(t)
	input.Kategori = NotaKategoriUmumADM
	input.SubKategori = "QHSE"
	_, err := service.Create(context.Background(), user, input)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want a validation error", err)
	}

	operasional := reimburseInput(t)
	operasional.Kategori = NotaKategoriOperasional
	operasional.SubKategori = "Material Bantu"
	if _, err := service.Create(context.Background(), user, operasional); err != nil {
		t.Fatalf("a valid pair was rejected: %v", err)
	}
}

// Only a business trip asks how it was travelled.
func TestCreateNotaTravelTypeFollowsTheSubCategory(t *testing.T) {
	service, _, user := newNotaFixture(t)

	missing := reimburseInput(t)
	missing.SubKategori = NotaSubPerjalananDinas
	if _, err := service.Create(context.Background(), user, missing); !errors.Is(err, ErrValidation) {
		t.Fatalf("a trip without its type was accepted: %v", err)
	}

	unknown := reimburseInput(t)
	unknown.SubKategori = NotaSubPerjalananDinas
	unknown.JenisPerjalanan = "Kapal"
	if _, err := service.Create(context.Background(), user, unknown); !errors.Is(err, ErrValidation) {
		t.Fatalf("an unknown travel type was accepted: %v", err)
	}

	trip := reimburseInput(t)
	trip.SubKategori = NotaSubPerjalananDinas
	trip.JenisPerjalanan = "BBM"
	nota, err := service.Create(context.Background(), user, trip)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if nota.JenisPerjalanan != "BBM" {
		t.Fatalf("jenis perjalanan = %q", nota.JenisPerjalanan)
	}

	// Anything else must not carry a travel type, or a stationery purchase
	// would be reported as fuel spending.
	other := reimburseInput(t)
	other.JenisPerjalanan = "Tiket"
	stored, err := service.Create(context.Background(), user, other)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if stored.JenisPerjalanan != "" {
		t.Fatalf("a non-trip carried a travel type: %q", stored.JenisPerjalanan)
	}
}

func TestCreateNotaTotalsItsItems(t *testing.T) {
	service, _, user := newNotaFixture(t)

	input := reimburseInput(t)
	input.Items = []NotaItemInput{
		{NamaProduk: "Kertas A4", Satuan: "rim", Volume: "2", Harga: "55000"},
		{},
		{NamaProduk: "Tinta printer", Satuan: "pcs", Volume: "1.5", Harga: "120000"},
	}
	nota, err := service.Create(context.Background(), user, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// The blank line between the two entries is a spare row, not an entry.
	if len(nota.Items) != 2 {
		t.Fatalf("stored %d items, want 2", len(nota.Items))
	}
	if nota.Items[1].Baris != 2 {
		t.Fatalf("second item numbered %d, want 2", nota.Items[1].Baris)
	}
	if nota.Items[0].Subtotal != 110000 || nota.Items[1].Subtotal != 180000 {
		t.Fatalf("subtotals wrong: %+v", nota.Items)
	}
	if nota.Total != 290000 {
		t.Fatalf("total = %v, want 290000", nota.Total)
	}
}

// The price field shows thousand separators while it is typed. The script
// strips them before submitting, and the server accepts the grouped form too so
// a browser without that script still files a nota.
func TestCreateNotaAcceptsAGroupedPrice(t *testing.T) {
	service, _, user := newNotaFixture(t)

	input := reimburseInput(t)
	input.Items = []NotaItemInput{
		{NamaProduk: "Laptop", Satuan: "unit", Volume: "1", Harga: "12.500.000"},
	}
	nota, err := service.Create(context.Background(), user, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if nota.Items[0].Harga != 12500000 || nota.Total != 12500000 {
		t.Fatalf("harga %v total %v, want 12500000", nota.Items[0].Harga, nota.Total)
	}
}

// A dot that is not a thousand separator is a decimal point and stays one; the
// rewrite only applies to full groups of three digits.
func TestUngroupThousandsLeavesDecimalsAlone(t *testing.T) {
	for value, want := range map[string]string{
		"1.500.000": "1500000",
		"1.000":     "1000",
		"1.5":       "1.5",
		"1.25":      "1.25",
		"12500000":  "12500000",
		"":          "",
	} {
		if got := ungroupThousands(value); got != want {
			t.Fatalf("ungroupThousands(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestCreateNotaRejectsEmptyAndInvalidItems(t *testing.T) {
	service, _, user := newNotaFixture(t)

	empty := reimburseInput(t)
	empty.Items = []NotaItemInput{{}}
	if _, err := service.Create(context.Background(), user, empty); !errors.Is(err, ErrValidation) {
		t.Fatalf("a nota with no items was accepted: %v", err)
	}

	for name, item := range map[string]NotaItemInput{
		"no name":        {Satuan: "rim", Volume: "1", Harga: "1000"},
		"no unit":        {NamaProduk: "Kertas", Volume: "1", Harga: "1000"},
		"zero volume":    {NamaProduk: "Kertas", Satuan: "rim", Volume: "0", Harga: "1000"},
		"negative price": {NamaProduk: "Kertas", Satuan: "rim", Volume: "1", Harga: "-5000"},
	} {
		input := reimburseInput(t)
		input.Items = []NotaItemInput{item}
		if _, err := service.Create(context.Background(), user, input); !errors.Is(err, ErrValidation) {
			t.Fatalf("%s was accepted: %v", name, err)
		}
	}
}

// The identifier says which day the expense was filed and counts within it.
func TestCreateNotaNumbersWithinTheDay(t *testing.T) {
	service, _, user := newNotaFixture(t)

	first, err := service.Create(context.Background(), user, reimburseInput(t))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if first.NotaID != "NTA-20260809-0001" {
		t.Fatalf("first id = %q", first.NotaID)
	}
	second, err := service.Create(context.Background(), user, reimburseInput(t))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if second.NotaID != "NTA-20260809-0002" {
		t.Fatalf("second id = %q", second.NotaID)
	}
	// Every line of a nota has to point back at its header, or the detail
	// belongs to nothing.
	for _, item := range second.Items {
		if item.NotaID != second.NotaID {
			t.Fatalf("item points at %q, want %q", item.NotaID, second.NotaID)
		}
	}
}

// The PIC picker suggests names already used and accepts new ones, adopting an
// existing spelling so one person does not appear twice.
func TestNotaPICIsCreatableAndAdoptsSpelling(t *testing.T) {
	service, _, user := newNotaFixture(t)

	if _, err := service.Create(context.Background(), user, reimburseInput(t)); err != nil {
		t.Fatalf("create: %v", err)
	}
	options, err := service.Options(context.Background())
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	if len(options.PIC) != 1 || options.PIC[0] != "Budi" {
		t.Fatalf("pic options = %v", options.PIC)
	}

	again := reimburseInput(t)
	again.PIC = "budi"
	nota, err := service.Create(context.Background(), user, again)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if nota.PIC != "Budi" {
		t.Fatalf("pic = %q, want the spelling already in use", nota.PIC)
	}

	fresh := reimburseInput(t)
	fresh.PIC = "Sari"
	if _, err := service.Create(context.Background(), user, fresh); err != nil {
		t.Fatalf("a new PIC was rejected: %v", err)
	}
}

func TestStatusForIgnoresAnythingButCA(t *testing.T) {
	if got := StatusFor(model.NotaMetodeCA); got != model.NotaStatusSudahDibayar {
		t.Fatalf("CA status = %q", got)
	}
	for _, metode := range []string{model.NotaMetodeReimburse, "", "apa saja"} {
		if got := StatusFor(metode); got != model.NotaStatusBelumDibayar {
			t.Fatalf("%q status = %q, want unpaid", metode, got)
		}
	}
}
