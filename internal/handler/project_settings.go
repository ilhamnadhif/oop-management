package handler

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"opp-management/internal/model"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

// ProjectSettingsPageData drives two views from one route. Opening the page
// shows the list and nothing else; picking a project opens its own page. A
// screen that showed every form for every project at once was unreadable, and
// most visits only want to see what exists.
type ProjectSettingsPageData struct {
	ShellPageData
	// Cards is the list view: one entry per project, already worded.
	Cards []ProjectCard
	// Selected is the project being edited. Its ProjectID is empty on the list
	// view, which is what the template branches on.
	Selected model.Project
	// Menus is every module that exists, with a tick against the ones this
	// project runs. It is built from the sidebar's own list, so a module added
	// to the app appears here without this page being touched.
	Menus []MenuChoice
	// Marks are the project's three uploads, worded for the form. They are
	// built rather than written into the template so the field name the form
	// posts and the column it lands in are named once.
	Marks []ProjectMark
	// Exports is every configurable report with this project's setting against
	// it: whether it may be downloaded, and how its signature block is laid out.
	Exports []ExportChoice
	Members []ProjectMember
	// Assignable is what the member dropdown offers: every project by name,
	// plus the entry that means all of them.
	Assignable []string
	// OpenNew asks the browser to show the add-project dialog on load, which is
	// how the page comes back after a refused submission.
	OpenNew bool
	// NewError is shown inside that dialog, and NewNama and NewSpreadsheetID
	// put back what was typed. A message about the dialog's own form belongs in
	// the dialog: on the page behind it, it is covered by the backdrop.
	NewError         string
	NewNama          string
	NewSpreadsheetID string
	SemuaProyek      string
	Error            string
	Success          string
}

// ProjectCard is one project as the list shows it: what it is called, what it
// runs, and how many people are in it.
type ProjectCard struct {
	model.Project
	// MenuLabels are the modules this project runs, in sidebar order and worded
	// the way the sidebar words them. Empty means every module.
	MenuLabels []string
	// SemuaMenu says the project lists no modules, which means it runs them all.
	SemuaMenu bool
	Anggota   int
	Aktif     bool
	// Active marks the project this session is currently working in.
	Active bool
}

// MenuChoice is one module and whether this project runs it.
type MenuChoice struct {
	Key    string
	Label  string
	Aktif  bool
	Lede   string
	Locked bool
}

// ProjectMember is one account on the settings screen, with the project it is
// assigned to shown as it is stored rather than as it is resolved.
type ProjectMember struct {
	model.User
	// Assigned is what the dropdown should preselect: the stored project, or
	// the entry meaning every project.
	Assigned string
}

func (s *Server) handleProjectSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleProjectSettingsPage(w, r)
	case http.MethodPost:
		s.handleProjectSettingsSave(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleProjectSettingsPage(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "project-settings")
	if !ok {
		return
	}
	view := projectSettingsView{
		selected: strings.TrimSpace(r.URL.Query().Get("project")),
		openNew:  r.URL.Query().Get("tambah") == "1",
	}
	s.renderProjectSettings(w, r, user, sessionValue, view, http.StatusOK)
}

// projectSettingsView is which of the two views to draw and what to say on it.
type projectSettingsView struct {
	// selected names the project to edit. Empty draws the list.
	selected string
	openNew  bool
	errMsg   string
	success  string
	// newErr belongs to the add-project dialog rather than to the page. A
	// refusal reported at the top of the page would sit behind the dialog's own
	// backdrop, which is to say nowhere.
	newErr string
	// What was typed, handed back so a refusal does not also mean retyping it.
	newNama          string
	newSpreadsheetID string
}

