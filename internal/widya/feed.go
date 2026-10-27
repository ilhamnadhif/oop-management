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
	TicketsUpdatedSince(ctx context.Context, since time.Time) ([]Ticket, error)
}

// Feed keeps the last reading of the scanner's figures for a while, so a page
// refreshing itself every few seconds, open on several screens, reads the
// scanner once per interval rather than once per screen. The tickets are kept
// by the Archive instead.
type Feed struct {
	source  Source
	maxAge  time.Duration
	timeout time.Duration
	now     func() time.Time

	mu   sync.Mutex
	last Snapshot
	has  bool
}

// NewFeed caches a source's readings for maxAge.
func NewFeed(source Source, maxAge time.Duration, now func() time.Time) *Feed {
	if maxAge <= 0 {
		maxAge = 30 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &Feed{source: source, maxAge: maxAge, timeout: 20 * time.Second, now: now}
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
