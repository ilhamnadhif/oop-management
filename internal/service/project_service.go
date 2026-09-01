package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// projectsCacheTTL keeps the switcher and the access check from re-reading the
// project sheet on every request. Projects are added by hand and settings are
// changed by hand, so a minute of staleness costs nothing; the settings screen
// clears the cache itself the moment it writes.
const projectsCacheTTL = time.Minute

// ProjectService answers which projects exist, what each is configured with,
// and which of them a given person may open.
//
// Every setting it hands out has already been merged with the deployment
// defaults, so a caller never has to know whether a figure came from the sheet
// or from the environment.
type ProjectService struct {
	store    repository.MasterStore
	defaults model.ProjectSettings
	location *time.Location
	now      NowFunc

	mu       sync.Mutex
	cached   []model.Project
	cachedAt time.Time
}

func NewProjectService(store repository.MasterStore, defaults model.ProjectSettings, location *time.Location, now NowFunc) *ProjectService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &ProjectService{store: store, defaults: defaults, location: location, now: now}
}

func projectIDPrefix() string { return "PRJ-" }

// List returns every project the sheet holds, active or not, with settings
// already merged. The order is the sheet's own.
func (s *ProjectService) List(ctx context.Context) ([]model.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cachedAt.After(time.Time{}) && s.now().Sub(s.cachedAt) < projectsCacheTTL {
		return append([]model.Project(nil), s.cached...), nil
	}
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("read projects: %w", err)
	}
	configs, err := s.store.ListExportConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("read export configs: %w", err)
	}
	for i := range projects {
		projects[i].Settings = s.merge(projects[i].Settings)
		projects[i].ExportConfigs = configsFor(projects[i].ProjectID, configs)
	}
	s.cached = projects
	s.cachedAt = s.now()
	return append([]model.Project(nil), projects...), nil
}

// configsFor keeps only the rows belonging to one project.
func configsFor(projectID string, configs []model.ExportConfig) []model.ExportConfig {
	mine := make([]model.ExportConfig, 0, len(configs))
	for _, config := range configs {
		if strings.EqualFold(strings.TrimSpace(config.ProjectID), strings.TrimSpace(projectID)) {
			mine = append(mine, config)
		}
	}
	return mine
}

// Active returns the projects that may be opened, in sheet order.
func (s *ProjectService) Active(ctx context.Context) ([]model.Project, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	active := make([]model.Project, 0, len(all))
	for _, project := range all {
		if project.Aktif() {
			active = append(active, project)
		}
	}
	return active, nil
}

// Find returns one project by name. Names are what the session and the user
// sheet carry, because an operator reading either should recognise what it says.
func (s *ProjectService) Find(ctx context.Context, nama string) (model.Project, error) {
	all, err := s.List(ctx)
	if err != nil {
		return model.Project{}, err
	}
	nama = strings.TrimSpace(nama)
	for _, project := range all {
		if strings.EqualFold(strings.TrimSpace(project.Nama), nama) {
			return project, nil
		}
	}
	return model.Project{}, fmt.Errorf("%w: Project %q tidak dikenal", ErrValidation, nama)
}

// Reachable lists the projects this account may open, in sheet order. An
// account that reaches everything gets every active project; anybody else gets
// the one they belong to, and nothing when that project is closed or gone.
//
// An account written before projects existed carries no project at all. It is
// given the first active one rather than none: reading a blank cell as "no
// access" would lock out everybody who already had an account.
func (s *ProjectService) Reachable(ctx context.Context, user *model.User) ([]model.Project, error) {
	if user == nil {
		return nil, nil
	}
	active, err := s.Active(ctx)
	if err != nil {
		return nil, err
	}
	if user.ReachesEveryProject() {
		return active, nil
	}
	assigned := strings.TrimSpace(user.Project)
	if assigned == "" {
		if len(active) == 0 {
			return nil, nil
		}
		return active[:1], nil
	}
	for _, project := range active {
		if strings.EqualFold(strings.TrimSpace(project.Nama), assigned) {
			return []model.Project{project}, nil
		}
	}
	return nil, nil
}

// Resolve settles which project a request is working in. The one already in the
// session is kept when the person may still open it, so a switch survives until
// it is switched again; otherwise the first project they can reach stands in.
func (s *ProjectService) Resolve(ctx context.Context, user *model.User, current string) (model.Project, error) {
	reachable, err := s.Reachable(ctx, user)
	if err != nil {
		return model.Project{}, err
	}
	if len(reachable) == 0 {
		return model.Project{}, fmt.Errorf("%w: Akun ini belum ditugaskan ke project mana pun", ErrValidation)
	}
	current = strings.TrimSpace(current)
	for _, project := range reachable {
		if strings.EqualFold(strings.TrimSpace(project.Nama), current) {
			return project, nil
		}
	}
	return reachable[0], nil
}

