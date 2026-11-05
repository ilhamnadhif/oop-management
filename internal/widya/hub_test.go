package widya

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"opp-management/internal/model"
)

// hubStore is a ticket store that keeps nothing.
type hubStore struct{}

func (hubStore) ListWidyaTickets(context.Context) ([]model.WidyaTicket, error) { return nil, nil }
func (hubStore) AppendWidyaTickets(context.Context, []model.WidyaTicket) error { return nil }
func (hubStore) UpdateWidyaTickets(context.Context, []model.WidyaTicket) error { return nil }

func TestHubKeepsOneArchivePerProject(t *testing.T) {
	var mu sync.Mutex
	var sheets, accounts []string
	var refuse error
	hub := NewHub(t.Context(),
		func(_ context.Context, spreadsheetID string) (TicketStore, error) {
			mu.Lock()
			defer mu.Unlock()
			if refuse != nil {
				return nil, refuse
			}
			sheets = append(sheets, spreadsheetID)
			return hubStore{}, nil
		},
		func(account model.WidyaAccount) Source {
			mu.Lock()
			defer mu.Unlock()
			accounts = append(accounts, account.URL)
			return &rangeSource{}
		}, wib, time.Hour, time.Now)
	ctx := context.Background()
	pcpm := model.WidyaAccount{URL: "https://star.example", Email: "a@example.com", Password: "satu"}
	kendal := model.WidyaAccount{URL: "https://kendal.example", Email: "b@example.com", Password: "dua"}

	if hub.For("PRJ-0001") != nil {
		t.Fatal("a project with no account has an archive")
	}
	if err := hub.Apply(ctx, "PRJ-0001", "sheet-pcpm", pcpm); err != nil {
		t.Fatalf("apply pcpm: %v", err)
	}
	if err := hub.Apply(ctx, "PRJ-0002", "sheet-kendal", kendal); err != nil {
		t.Fatalf("apply kendal: %v", err)
	}
	first, second := hub.For("PRJ-0001"), hub.For("prj-0002")
	if first == nil || second == nil || first == second {
		t.Fatalf("archives = %p and %p", first, second)
	}
	if len(sheets) != 2 || sheets[0] != "sheet-pcpm" || sheets[1] != "sheet-kendal" ||
		accounts[0] != pcpm.URL || accounts[1] != kendal.URL {
		t.Fatalf("each project should use its own: sheets %v, accounts %v", sheets, accounts)
	}

	// Saving the settings with nothing changed must not restart a sync.
	if err := hub.Apply(ctx, "PRJ-0002", "sheet-kendal", kendal); err != nil {
		t.Fatalf("apply again: %v", err)
	}
	if hub.For("PRJ-0002") != second {
		t.Fatal("the same account restarted the archive")
	}

	// A new password replaces that project's archive and no other.
	changed := kendal
	changed.Password = "tiga"
	if err := hub.Apply(ctx, "PRJ-0002", "sheet-kendal", changed); err != nil {
		t.Fatalf("apply changed: %v", err)
	}
	replaced := hub.For("PRJ-0002")
	if replaced == nil || replaced == second || hub.For("PRJ-0001") != first {
		t.Fatal("a changed account did not replace only its own archive")
	}

	// A spreadsheet out of reach leaves what is running alone.
	refuse = errors.New("sheets down")
	another := changed
	another.Password = "empat"
	if err := hub.Apply(ctx, "PRJ-0002", "sheet-kendal", another); err == nil {
		t.Fatal("a store that cannot be opened was not reported")
	}
	if hub.For("PRJ-0002") != replaced {
		t.Fatal("a failed change dropped the running archive")
	}
	refuse = nil

	// An empty account stops it.
	if err := hub.Apply(ctx, "PRJ-0002", "sheet-kendal", model.WidyaAccount{}); err != nil {
		t.Fatalf("apply empty: %v", err)
	}
	if hub.For("PRJ-0002") != nil || hub.For("PRJ-0001") != first {
		t.Fatal("clearing an account did not stop only its own archive")
	}
}
