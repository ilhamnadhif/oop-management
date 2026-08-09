package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func settledFixture(t *testing.T) (*NotaService, *repository.TestRepository, *model.User, *model.Nota) {
	t.Helper()
	service, store, user := newNotaFixture(t)
	nota, err := service.Create(context.Background(), user, reimburseInput(t))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return service, store, user, nota
}

// A cash advance is money that left the company before the nota was filed, so
// it is not something finance can pay again.
func TestOutstandingListsOnlyUnpaidReimbursements(t *testing.T) {
	service, _, user := newNotaFixture(t)
	if _, err := service.Create(context.Background(), user, reimburseInput(t)); err != nil {
		t.Fatalf("create reimburse: %v", err)
	}
	if _, err := service.Create(context.Background(), user, cashAdvanceInput(t)); err != nil {
		t.Fatalf("create ca: %v", err)
	}

	rows, err := service.Outstanding(context.Background(), "")
	if err != nil {
		t.Fatalf("outstanding: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("listed %d notes, want 1", len(rows))
	}
	if rows[0].MetodePembayaran != model.NotaMetodeReimburse ||
		rows[0].StatusPembayaran != model.NotaStatusBelumDibayar {
		t.Fatalf("unexpected nota in the list: %+v", rows[0])
	}
}

// Finance reads the number off a printed nota, so a partial one has to find it.
func TestOutstandingSearchesByTransactionNumber(t *testing.T) {
	service, _, user, nota := settledFixture(t)
	second, err := service.Create(context.Background(), user, reimburseInput(t))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	rows, err := service.Outstanding(context.Background(), nota.NotaID)
	if err != nil {
		t.Fatalf("outstanding: %v", err)
	}
	if len(rows) != 1 || rows[0].NotaID != nota.NotaID {
		t.Fatalf("searching the full number returned %+v", rows)
	}

	partial, err := service.Outstanding(context.Background(), "0002")
	if err != nil {
		t.Fatalf("outstanding: %v", err)
	}
	if len(partial) != 1 || partial[0].NotaID != second.NotaID {
		t.Fatalf("searching a partial number returned %+v", partial)
	}

	// A number nobody filed finds nothing rather than everything.
	none, err := service.Outstanding(context.Background(), "NTA-19990101-0001")
	if err != nil {
		t.Fatalf("outstanding: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("an unknown number matched %d notes", len(none))
	}
}

func TestSettleMarksTheNotaPaidAndRecordsWhoDidIt(t *testing.T) {
	service, store, user, nota := settledFixture(t)

	settled, err := service.Settle(context.Background(), user, nota.NotaID, testPhoto(t))
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if settled.StatusPembayaran != model.NotaStatusSudahDibayar {
		t.Fatalf("status = %q", settled.StatusPembayaran)
	}
	stored := store.NotaList()[0]
	if stored.StatusPembayaran != model.NotaStatusSudahDibayar {
		t.Fatalf("the stored nota is still %q", stored.StatusPembayaran)
	}
	if stored.BuktiBayar == "" {
		t.Fatal("the payment proof was not stored")
	}
	// A status that changed with nobody's name against it is not something an
	// audit can follow up.
	if stored.DirekonsiliasiOleh != "Budi" || stored.DirekonsiliasiOlehID != "usr_1" {
		t.Fatalf("the settlement names %q (%q)", stored.DirekonsiliasiOleh, stored.DirekonsiliasiOlehID)
	}
	if stored.DibayarPada == nil || stored.DibayarPada.IsZero() {
		t.Fatal("the settlement recorded no payment date")
	}
	// It leaves the outstanding list the moment it is paid.
	rows, err := service.Outstanding(context.Background(), "")
	if err != nil {
		t.Fatalf("outstanding: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("a settled nota is still outstanding: %+v", rows)
	}
}

// The proof is the point of the exercise: a status flipped without evidence is
// exactly what reconciliation exists to prevent.
func TestSettleRequiresProofOfPayment(t *testing.T) {
	service, store, user, nota := settledFixture(t)

	if _, err := service.Settle(context.Background(), user, nota.NotaID, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want a validation error", err)
	}
	if _, err := service.Settle(context.Background(), user, nota.NotaID, "bukan gambar"); !errors.Is(err, ErrInvalidPhoto) {
		t.Fatalf("error = %v, want an invalid photo error", err)
	}
	if store.NotaList()[0].StatusPembayaran != model.NotaStatusBelumDibayar {
		t.Fatal("the nota was settled without proof")
	}
}

func TestSettleRefusesUnknownAlreadyPaidAndCashAdvances(t *testing.T) {
	service, _, user, nota := settledFixture(t)

	if _, err := service.Settle(context.Background(), user, "NTA-19990101-0001", testPhoto(t)); !errors.Is(err, ErrNotaNotFound) {
		t.Fatalf("unknown nota: error = %v", err)
	}

	if _, err := service.Settle(context.Background(), user, nota.NotaID, testPhoto(t)); err != nil {
		t.Fatalf("settle: %v", err)
	}
	// Paying the same nota twice would double the money leaving the company.
	if _, err := service.Settle(context.Background(), user, nota.NotaID, testPhoto(t)); !errors.Is(err, ErrNotaAlreadyPaid) {
		t.Fatalf("second settlement: error = %v, want already paid", err)
	}

	advance, err := service.Create(context.Background(), user, cashAdvanceInput(t))
	if err != nil {
		t.Fatalf("create ca: %v", err)
	}
	if _, err := service.Settle(context.Background(), user, advance.NotaID, testPhoto(t)); !errors.Is(err, ErrValidation) {
		t.Fatalf("cash advance: error = %v, want a validation error", err)
	}
}

// The number is read off paper, where nobody minds about spaces or case.
func TestSettleAcceptsALooselyTypedNumber(t *testing.T) {
	service, _, user, nota := settledFixture(t)

	if _, err := service.Settle(context.Background(), user, "  "+strings.ToLower(nota.NotaID)+"  ", testPhoto(t)); err != nil {
		t.Fatalf("settle: %v", err)
	}
}