// EnsureFirst writes the opening row of the project sheet when it is empty: the
// project the app was already keeping books for, pointing at the spreadsheet it
// has always written to.
//
// It is what carries an existing deployment across. Without it the first start
// after the upgrade would find no projects and let nobody in.
func (s *ProjectService) EnsureFirst(ctx context.Context, nama, spreadsheetID string) (model.Project, error) {
	existing, err := s.List(ctx)
	if err != nil {
		return model.Project{}, err
	}
	if len(existing) > 0 {
		return existing[0], nil
	}
	now := s.now().In(s.location)
	project := &model.Project{
		ProjectID:     fmt.Sprintf("%s%04d", projectIDPrefix(), 1),
		Nama:          strings.TrimSpace(nama),
		SpreadsheetID: strings.TrimSpace(spreadsheetID),
		// No menus listed means every menu, which is what the app had before
		// projects could switch any of them off.
		MenuAktif: nil,
		Status:    model.StatusAktif,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.CreateProject(ctx, project); err != nil {
		return model.Project{}, fmt.Errorf("create first project: %w", err)
	}
	s.invalidate()
	project.Settings = s.merge(project.Settings)
	return *project, nil
}

// Provisioner prepares a spreadsheet to hold one project's books: it opens the
// file and writes the sheets and headers into it. It is supplied from outside
// because opening a spreadsheet is not this package's business.
type Provisioner func(ctx context.Context, spreadsheetID string) error

// Create adds a project. The spreadsheet itself is made by hand and its id
// pasted in - an app that can create spreadsheets can also fill a drive with
// them - but the sheets inside it are written here, before the project row is.
//
// Doing it in that order is what makes the error land where it can be acted on.
// Left until the project is first opened, a mistyped id or a file nobody shared
// with the service account showed up later as a project that would not open,
// with the person who could fix it long gone from the screen that caused it.
func (s *ProjectService) Create(ctx context.Context, nama, spreadsheetID string, menus []string, provision Provisioner) (model.Project, error) {
	nama = strings.Join(strings.Fields(nama), " ")
	if nama == "" {
		return model.Project{}, fmt.Errorf("%w: Nama project wajib diisi", ErrValidation)
	}
	spreadsheetID = strings.TrimSpace(spreadsheetID)
	if spreadsheetID == "" {
		return model.Project{}, fmt.Errorf("%w: Spreadsheet ID wajib diisi", ErrValidation)
	}

	existing, err := s.List(ctx)
	if err != nil {
		return model.Project{}, err
	}
	for _, project := range existing {
		if strings.EqualFold(strings.TrimSpace(project.Nama), nama) {
			return model.Project{}, fmt.Errorf("%w: Project %q sudah ada", ErrValidation, nama)
		}
		// Two projects sharing a spreadsheet would each write into the other's
		// books, which is the one thing this whole arrangement exists to stop.
		if strings.EqualFold(strings.TrimSpace(project.SpreadsheetID), spreadsheetID) {
			return model.Project{}, fmt.Errorf("%w: Spreadsheet itu sudah dipakai project %s", ErrValidation, project.Nama)
		}
	}

	// After the checks above, so a name or a file that is going to be refused is
	// refused before anything is written into anybody's spreadsheet.
	if provision != nil {
		if err := provision(ctx, spreadsheetID); err != nil {
			// Reported as a validation failure because that is what it is from
			// where the person is standing: something they typed, that they can
			// retype. The page shows the reason rather than logging it away.
			return model.Project{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
		}
	}

	highest, err := s.store.MaxProjectSequence(ctx, projectIDPrefix())
	if err != nil {
		return model.Project{}, fmt.Errorf("read last project id: %w", err)
	}
	now := s.now().In(s.location)
	project := &model.Project{
		ProjectID:     fmt.Sprintf("%s%04d", projectIDPrefix(), highest+1),
		Nama:          nama,
		SpreadsheetID: spreadsheetID,
		MenuAktif:     cleanMenus(menus),
		Status:        model.StatusAktif,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.store.CreateProject(ctx, project); err != nil {
		return model.Project{}, fmt.Errorf("create project: %w", err)
	}
	s.invalidate()
	project.Settings = s.merge(project.Settings)
	return *project, nil
}

// ProjectUpdate is what the settings screen may change. The spreadsheet id is
// not among them: repointing a project at another file would orphan every row
// already written into this one.
type ProjectUpdate struct {
	Nama                 string
	MenuAktif            []string
	Status               string
	WorkStart            string
	WorkEnd              string
	LateToleranceMinutes int
	A2BWorkMinutes       int
	Company              string
	SignatoryName        string
	SignatoryTitle       string
	SignatoryPlace       string
}

// Update saves the settings screen. Blank settings are stored blank rather than
// filled in with the defaults, so a project that has never been configured
// keeps following the deployment instead of freezing today's values into a row.
func (s *ProjectService) Update(ctx context.Context, projectID string, update ProjectUpdate) (model.Project, error) {
	nama := strings.Join(strings.Fields(update.Nama), " ")
	if nama == "" {
		return model.Project{}, fmt.Errorf("%w: Nama project wajib diisi", ErrValidation)
	}
	if err := validateOptionalClock("Jam masuk", update.WorkStart); err != nil {
		return model.Project{}, err
	}
	if err := validateOptionalClock("Jam pulang", update.WorkEnd); err != nil {
		return model.Project{}, err
	}
	if update.LateToleranceMinutes < 0 {
		return model.Project{}, fmt.Errorf("%w: Toleransi telat tidak boleh minus", ErrValidation)
	}
	if update.A2BWorkMinutes < 0 {
		return model.Project{}, fmt.Errorf("%w: Menit kerja A2B tidak boleh minus", ErrValidation)
	}

	stored, rowNumber, err := s.store.FindProjectRow(ctx, projectID)
	if err != nil {
		return model.Project{}, fmt.Errorf("read project %s: %w", projectID, err)
	}

	all, err := s.List(ctx)
	if err != nil {
		return model.Project{}, err
	}
	for _, project := range all {
		if strings.EqualFold(project.ProjectID, stored.ProjectID) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(project.Nama), nama) {
			return model.Project{}, fmt.Errorf("%w: Project %q sudah ada", ErrValidation, nama)
		}
	}

	status := strings.ToUpper(strings.TrimSpace(update.Status))
	if status != model.StatusAktif && status != model.StatusTidakAktif {
		status = model.StatusAktif
	}

	stored.Nama = nama
	stored.MenuAktif = cleanMenus(update.MenuAktif)
	stored.Status = status
	stored.Settings = model.ProjectSettings{
		WorkStart:            strings.TrimSpace(update.WorkStart),
		WorkEnd:              strings.TrimSpace(update.WorkEnd),
		LateToleranceMinutes: update.LateToleranceMinutes,
		A2BWorkMinutes:       update.A2BWorkMinutes,
		Company:              strings.TrimSpace(update.Company),
		SignatoryName:        strings.TrimSpace(update.SignatoryName),
		SignatoryTitle:       strings.TrimSpace(update.SignatoryTitle),
		SignatoryPlace:       strings.TrimSpace(update.SignatoryPlace),
	}
	stored.UpdatedAt = s.now().In(s.location)

	if err := s.store.UpdateProject(ctx, rowNumber, stored); err != nil {
		return model.Project{}, fmt.Errorf("update project: %w", err)
	}
	s.invalidate()
	stored.Settings = s.merge(stored.Settings)
	return *stored, nil
}

// ExportConfig is what the settings screen may change for one export type:
// whether it may be downloaded, and how its signature block is laid out.
func (s *ProjectService) SaveExportConfig(ctx context.Context, config model.ExportConfig) error {
	if strings.TrimSpace(config.ProjectID) == "" {
		return fmt.Errorf("%w: project tidak dikenal", ErrValidation)
	}
	known := false
	for _, key := range model.ExportTypeKeys {
		if strings.EqualFold(strings.TrimSpace(config.ExportKey), string(key)) {
			config.ExportKey = string(key)
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("%w: jenis export tidak terdaftar", ErrValidation)
	}
	if config.TTDCount < 1 || config.TTDCount > 3 {
		return fmt.Errorf("%w: jumlah tanda tangan harus 1, 2, atau 3", ErrValidation)
	}
	if err := s.store.SaveExportConfig(ctx, config); err != nil {
		return fmt.Errorf("save export config: %w", err)
	}
	s.invalidate()
	return nil
}

// Assign puts one account into a project, or into every project when the name
// given is model.ProjectSemua.
func (s *ProjectService) Assign(ctx context.Context, userID, project string) error {
	project = strings.Join(strings.Fields(project), " ")
	if project == "" {
		return fmt.Errorf("%w: Project wajib dipilih", ErrValidation)
	}
	if project != model.ProjectSemua {
		known, err := s.Find(ctx, project)
		if err != nil {
			return err
		}
		project = known.Nama
	}
	_, rowNumber, err := s.store.FindUserRow(ctx, userID)
	if err != nil {
		return fmt.Errorf("read user %s: %w", userID, err)
	}
	if err := s.store.UpdateUserProject(ctx, rowNumber, project, s.now().In(s.location)); err != nil {
		return fmt.Errorf("assign user to project: %w", err)
	}
	return nil
}

// Members lists the accounts belonging to a project, plus the ones that reach
// every project, so the settings screen shows who is actually there.
func (s *ProjectService) Members(ctx context.Context, nama string) ([]model.User, error) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	// An account with no project named is read as belonging to the first one,
	// the same way Reachable reads it: those are the accounts that existed
	// before projects did, and they all belong to the project that was here
	// first.
	first := ""
	if active, err := s.Active(ctx); err == nil && len(active) > 0 {
		first = strings.TrimSpace(active[0].Nama)
	}
	nama = strings.TrimSpace(nama)
	members := make([]model.User, 0, len(users))
	for _, user := range users {
		assigned := strings.TrimSpace(user.Project)
		if assigned == "" {
			assigned = first
		}
		if user.ReachesEveryProject() || strings.EqualFold(assigned, nama) {
			members = append(members, user)
		}
	}
	return members, nil
}

// AllUsers is every account, for the screen that shows several projects at
// once. Membership is worked out by the caller from one read rather than by
// asking this service once per project.
func (s *ProjectService) AllUsers(ctx context.Context) ([]model.User, error) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	return users, nil
}

// UserLister hands a project's own service the accounts that belong to it. The
// accounts live in the master spreadsheet while attendance and leave live in
// the project's, so the two have to be introduced rather than read together.
type UserLister func(ctx context.Context) ([]model.User, error)

// MembersOf is Members bound to one project, in the shape the services take.
func (s *ProjectService) MembersOf(nama string) UserLister {
	return func(ctx context.Context) ([]model.User, error) {
		return s.Members(ctx, nama)
	}
}

// invalidate drops the cache so a setting just saved is in force on the very
// next request rather than up to a minute later.
func (s *ProjectService) invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cachedAt = time.Time{}
}