// handleProjectSettingsSave answers three forms from one route. They are one
// screen to the person using it, and splitting them would mean three redirects
// that all land back here.
func (s *Server) handleProjectSettingsSave(w http.ResponseWriter, r *http.Request) {
	sessionValue, ok := s.currentSession(r)
	if !ok {
		redirect(w, r, "/login")
		return
	}
	user, err := s.auth.LoadUser(r.Context(), sessionValue.UserID)
	if err != nil || user.StatusPengguna != model.StatusAktif {
		s.sessions.Delete(r, w)
		redirect(w, r, "/login")
		return
	}
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "project-settings")
	if !okProject {
		return
	}

	// The settings form carries the project's three marks, so it is multipart.
	// The body is bounded before it is parsed, and the token is then read out
	// of the parsed form the way every other upload form on this app does it.
	maxBody := 3*s.maxUploadBytes + 64*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Form tidak valid atau gambar terlalu besar", http.StatusUnprocessableEntity)
			return
		}
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	if !s.sessions.ValidCSRFToken(r.FormValue("csrf_token"), sessionValue) {
		http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
		return
	}

	view := projectSettingsView{selected: strings.TrimSpace(r.FormValue("project_nama"))}
	var actionErr error
	switch strings.TrimSpace(r.FormValue("aksi")) {
	case "tambah":
		nama := strings.TrimSpace(r.FormValue("nama_baru"))
		spreadsheetID := strings.TrimSpace(r.FormValue("spreadsheet_id"))
		created, err := s.projects.Create(r.Context(), nama, spreadsheetID, nil, s.provision)
		// The list is what this page is for, so a new project appears there
		// rather than dragging the person into a form they did not ask for.
		view.selected = ""
		if err == nil {
			view.success = "Project " + created.Nama + " dibuat. Buka Atur untuk memilih menu dan setelannya."
			break
		}
		// Answered inside the dialog, which reopens holding what was typed. The
		// shared error path below would put it on the page instead, where the
		// dialog covers it.
		view.openNew = true
		view.newNama = nama
		view.newSpreadsheetID = spreadsheetID
		view.newErr = "Project tidak bisa ditambahkan"
		if errors.Is(err, service.ErrValidation) {
			view.newErr = strings.TrimPrefix(err.Error(), "validation error: ")
		} else {
			log.Printf("create project: %v", err)
		}
		s.renderProjectSettings(w, r, user, sessionValue, view, http.StatusUnprocessableEntity)
		return
	case "simpan":
		marks, markErr := s.readProjectMarks(r)
		if markErr != nil {
			view.errMsg = markErr.Error()
			s.renderProjectSettings(w, r, user, sessionValue, view, http.StatusUnprocessableEntity)
			return
		}
		updated, err := s.projects.Update(r.Context(), strings.TrimSpace(r.FormValue("project_id")), service.ProjectUpdate{
			Nama:                 r.FormValue("nama"),
			MenuAktif:            r.Form["menu"],
			Status:               r.FormValue("status"),
			WorkStart:            r.FormValue("work_start"),
			WorkEnd:              r.FormValue("work_end"),
			LateToleranceMinutes: optionalMinutes(r.FormValue("late_tolerance")),
			A2BWorkMinutes:       optionalMinutes(r.FormValue("a2b_work_minutes")),
			Company:              r.FormValue("company"),
			SignatoryPlace:       r.FormValue("signatory_place"),
			LogoSistem:           marks.LogoSistem,
			LogoExport:           marks.LogoExport,
			Favicon:              marks.Favicon,
			ClearLogoSistem:      marks.ClearLogoSistem,
			ClearLogoExport:      marks.ClearLogoExport,
			ClearFavicon:         marks.ClearFavicon,
		})
		actionErr = err
		if err == nil {
			view.selected = updated.Nama
			// The services were built from the old settings, so they are dropped
			// rather than left holding figures the sheet no longer says.
			s.bundles.forget(updated)
			view.success = "Setelan project " + updated.Nama + " tersimpan."
		}
	case "export":
		config := exportConfigFromForm(r, strings.TrimSpace(r.FormValue("project_id")))
		actionErr = s.projects.SaveExportConfig(r.Context(), config)
		if actionErr == nil {
			// Nothing to drop from the service cache: the closing block is read
			// off the project on each request, and saving invalidated that list.
			label := exportLabels[model.ExportTypeKey(config.ExportKey)]
			if label == "" {
				label = "Export"
			}
			view.success = "Setelan export " + label + " tersimpan."
		}
	case "tugaskan":
		actionErr = s.projects.Assign(r.Context(), strings.TrimSpace(r.FormValue("user_id")), r.FormValue("user_project"))
		if actionErr == nil {
			view.success = "Penugasan pengguna tersimpan."
		}
	default:
		actionErr = errors.New("aksi tidak dikenal")
	}

	if actionErr != nil {
		view.errMsg = "Perubahan tidak bisa disimpan"
		if errors.Is(actionErr, service.ErrValidation) {
			view.errMsg = strings.TrimPrefix(actionErr.Error(), "validation error: ")
		} else {
			log.Printf("project settings: %v", actionErr)
		}
		s.renderProjectSettings(w, r, user, sessionValue, view, http.StatusUnprocessableEntity)
		return
	}
	s.renderProjectSettings(w, r, user, sessionValue, view, http.StatusOK)
}

