package widya

import (
	"context"
	"errors"
	"testing"
	"time"

	"opp-management/internal/model"
)

// memoryStore keeps tickets the way the sheet does: appended in order, each
// at the row after the last.
type memoryStore struct {
	rows    []model.WidyaTicket
	appends int
	updates int
}

func (m *memoryStore) ListWidyaTickets(context.Context) ([]model.WidyaTicket, error) {
	return append([]model.WidyaTicket(nil), m.rows...), nil
}

func (m *memoryStore) AppendWidyaTickets(_ context.Context, tickets []model.WidyaTicket) error {
	m.appends++
	for _, ticket := range tickets {
		ticket.RowNumber = len(m.rows) + 2
		m.rows = append(m.rows, ticket)
	}
	return nil
}

func (m *memoryStore) UpdateWidyaTickets(_ context.Context, tickets []model.WidyaTicket) error {
	m.updates++
	for _, ticket := range tickets {
		m.rows[ticket.RowNumber-2] = ticket
	}
	return nil
}

// rangeSource answers the way the scanner does: only the tickets created in
// the range asked for, which here is when they were scanned full.
type rangeSource struct {
	tickets []Ticket
	err     error
	reads   [][2]time.Time
	since   []time.Time
}

func (s *rangeSource) Fetch(context.Context) (Snapshot, error) { return Snapshot{}, nil }

// TicketsUpdatedSince answers with the tickets changed at or after since. A
// ticket without an UpdatedAt counts as changed when it was scanned full.
func (s *rangeSource) TicketsUpdatedSince(_ context.Context, since time.Time) ([]Ticket, error) {
	s.since = append(s.since, since)
	if s.err != nil {
		return nil, s.err
	}
	var out []Ticket
	for _, ticket := range s.tickets {
		changed := ticket.UpdatedAt
		if changed.IsZero() {
			changed = ticket.ScanFullAt
		}
		if !changed.Before(since) {
			out = append(out, ticket)
		}
	}
	return out, nil
}

func (s *rangeSource) Tickets(_ context.Context, from, to time.Time) ([]Ticket, error) {
	s.reads = append(s.reads, [2]time.Time{from, to})
	if s.err != nil {
		return nil, s.err
	}
	var out []Ticket
	for _, ticket := range s.tickets {
		if !ticket.ScanFullAt.Before(from) && !ticket.ScanFullAt.After(to) {
			out = append(out, ticket)
		}
	}
	return out, nil
}

// An empty sheet is filled with two months, a week at a time, then only
// today is read again; every ticket is kept once.
func TestArchiveFillsAnEmptySheetThenReadsToday(t *testing.T) {
	now := at("2026-10-25", "10:00")
	source := &rangeSource{tickets: []Ticket{
		{Number: "LS-1", Vehicle: "A", Supplier: "HPP", Volume: 25, ScanFullAt: at("2026-09-01", "09:00")},
		{Number: "LS-2", Vehicle: "A", Supplier: "HPP", Volume: 26, ScanFullAt: at("2026-10-24", "09:00")},
		{Number: "LS-3", Vehicle: "B", Supplier: "MNP", Volume: 12, ScanFullAt: at("2026-10-25", "08:00")},
		// Older than the backfill reaches.
		{Number: "LS-0", Vehicle: "A", Supplier: "HPP", Volume: 25, ScanFullAt: at("2026-08-01", "09:00")},
	}}
	store := &memoryStore{}
	archive := NewArchive(source, store, wib, 30*time.Second, func() time.Time { return now })

	if err := archive.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(store.rows) != 3 || store.rows[0].Number != "LS-1" || store.rows[2].Number != "LS-3" {
		t.Fatalf("kept = %+v", store.rows)
	}
	// 62 days a week at a time is nine reads, then yesterday and today.
	if len(source.reads) != 11 {
		t.Fatalf("read the scanner %d times, want 11", len(source.reads))
	}

	source.reads = nil
	now = now.Add(time.Minute)
	source.tickets = append(source.tickets, Ticket{Number: "LS-4", Vehicle: "B", Supplier: "MNP", Volume: 13, ScanFullAt: at("2026-10-25", "10:00")})
	if err := archive.Sync(context.Background()); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	// The second round reads forward from the first, not today again.
	if len(source.reads) != 0 || len(source.since) != 1 || len(store.rows) != 4 {
		t.Fatalf("second round read %d ranges and %d since, kept %d tickets; want one read since and 4",
			len(source.reads), len(source.since), len(store.rows))
	}
	if want := at("2026-10-25", "10:00").Add(-cursorOverlap).Add(-cursorOverlap); !source.since[0].Equal(want) {
		t.Fatalf("read since %v, want %v", source.since[0], want)
	}

	summary, has, err := archive.Ritase(context.Background(), "2026-10-24", "2026-10-25", wib)
	if err != nil || !has || summary.TotalRitase != 3 {
		t.Fatalf("summary = %+v, %v, %v", summary, has, err)
	}
}

// A restart reads the sheet, fills only the days since its newest ticket, and
// keeps nothing twice.
func TestArchiveRestartFillsOnlyTheGap(t *testing.T) {
	now := at("2026-10-25", "10:00")
	store := &memoryStore{rows: []model.WidyaTicket{
		{RowNumber: 2, Number: "LS-1", Vehicle: "A", Supplier: "HPP", Volume: 25, ScanFullAt: at("2026-10-22", "09:00")},
	}}
	source := &rangeSource{tickets: []Ticket{
		{Number: "LS-1", Vehicle: "A", Supplier: "HPP", Volume: 25, ScanFullAt: at("2026-10-22", "09:00")},
		{Number: "LS-2", Vehicle: "A", Supplier: "HPP", Volume: 26, ScanFullAt: at("2026-10-23", "09:00")},
	}}
	archive := NewArchive(source, store, wib, 30*time.Second, func() time.Time { return now })
	if err := archive.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(store.rows) != 2 {
		t.Fatalf("kept = %+v", store.rows)
	}
	// The gap from the 22nd to today is one read.
	if from := source.reads[0][0]; !from.Equal(at("2026-10-22", "00:00").Add(-createdLead)) {
		t.Fatalf("backfill began at %v", from)
	}
}