// Invalidate is invalidate for callers outside this package, used when a
// project's store is rebuilt and the settings behind it have to be re-read.
func (s *ProjectService) Invalidate() { s.invalidate() }

// merge fills every blank setting from the deployment defaults. A project row
// therefore only has to carry what it wants to differ in.
func (s *ProjectService) merge(settings model.ProjectSettings) model.ProjectSettings {
	if settings.WorkStart == "" {
		settings.WorkStart = s.defaults.WorkStart
	}
	if settings.WorkEnd == "" {
		settings.WorkEnd = s.defaults.WorkEnd
	}
	if settings.LateToleranceMinutes <= 0 {
		settings.LateToleranceMinutes = s.defaults.LateToleranceMinutes
	}
	if settings.A2BWorkMinutes <= 0 {
		settings.A2BWorkMinutes = s.defaults.A2BWorkMinutes
	}
	if settings.Company == "" {
		settings.Company = s.defaults.Company
	}
	if settings.SignatoryName == "" {
		settings.SignatoryName = s.defaults.SignatoryName
	}
	if settings.SignatoryTitle == "" {
		settings.SignatoryTitle = s.defaults.SignatoryTitle
	}
	if settings.SignatoryPlace == "" {
		settings.SignatoryPlace = s.defaults.SignatoryPlace
	}
	return settings
}

// cleanMenus normalises the ticked menu keys, dropping blanks and repeats while
// keeping the order they were given in.
func cleanMenus(menus []string) []string {
	seen := make(map[string]bool, len(menus))
	cleaned := make([]string, 0, len(menus))
	for _, menu := range menus {
		menu = strings.TrimSpace(menu)
		if menu == "" || seen[strings.ToLower(menu)] {
			continue
		}
		seen[strings.ToLower(menu)] = true
		cleaned = append(cleaned, menu)
	}
	return cleaned
}

// validateOptionalClock accepts an empty time, which means the project follows
// the deployment default rather than a time of its own.
func validateOptionalClock(label, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if _, err := time.Parse("15:04", value); err != nil {
		return fmt.Errorf("%w: %s harus berformat HH:MM", ErrValidation, label)
	}
	return nil
}
