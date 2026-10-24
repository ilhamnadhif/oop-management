package widya

import (
	"context"
	"sync"
	"time"
)

// Source is anything that can read the scanner. The client is one; the tests
// hand in their own.
type Source interface {
	Fetch(ctx context.Context) (Snapshot, error)
	Tickets(ctx context.Context, from, to time.Time) ([]Ticket, error)
}

// Feed keeps the last reading for a while, so a page refreshing itself every
// few seconds, open on several screens, reads the scanner once per interval
// rather than once per screen.
type Feed struct {
	source  Source
	maxAge  time.Duration
	timeout time.Duration
	now     func() time.Time

	mu   sync.Mutex
	last Snapshot
	has  bool
	// days holds the loads of each day that has ended, bucketed by the day
	// they were scanned full. A day that has ended does not change, so it is
	// read once and kept.
	days map[string][]Ticket
	// today is read again every interval, like the figures.
	today        string
	todayTickets []Ticket
	todayAt      time.Time
}

// dayCacheLimit bounds how many ended days are kept, about a year. Each is a
// few hundred small records.
const dayCacheLimit = 400

// createdLead is how far before a day the tickets scanned full on it are
// looked for. The scanner filters on when a ticket was created, which can sit
// before its full scan.
const createdLead = 12 * time.Hour

// NewFeed caches a source's readings for maxAge.
func NewFeed(source Source, maxAge time.Duration, now func() time.Time) *Feed {
	if maxAge <= 0 {
		maxAge = 30 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &Feed{source: source, maxAge: maxAge, timeout: 20 * time.Second, now: now,
		days: make(map[string][]Ticket)}
}

// MaxAge is how often the figures are read again, which is also how often a
// page showing them is worth refreshing.
func (f *Feed) MaxAge() time.Duration { return f.maxAge }

// Latest returns the newest reading. When the scanner cannot be read it returns
// the last reading it had alongside the error, so a page can keep showing the
// figures and say how old they are rather than going blank.
func (f *Feed) Latest(ctx context.Context) (Snapshot, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.has && f.now().Sub(f.last.FetchedAt) < f.maxAge {
		return f.last, true, nil
	}
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	snapshot, err := f.source.Fetch(ctx)
	if err != nil {
		return f.last, f.has, err
	}
	snapshot.FetchedAt = f.now()
	f.last, f.has = snapshot, true
	return snapshot, true, nil
}

// Ritase summarises the loads scanned full from one day to another, both
// included. Days that have ended are read from the scanner once; today is read
// again once per interval. When today cannot be read the last reading of it is
// used and the error returned alongside, so the page can say the figures are
// old rather than going blank.
func (f *Feed) Ritase(ctx context.Context, from, to string, location *time.Location) (RitaseSummary, bool, error) {
	start, err := time.ParseInLocation("2006-01-02", from, location)
	if err != nil {
		return RitaseSummary{}, false, err
	}
	end, err := time.ParseInLocation("2006-01-02", to, location)
	if err != nil {
		return RitaseSummary{}, false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	now := f.now().In(location)
	today := now.Format("2006-01-02")
	if f.today != today {
		// The day turned over: what was today is now an ended day, and is read
		// in full with the others.
		f.today, f.todayTickets, f.todayAt = today, nil, time.Time{}
	}
	if len(f.days) > dayCacheLimit {
		f.days = make(map[string][]Ticket)
	}

	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	// The ended days not yet read, in runs of consecutive days so each run is
	// one read.
	var runStart time.Time
	flush := func(runEnd time.Time) error {
		if runStart.IsZero() {
			return nil
		}
		defer func() { runStart = time.Time{} }()
		tickets, err := f.source.Tickets(ctx, runStart.Add(-createdLead), runEnd.AddDate(0, 0, 1).Add(-time.Second))
		if err != nil {
			return err
		}
		for day := runStart; !day.After(runEnd); day = day.AddDate(0, 0, 1) {
			f.days[day.Format("2006-01-02")] = []Ticket{}
		}
		for _, ticket := range tickets {
			day := ticket.ScanFullAt.In(location).Format("2006-01-02")
			if loads, inRun := f.days[day]; inRun && day >= runStart.Format("2006-01-02") && day <= runEnd.Format("2006-01-02") {
				f.days[day] = append(loads, ticket)
			}
		}
		return nil
	}
	var last time.Time
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		if key >= today {
			break
		}
		if _, read := f.days[key]; read {
			if err := flush(last); err != nil {
				return RitaseSummary{}, false, err
			}
			continue
		}
		if runStart.IsZero() {
			runStart = day
		}
		last = day
	}
	if err := flush(last); err != nil {
		return RitaseSummary{}, false, err
	}

	var todayErr error
	if to >= today && from <= today && now.Sub(f.todayAt) >= f.maxAge {
		dayStart, _ := time.ParseInLocation("2006-01-02", today, location)
		tickets, err := f.source.Tickets(ctx, dayStart.Add(-createdLead), now.Add(time.Hour))
		if err != nil {
			todayErr = err
		} else {
			// The read reaches back before midnight; what was scanned full
			// then belongs to an ended day, read with those.
			kept := make([]Ticket, 0, len(tickets))
			for _, ticket := range tickets {
				if ticket.ScanFullAt.In(location).Format("2006-01-02") == today {
					kept = append(kept, ticket)
				}
			}
			f.todayTickets, f.todayAt = kept, now
		}
	}

	var tickets []Ticket
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		tickets = append(tickets, f.days[day.Format("2006-01-02")]...)
	}
	if to >= today && from <= today {
		tickets = append(tickets, f.todayTickets...)
	}
	return Summarize(tickets, from, to, location), true, todayErr
}
