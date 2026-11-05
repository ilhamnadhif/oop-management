package widya

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
)

// StoreOpener reaches the spreadsheet a project keeps its tickets in.
type StoreOpener func(ctx context.Context, spreadsheetID string) (TicketStore, error)

// SourceOpener reaches the scanner an account signs in to.
type SourceOpener func(account model.WidyaAccount) Source

// Hub runs one archive per project with a scanner, each signing in with that
// project's own account and keeping its tickets in that project's own
// spreadsheet. Accounts change while the app runs, from the settings screen, so
// an archive can be stopped and replaced without touching the others.
type Hub struct {
	ctx        context.Context
	openStore  StoreOpener
	openSource SourceOpener
	location   *time.Location
	interval   time.Duration
	now        func() time.Time

	// applying takes turns between changes, which reach a spreadsheet and are
	// slow; mu guards only the map, so a page is never held up by one.
	applying sync.Mutex
	mu       sync.RWMutex
	running  map[string]*hubArchive
}

type hubArchive struct {
	account       model.WidyaAccount
	spreadsheetID string
	archive       *Archive
	cancel        context.CancelFunc
	done          chan struct{}
}

// NewHub prepares a hub. Every archive stops with ctx.
func NewHub(ctx context.Context, openStore StoreOpener, openSource SourceOpener, location *time.Location, interval time.Duration, now func() time.Time) *Hub {
	return &Hub{ctx: ctx, openStore: openStore, openSource: openSource, location: location,
		interval: interval, now: now, running: make(map[string]*hubArchive)}
}

func hubKey(projectID string) string {
	return strings.ToUpper(strings.TrimSpace(projectID))
}

// For returns a project's archive, or nil when it has no scanner.
func (h *Hub) For(projectID string) *Archive {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if entry := h.running[hubKey(projectID)]; entry != nil {
		return entry.archive
	}
	return nil
}

// Apply brings a project in line with its account: an account with nothing in
// it stops the archive, a new one replaces it, and the same one changes
// nothing. When the spreadsheet cannot be reached the running archive is left
// as it is.
func (h *Hub) Apply(ctx context.Context, projectID, spreadsheetID string, account model.WidyaAccount) error {
	h.applying.Lock()
	defer h.applying.Unlock()

	key := hubKey(projectID)
	h.mu.RLock()
	current := h.running[key]
	h.mu.RUnlock()

	if !account.Configured() {
		if current != nil {
			current.cancel()
			h.mu.Lock()
			delete(h.running, key)
			h.mu.Unlock()
		}
		return nil
	}
	if current != nil && current.account == account && current.spreadsheetID == spreadsheetID {
		return nil
	}

	store, err := h.openStore(ctx, spreadsheetID)
	if err != nil {
		return fmt.Errorf("open tickets of project %s: %w", projectID, err)
	}
	run, cancel := context.WithCancel(h.ctx)
	entry := &hubArchive{
		account: account, spreadsheetID: spreadsheetID, cancel: cancel, done: make(chan struct{}),
		archive: NewArchive(h.openSource(account), store, h.location, h.interval, h.now),
	}
	h.mu.Lock()
	h.running[key] = entry
	h.mu.Unlock()

	if current != nil {
		current.cancel()
	}
	go func() {
		defer close(entry.done)
		// Two archives writing one sheet would append the same ticket twice,
		// so the new one waits for the old one to finish its round.
		if current != nil {
			<-current.done
		}
		entry.archive.Run(run)
	}()
	return nil
}