func (s *Server) renderProjectSettings(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, view projectSettingsView, status int) {
	projects, err := s.projects.List(r.Context())
	if err != nil {
		log.Printf("list projects: %v", err)
		if view.errMsg == "" {
			view.errMsg = "Daftar project gagal dimuat"
		}
	}

	// One read of the accounts serves every card, rather than one per project.
	users, err := s.projects.AllUsers(r.Context())
	if err != nil {
		log.Printf("list users: %v", err)
	}

	data := s.shellData(user, sessionValue, "project-settings")
	page := ProjectSettingsPageData{
		ShellPageData:    data,
		Assignable:       append(projectNames(projects), model.ProjectSemua),
		OpenNew:          view.openNew,
		NewError:         view.newErr,
		NewNama:          view.newNama,
		NewSpreadsheetID: view.newSpreadsheetID,
		SemuaProyek:      model.ProjectSemua,
		Error:            view.errMsg,
		Success:          view.success,
	}

	// An empty name draws the list. That is what opening the page does, and
	// what saving something that is not one project's settings returns to.
	if strings.TrimSpace(view.selected) == "" {
		page.Cards = projectCards(projects, users, s.project.Nama, firstProjectName(projects))
		page.PageTitle = "Project"
		s.render(w, "project_settings", page, status)
		return
	}

	for _, project := range projects {
		if strings.EqualFold(strings.TrimSpace(project.Nama), strings.TrimSpace(view.selected)) {
			page.Selected = project
			break
		}
	}
	if page.Selected.ProjectID == "" {
		// The name named nothing. Falling back to the list is better than an
		// empty form: it says what does exist.
		page.Cards = projectCards(projects, users, s.project.Nama, firstProjectName(projects))
		if page.Error == "" {
			page.Error = "Project " + view.selected + " tidak ditemukan."
		}
		s.render(w, "project_settings", page, status)
		return
	}

	page.Menus = menuChoicesFor(page.Selected)
	page.Marks = projectMarksFor(page.Selected)
	page.Exports = exportChoicesFor(page.Selected)
	page.Members = membersOf(users, page.Selected.Nama, firstProjectName(projects))
	page.PageTitle = page.Selected.Nama
	page.Breadcrumb = page.Selected.Nama
	page.Section = "Project"
	page.Lede = "Menu, jam kerja, kop laporan, dan pengguna project ini."
	s.render(w, "project_settings", page, status)
}

// projectCards words the list. The member count comes from one read of the
// accounts shared across every card.
func projectCards(projects []model.Project, users []model.User, activeName, firstName string) []ProjectCard {
	cards := make([]ProjectCard, 0, len(projects))
	for _, project := range projects {
		card := ProjectCard{
			Project:   project,
			SemuaMenu: len(project.MenuAktif) == 0,
			Anggota:   len(membersOf(users, project.Nama, firstName)),
			Aktif:     project.Aktif(),
			Active:    strings.EqualFold(strings.TrimSpace(project.Nama), strings.TrimSpace(activeName)),
		}
		// Worded and ordered the way the sidebar does, so the list reads as the
		// menu somebody in that project would see.
		for _, item := range navItems {
			if _, isModule := menuAccess[item.Key]; !isModule || item.Key == projectSettingsKey {
				continue
			}
			if !card.SemuaMenu && project.HasMenu(item.Key) {
				card.MenuLabels = append(card.MenuLabels, item.Label)
			}
		}
		cards = append(cards, card)
	}
	return cards
}

// membersOf is the membership rule applied to a list already read: the people
// assigned here, plus the ones who reach everywhere, plus the accounts written
// before projects existed when this is the project that was here first.
func membersOf(users []model.User, nama, firstName string) []ProjectMember {
	members := make([]ProjectMember, 0, len(users))
	for _, user := range users {
		assigned := strings.TrimSpace(user.Project)
		if assigned == "" {
			assigned = firstName
		}
		if !user.ReachesEveryProject() && !strings.EqualFold(assigned, strings.TrimSpace(nama)) {
			continue
		}
		shown := assigned
		if user.ReachesEveryProject() {
			shown = model.ProjectSemua
		}
		members = append(members, ProjectMember{User: user, Assigned: shown})
	}
	return members
}

// firstProjectName is the project accounts written before projects existed
// belong to.
func firstProjectName(projects []model.Project) string {
	for _, project := range projects {
		if project.Aktif() {
			return strings.TrimSpace(project.Nama)
		}
	}
	return ""
}

// menuChoicesFor lists every module with a tick against the ones this project
// runs. It reads the sidebar rather than a list of its own, so a module added
// to navItems shows up here on its own.
func menuChoicesFor(project model.Project) []MenuChoice {
	choices := make([]MenuChoice, 0, len(navItems))
	for _, item := range navItems {
		// A page open to everybody is not a module anybody chooses, and the
		// settings screen must never be switchable off.
		if _, restricted := menuAccess[item.Key]; !restricted {
			continue
		}
		locked := item.Key == projectSettingsKey
		choices = append(choices, MenuChoice{
			Key:    item.Key,
			Label:  item.Label,
			Aktif:  locked || project.HasMenu(item.Key),
			Lede:   moduleLede(item),
			Locked: locked,
		})
	}
	return choices
}

// moduleLede says what a module contains. A group has no sentence of its own -
// its pages carry those - so the pages are named instead, which is also what
// switching the module off actually takes away.
func moduleLede(item NavItem) string {
	if len(item.Children) == 0 {
		return item.Lede
	}
	labels := make([]string, 0, len(item.Children))
	for _, child := range item.Children {
		labels = append(labels, child.Label)
	}
	return strings.Join(labels, " · ")
}

// optionalMinutes reads a figure that may be left blank, which means the
// project follows the deployment default. Anything unreadable is treated the
// same way rather than refused: the field offers a number or nothing.
func optionalMinutes(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	minutes, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return minutes
}
