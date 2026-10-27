package widya

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
)

// TicketStore is where the archive keeps the tickets it has read: the project
// spreadsheet in production, memory in the tests.
type TicketStore interface {
	ListWidyaTickets(ctx context.Context) ([]model.WidyaTicket, error)
	AppendWidyaTickets(ctx context.Context, tickets []model.WidyaTicket) error
	UpdateWidyaTickets(ctx context.Context, tickets []model.WidyaTicket) error
}

// Archive keeps every ticket read off the scanner, in the project spreadsheet
// and in memory. The scanner is asked only for today's tickets each interval,
// plus yesterday's once a day in case a load was corrected after the day
// closed; everything older is read from the sheet, once, when the app starts.
// The pages read the memory, never the sheet or the scanner.
//
// Today is read in full once, then forward from the newest change the scanner
// reported: each round asks only for tickets created or corrected since, which
// is usually none or one, a few kilobytes rather than a day's worth.
type Archive struct {
	source   Source
	store    TicketStore
	location *time.Location
	interval time.Duration
	now      func() time.Time
	figures  *Feed

	mu      sync.RWMutex
	tickets map[string]model.WidyaTicket
	loaded  bool
	// backfilled is set once the history behind today has been read, so a
	// restart after a few days off fills the gap and then stops asking.
	// backfillNext is the first day not read yet: it is settled once, before
	// today's loads are kept, and moves forward a week at a time, so a week
	// the scanner refused is asked for again rather than skipped.
	backfilled   bool
	backfillNext time.Time
	// recheckedFor is the day whose tickets were last read again in full.
	recheckedFor string
	// cursor is the newest updatedAt the scanner has reported. Zero until
	// today has been read in full once.
	cursor   time.Time
	lastErr  error
	lastSync time.Time
}

// cursorOverlap is how far behind the cursor each round reads again. The
// scanner's clock and the order it commits changes in are its own; reading a
// little twice costs a ticket or two, missing one costs a load.
const cursorOverlap = 2 * time.Minute

// backfillDays is how much history a sheet with nothing in it is filled with:
// the longest range a Produksi New table may ask for.
const backfillDays = 62

// backfillChunk is how many days one backfill read covers. A week is a few
// pages of tickets, comfortably inside one request's time.
const backfillChunk = 7

// createdLead is how far before a day the tickets scanned full on it are
// looked for. The scanner filters on when a ticket was created, which can sit
// before its full scan.
const createdLead = 12 * time.Hour

// syncTimeout bounds one round of reads. A first backfill reads two months a
// week at a time and is given the longest.
const syncTimeout = 3 * time.Minute

// NewArchive keeps a source's tickets in a store. interval is how often today
// is read again; the figures share it.
func NewArchive(source Source, store TicketStore, location *time.Location, interval time.Duration, now func() time.Time) *Archive {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &Archive{
		source:   source,
		store:    store,
		location: location,
		interval: interval,
		now:      now,
		figures:  NewFeed(source, interval, now),
		tickets:  make(map[string]model.WidyaTicket),
	}
}

// MaxAge is how often the archive reads today again, which is also how often a
// page showing it is worth refreshing.
func (a *Archive) MaxAge() time.Duration { return a.interval }

// Latest returns the scanner's own dashboard figures.
func (a *Archive) Latest(ctx context.Context) (Snapshot, bool, error) {
	return a.figures.Latest(ctx)
}

