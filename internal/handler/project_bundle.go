package handler

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"opp-management/internal/export"
	"opp-management/internal/model"
	"opp-management/internal/service"
)

// ProjectServices is everything that answers for one project. Each set is wired
// over that project's own spreadsheet, so nothing here has to filter by project:
// the rows of another one are not in the file to begin with.
//
// Company and Signatory ride along because they are per project too. The
// letterhead on an export names the site that produced it.
type ProjectServices struct {
	Attendance   *service.AttendanceService
	UnitDT       *service.UnitDTService
	Produksi     *service.ProduksiService
	Overview     *service.OverviewService
	UnitA2B      *service.UnitA2BService
	Nota         *service.NotaService
	Leave        *service.LeaveService
	UnitOverview *service.UnitOverviewService
	FuelMasuk    *service.FuelMasukService
	FuelKeluar   *service.FuelKeluarService
	HourMeter    *service.HourMeterService
	Company      string
	Signatory    export.Signatory
}

// ServiceFactory builds one project's services. It is supplied from outside
// because opening a spreadsheet is not this package's business: the web command
// knows about Google credentials, and the tests hand back services over a store
// held in memory.
type ServiceFactory func(ctx context.Context, project model.Project) (*ProjectServices, error)

// projectCache holds the built service sets, one per project, and builds each
// the first time it is asked for. Building means opening a spreadsheet, so it is
// not done per request; doing it lazily rather than at startup is what lets a
// project added this afternoon be opened without a restart.
type projectCache struct {
	factory ServiceFactory
	mu      sync.Mutex
	built   map[string]*ProjectServices
}

func newProjectCache(factory ServiceFactory) *projectCache {
	return &projectCache{factory: factory, built: make(map[string]*ProjectServices)}
}

func (c *projectCache) get(ctx context.Context, project model.Project) (*ProjectServices, error) {
	key := cacheKey(project)
	if key == "" {
		return nil, fmt.Errorf("project tanpa identitas tidak bisa dibuka")
	}

	c.mu.Lock()
	built, ok := c.built[key]
	c.mu.Unlock()
	if ok {
		return built, nil
	}

	// Built outside the lock: opening a spreadsheet is a round trip, and holding
	// the lock through it would stall every other project's first request too.
	// Two requests racing on the same project each build one, and the loser's
	// copy is dropped - wasteful once, never wrong.
	services, err := c.factory(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("siapkan project %s: %w", project.Nama, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.built[key]; ok {
		return existing, nil
	}
	c.built[key] = services
	return services, nil
}

// forget drops one project's services so the next request builds them again.
// The settings screen calls it: work hours and the signatory are read once when
// a service is built, so a saved setting is only in force after a rebuild.
func (c *projectCache) forget(project model.Project) {
	key := cacheKey(project)
	if key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.built, key)
}

// cacheKey identifies a project by id, falling back to its name for a row typed
// straight into the sheet without one.
func cacheKey(project model.Project) string {
	if id := strings.TrimSpace(project.ProjectID); id != "" {
		return id
	}
	return strings.TrimSpace(project.Nama)
}

// projectNames is the switcher's option list.
func projectNames(projects []model.Project) []string {
	names := make([]string, 0, len(projects))
	for _, project := range projects {
		names = append(names, project.Nama)
	}
	return names
}

// scanGate is the AI scanner's budget: how many scans one account may run, and
// how many may run at once. It belongs to the deployment rather than to any
// project, and is shared by every copy of the server.
type scanGate struct {
	mu    sync.Mutex
	rates map[string]aiScanRateEntry
	slots chan struct{}
}