// Yesterday is read again once a day, and a load the scanner corrected is
// rewritten in its row rather than appended again. A correction older than
// yesterday is not looked for.
func TestArchiveRewritesACorrectedTicket(t *testing.T) {
	now := at("2026-10-25", "10:00")
	source := &rangeSource{tickets: []Ticket{
		{Number: "LS-1", Vehicle: "A", Supplier: "HPP", Volume: 25, ScanFullAt: at("2026-10-25", "09:00")},
	}}
	store := &memoryStore{}
	archive := NewArchive(source, store, wib, 30*time.Second, func() time.Time { return now })
	if err := archive.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// The next morning the scanner holds a corrected volume for the 25th,
	// which is now yesterday.
	now = at("2026-10-26", "07:00")
	source.tickets[0].Volume = 27.5
	if err := archive.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(store.rows) != 1 || store.rows[0].Volume != 27.5 || store.updates != 1 {
		t.Fatalf("rows = %+v, %d updates", store.rows, store.updates)
	}
	summary, _, _ := archive.Ritase(context.Background(), "2026-10-25", "2026-10-25", wib)
	if summary.TotalVolume != 27.5 {
		t.Fatalf("the page reads %v, want the corrected 27.5", summary.TotalVolume)
	}

	// Yesterday is read once a day, not every round: a later round only reads
	// forward.
	source.reads = nil
	now = now.Add(time.Minute)
	if err := archive.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(source.reads) != 0 {
		t.Fatalf("read %d ranges in a later round, want none", len(source.reads))
	}
}

// When the scanner cannot be read the kept tickets stand, with the error.
func TestArchiveKeepsWhatItHasWhenTheScannerIsDown(t *testing.T) {
	now := at("2026-10-25", "10:00")
	source := &rangeSource{tickets: []Ticket{
		{Number: "LS-1", Vehicle: "A", Supplier: "HPP", Volume: 25, ScanFullAt: at("2026-10-25", "08:00")},
	}}
	archive := NewArchive(source, &memoryStore{}, wib, 30*time.Second, func() time.Time { return now })
	if _, has, _ := archive.Ritase(context.Background(), "2026-10-25", "2026-10-25", wib); has {
		t.Fatal("tickets were served before the sheet was read")
	}
	if err := archive.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	source.err = errors.New("down")
	now = now.Add(time.Minute)
	if err := archive.Sync(context.Background()); err == nil {
		t.Fatal("an outage read as a success")
	}
	summary, has, err := archive.Ritase(context.Background(), "2026-10-25", "2026-10-25", wib)
	if err == nil || !has || summary.TotalRitase != 1 {
		t.Fatalf("an outage lost the kept tickets: %+v, %v, %v", summary, has, err)
	}
	if _, _, err := archive.Ritase(context.Background(), "25-10-2026", "2026-10-25", wib); err == nil {
		t.Fatal("a malformed date was read")
	}
}

// Each round reads forward from the newest change seen: a ticket corrected on
// any day is rewritten, and the cursor follows the scanner's own clock.
func TestArchiveReadsForwardFromTheNewestChange(t *testing.T) {
	now := at("2026-10-25", "10:00")
	source := &rangeSource{tickets: []Ticket{
		{Number: "LS-1", Vehicle: "A", Supplier: "HPP", Volume: 25, ScanFullAt: at("2026-10-25", "09:00"), UpdatedAt: at("2026-10-25", "09:00")},
	}}
	store := &memoryStore{}
	archive := NewArchive(source, store, wib, 10*time.Second, func() time.Time { return now })
	if err := archive.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// A new load, and a correction to one from three days ago.
	now = now.Add(10 * time.Second)
	source.tickets = append(source.tickets,
		Ticket{Number: "LS-2", Vehicle: "B", Supplier: "MNP", Volume: 12, ScanFullAt: at("2026-10-25", "10:00"), UpdatedAt: at("2026-10-25", "10:00")},
	)
	store.rows = append(store.rows, model.WidyaTicket{RowNumber: len(store.rows) + 2, Number: "LS-0",
		Vehicle: "A", Supplier: "HPP", Volume: 20, ScanFullAt: at("2026-10-22", "09:00")})
	archive.mu.Lock()
	archive.tickets["LS-0"] = store.rows[len(store.rows)-1]
	archive.mu.Unlock()
	source.tickets = append(source.tickets,
		Ticket{Number: "LS-0", Vehicle: "A", Supplier: "HPP", Volume: 22, ScanFullAt: at("2026-10-22", "09:00"), UpdatedAt: at("2026-10-25", "10:00")},
	)
	if err := archive.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(store.rows) != 3 || store.rows[1].Volume != 22 {
		t.Fatalf("rows = %+v", store.rows)
	}
	// The next round starts from the newest change the scanner reported.
	now = now.Add(10 * time.Second)
	if err := archive.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if last := source.since[len(source.since)-1]; !last.Equal(at("2026-10-25", "10:00").Add(-cursorOverlap)) {
		t.Fatalf("read since %v", last)
	}
}