// Run keeps the archive current until ctx ends: one round straight away, then
// one each interval. A failed round is logged and tried again next interval;
// the pages keep showing what was kept.
func (a *Archive) Run(ctx context.Context) {
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	for {
		round, cancel := context.WithTimeout(ctx, syncTimeout)
		if err := a.Sync(round); err != nil && ctx.Err() == nil {
			log.Printf("sync widya tickets: %v", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Sync runs one round: load the sheet the first time, fill any history behind
// today, read yesterday again once a day, and read today.
func (a *Archive) Sync(ctx context.Context) error {
	err := a.sync(ctx)
	a.mu.Lock()
	a.lastErr = err
	if err == nil {
		a.lastSync = a.now()
	}
	a.mu.Unlock()
	return err
}

func (a *Archive) sync(ctx context.Context) error {
	if err := a.load(ctx); err != nil {
		return err
	}
	now := a.now().In(a.location)
	todayStart := startOfDay(now)

	// Today is read whatever happens to the history: a backfill page the
	// scanner will not serve must not stop the page showing what is being
	// hauled now. What failed is tried again next round.
	var problems []error
	if err := a.backfill(ctx, todayStart); err != nil {
		problems = append(problems, err)
	}

	yesterday := todayStart.AddDate(0, 0, -1)
	a.mu.RLock()
	recheck := a.recheckedFor != yesterday.Format("2006-01-02")
	a.mu.RUnlock()
	if recheck {
		if err := a.recheckYesterday(ctx, yesterday, todayStart); err != nil {
			problems = append(problems, err)
		}
	}

	if err := a.readToday(ctx, now, todayStart); err != nil {
		problems = append(problems, err)
	}
	return errors.Join(problems...)
}

// recheckYesterday reads yesterday in full again, for loads corrected after
// the day closed.
func (a *Archive) recheckYesterday(ctx context.Context, yesterday, todayStart time.Time) error {
	fetched, err := a.source.Tickets(ctx, yesterday.Add(-createdLead), todayStart.Add(-time.Second))
	if err != nil {
		return fmt.Errorf("read yesterday: %w", err)
	}
	if err := a.merge(ctx, fetched); err != nil {
		return err
	}
	a.mu.Lock()
	a.recheckedFor = yesterday.Format("2006-01-02")
	a.mu.Unlock()
	return nil
}

// readToday reads today in full the first time, then forward from the newest
// change the scanner reported.
func (a *Archive) readToday(ctx context.Context, now, todayStart time.Time) error {
	a.mu.RLock()
	cursor := a.cursor
	a.mu.RUnlock()
	var fetched []Ticket
	var err error
	if cursor.IsZero() {
		// The first round reads today in full and starts the cursor there.
		fetched, err = a.source.Tickets(ctx, todayStart.Add(-createdLead), now.Add(time.Hour))
		cursor = now.Add(-cursorOverlap)
	} else {
		fetched, err = a.source.TicketsUpdatedSince(ctx, cursor.Add(-cursorOverlap))
	}
	if err != nil {
		return fmt.Errorf("read today: %w", err)
	}
	if err := a.merge(ctx, fetched); err != nil {
		return err
	}
	for _, ticket := range fetched {
		if ticket.UpdatedAt.After(cursor) {
			cursor = ticket.UpdatedAt
		}
	}
	a.mu.Lock()
	a.cursor = cursor
	a.mu.Unlock()
	return nil
}

// load reads the sheet into memory the first time it is needed.
func (a *Archive) load(ctx context.Context) error {
	a.mu.RLock()
	loaded := a.loaded
	a.mu.RUnlock()
	if loaded {
		return nil
	}
	kept, err := a.store.ListWidyaTickets(ctx)
	if err != nil {
		return fmt.Errorf("read kept tickets: %w", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ticket := range kept {
		a.tickets[ticket.Number] = ticket
	}
	a.loaded = true
	return nil
}

// backfill reads the history behind today once per start. An empty sheet is
// filled with the last backfillDays; a sheet that already holds tickets is
// filled from the day of its newest one, which covers the days the app was
// down. Both are read a week at a time.
func (a *Archive) backfill(ctx context.Context, todayStart time.Time) error {
	a.mu.Lock()
	if a.backfilled {
		a.mu.Unlock()
		return nil
	}
	if a.backfillNext.IsZero() {
		// Settled from the sheet as it was before this run kept anything:
		// once today's loads are in, the newest ticket says nothing about the
		// days behind it.
		from := todayStart.AddDate(0, 0, -(backfillDays - 1))
		var newest time.Time
		for _, ticket := range a.tickets {
			if ticket.ScanFullAt.After(newest) {
				newest = ticket.ScanFullAt
			}
		}
		if !newest.IsZero() {
			if day := startOfDay(newest.In(a.location)); day.After(from) {
				from = day
			}
		}
		a.backfillNext = from
	}
	start := a.backfillNext
	a.mu.Unlock()

	for ; start.Before(todayStart); start = start.AddDate(0, 0, backfillChunk) {
		end := start.AddDate(0, 0, backfillChunk)
		if end.After(todayStart) {
			end = todayStart
		}
		fetched, err := a.source.Tickets(ctx, start.Add(-createdLead), end.Add(-time.Second))
		if err != nil {
			return fmt.Errorf("backfill from %s: %w", start.Format("2006-01-02"), err)
		}
		if err := a.merge(ctx, fetched); err != nil {
			return err
		}
		a.mu.Lock()
		a.backfillNext = end
		a.mu.Unlock()
	}
	a.mu.Lock()
	a.backfilled = true
	a.mu.Unlock()
	return nil
}

// merge writes what was read: tickets not kept yet are appended, and kept
// tickets the scanner has since changed are rewritten in place. Memory is
// updated only after the sheet accepted the write, so the two never disagree.
func (a *Archive) merge(ctx context.Context, fetched []Ticket) error {
	now := a.now()
	var fresh, changed []model.WidyaTicket
	seen := make(map[string]bool, len(fetched))
	a.mu.RLock()
	for _, ticket := range fetched {
		number := strings.TrimSpace(ticket.Number)
		if number == "" || seen[number] {
			continue
		}
		seen[number] = true
		record := model.WidyaTicket{
			Number: number, ScanFullAt: ticket.ScanFullAt, Vehicle: ticket.Vehicle,
			Supplier: ticket.Supplier, Material: ticket.Material, Volume: ticket.Volume, SyncedAt: now,
		}
		kept, known := a.tickets[number]
		switch {
		case !known:
			fresh = append(fresh, record)
		case !sameLoad(kept, record):
			record.RowNumber = kept.RowNumber
			changed = append(changed, record)
		}
	}
	a.mu.RUnlock()

	if len(fresh) > 0 {
		// In scan order, so the sheet reads as a log.
		sort.Slice(fresh, func(i, j int) bool { return fresh[i].ScanFullAt.Before(fresh[j].ScanFullAt) })
		if err := a.store.AppendWidyaTickets(ctx, fresh); err != nil {
			return fmt.Errorf("keep %d new tickets: %w", len(fresh), err)
		}
		a.mu.Lock()
		for _, record := range fresh {
			a.tickets[record.Number] = record
		}
		a.mu.Unlock()
	}

	if len(changed) > 0 {
		if err := a.rowsFor(ctx, changed); err != nil {
			return err
		}
		if err := a.store.UpdateWidyaTickets(ctx, changed); err != nil {
			return fmt.Errorf("rewrite %d corrected tickets: %w", len(changed), err)
		}
		a.mu.Lock()
		for _, record := range changed {
			a.tickets[record.Number] = record
		}
		a.mu.Unlock()
	}
	return nil
}

// rowsFor fills in the sheet rows of tickets appended since the sheet was last
// read, which do not know where they landed. It reads the sheet again only
// when one of them is actually missing its row.
func (a *Archive) rowsFor(ctx context.Context, changed []model.WidyaTicket) error {
	missing := false
	for _, record := range changed {
		if record.RowNumber < 2 {
			missing = true
			break
		}
	}
	if !missing {
		return nil
	}
	kept, err := a.store.ListWidyaTickets(ctx)
	if err != nil {
		return fmt.Errorf("read kept tickets: %w", err)
	}
	rows := make(map[string]int, len(kept))
	for _, ticket := range kept {
		rows[ticket.Number] = ticket.RowNumber
	}
	a.mu.Lock()
	for number, ticket := range a.tickets {
		if row, ok := rows[number]; ok {
			ticket.RowNumber = row
			a.tickets[number] = ticket
		}
	}
	a.mu.Unlock()
	for i := range changed {
		row, ok := rows[changed[i].Number]
		if !ok {
			return fmt.Errorf("ticket %s is not in the sheet", changed[i].Number)
		}
		changed[i].RowNumber = row
	}
	return nil
}

// sameLoad reports whether a kept ticket still says what the scanner says.
// The volume is compared to the hundredth the sheet shows.
func sameLoad(kept, read model.WidyaTicket) bool {
	return kept.ScanFullAt.Equal(read.ScanFullAt) &&
		kept.Vehicle == read.Vehicle && kept.Supplier == read.Supplier && kept.Material == read.Material &&
		round2(kept.Volume) == round2(read.Volume)
}

// errNotLoaded is what a page sees before the sheet has been read once.
var errNotLoaded = errors.New("tiket widya belum dimuat")

// Ritase summarises the kept loads scanned full from one day to another. It
// returns the last round's error alongside, so a page can say the figures may
// be behind.
func (a *Archive) Ritase(_ context.Context, from, to string, location *time.Location) (RitaseSummary, bool, error) {
	tickets, has, err := a.between(from, to, location)
	if !has {
		return RitaseSummary{}, false, err
	}
	return Summarize(tickets, from, to, location), true, err
}

// Detail lays one day out unit by unit from the kept loads.
func (a *Archive) Detail(_ context.Context, day string, location *time.Location) (UnitDetailReport, bool, error) {
	tickets, has, err := a.between(day, day, location)
	if !has {
		return UnitDetailReport{}, false, err
	}
	return DetailUnits(tickets, day, location), true, err
}

// TicketsBetween returns the kept loads scanned full from one day to another,
// for a report to lay out its own way.
func (a *Archive) TicketsBetween(_ context.Context, from, to string, location *time.Location) ([]Ticket, bool, error) {
	return a.between(from, to, location)
}

// Vendors names every vendor the kept tickets carry, alphabetically, for a
// filter to offer.
func (a *Archive) Vendors(context.Context) []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	seen := make(map[string]string)
	for _, ticket := range a.tickets {
		name := strings.TrimSpace(ticket.Supplier)
		if name == "" {
			continue
		}
		seen[strings.ToLower(name)] = name
	}
	vendors := make([]string, 0, len(seen))
	for _, name := range seen {
		vendors = append(vendors, name)
	}
	sort.Slice(vendors, func(i, j int) bool { return strings.ToLower(vendors[i]) < strings.ToLower(vendors[j]) })
	return vendors
}

func (a *Archive) between(from, to string, location *time.Location) ([]Ticket, bool, error) {
	if _, err := time.ParseInLocation("2006-01-02", from, location); err != nil {
		return nil, false, err
	}
	if _, err := time.ParseInLocation("2006-01-02", to, location); err != nil {
		return nil, false, err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.loaded {
		if a.lastErr != nil {
			return nil, false, a.lastErr
		}
		return nil, false, errNotLoaded
	}
	var tickets []Ticket
	for _, kept := range a.tickets {
		day := kept.ScanFullAt.In(location).Format("2006-01-02")
		if day < from || day > to {
			continue
		}
		tickets = append(tickets, Ticket{
			Number: kept.Number, Vehicle: kept.Vehicle, Supplier: kept.Supplier,
			Material: kept.Material, Volume: kept.Volume, ScanFullAt: kept.ScanFullAt,
		})
	}
	return tickets, true, a.lastErr
}

func startOfDay(moment time.Time) time.Time {
	return time.Date(moment.Year(), moment.Month(), moment.Day(), 0, 0, 0, 0, moment.Location())
}
