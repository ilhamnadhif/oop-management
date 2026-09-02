package handler

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"opp-management/internal/model"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

// UserManagementPageData drives the HR screen that moves people between
// positions and decides which menus each position may open. The two edits are
// one page because they answer the same question: who may do what here.
type UserManagementPageData struct {
	ShellPageData
	// Members is every account in this project, with the dropdown options each
	// row may be moved to.
	Members []UserManagementMember
	// SemuaProyekMark is what the Assigned column holds for an account that
	// reaches every project.
	SemuaProyekMark string
	// AccessMenus is one column per top-level menu that can be granted, with
	// the locked ones marked.
	AccessMenus []MenuChoice
	// AccessRows is one row per position with a tick against every menu it may
	// open.
	AccessRows []JabatanAccessRow
	// ProjectNama names the project every edit on this page belongs to. Nothing
	// here reaches another site, and the page says so rather than leaving it to
	// be assumed.
	ProjectNama string
	// JabatanBaru is what was typed into the add-position form, handed back so a
	// refusal does not also mean retyping it.
	JabatanBaru string
	// NamaJabatanMax is the longest name the server will take.
	NamaJabatanMax int
	Error          string
	Success        string
}

// UserManagementMember is one account on the move-position table. Options is
// the closed set its row's dropdown may choose from, which is what the acting
// person may assign rather than every position that exists.
type UserManagementMember struct {
	ProjectMember
	Options []string
}

// JabatanAccessRow is one row of the access matrix: a position and whether
// each configurable menu is open to it.
type JabatanAccessRow struct {
	Jabatan string
	Menus   []MenuChoice
}

func (s *Server) handleUserManagement(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleUserManagementPage(w, r)
	case http.MethodPost:
		s.handleUserManagementSave(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUserManagementPage(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "hr-user-management")
	if !ok {
		return
	}
	s.renderUserManagement(w, r, user, sessionValue, "", "", http.StatusOK)
}

// handleUserManagementSave answers two forms from one route: moving one person
// to a different position, and saving the whole access matrix.
func (s *Server) handleUserManagementSave(w http.ResponseWriter, r *http.Request) {
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusUnprocessableEntity)
		return
	}
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "hr-user-management")
	if !okProject {
		return
	}
	if !s.sessions.ValidCSRF(r, sessionValue) {
		http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
		return
	}

	switch strings.TrimSpace(r.FormValue("aksi")) {
	case "ubah-jabatan":
		userID := strings.TrimSpace(r.FormValue("user_id"))
		jabatan := strings.TrimSpace(r.FormValue("jabatan"))
		if _, err := s.auth.ChangeJabatan(r.Context(), userID, jabatan, canCreateManagement(user)); err != nil {
			message := "Jabatan tidak bisa diubah"
			status := http.StatusUnprocessableEntity
			if errors.Is(err, service.ErrValidation) {
				message = strings.TrimPrefix(err.Error(), "validation error: ")
			} else {
				log.Printf("change jabatan: %v", err)
				status = http.StatusInternalServerError
			}
			s.renderUserManagement(w, r, user, sessionValue, message, "", status)
			return
		}
		s.renderUserManagement(w, r, user, sessionValue, "", "Jabatan tersimpan.", http.StatusOK)
	case "tambah-jabatan":
		nama := strings.TrimSpace(r.FormValue("nama_jabatan"))
		if err := s.auth.CreateJabatan(r.Context(), s.project.Nama, nama, user.NamaLengkap); err != nil {
			message := "Jabatan tidak bisa ditambahkan"
			status := http.StatusUnprocessableEntity
			if errors.Is(err, service.ErrValidation) {
				message = strings.TrimPrefix(err.Error(), "validation error: ")
			} else {
				log.Printf("create jabatan: %v", err)
				status = http.StatusInternalServerError
			}
			s.renderUserManagementWith(w, r, user, sessionValue, message, "", nama, status)
			return
		}
		s.renderUserManagement(w, r, user, sessionValue, "",
			"Jabatan "+nama+" ditambahkan untuk project "+s.project.Nama+".", http.StatusOK)
	case "simpan-akses":
		if err := s.saveAccessMatrix(r, user); err != nil {
			message := "Akses menu tidak bisa disimpan"
			status := http.StatusUnprocessableEntity
			if errors.Is(err, service.ErrValidation) {
				message = strings.TrimPrefix(err.Error(), "validation error: ")
			} else {
				log.Printf("save jabatan access: %v", err)
				status = http.StatusInternalServerError
			}
			s.renderUserManagement(w, r, user, sessionValue, message, "", status)
			return
		}
		s.renderUserManagement(w, r, user, sessionValue, "", "Akses menu per jabatan tersimpan.", http.StatusOK)
	default:
		s.renderUserManagement(w, r, user, sessionValue, "Aksi tidak dikenal.", "", http.StatusUnprocessableEntity)
	}
}

// saveAccessMatrix writes one row per position holding the menus that were
// ticked. Every position is written, empty list included, so a save is a full
// replacement of the stored rules rather than a patch.
func (s *Server) saveAccessMatrix(r *http.Request, actor *model.User) error {
	valid := configurableMenuKeys()
	for _, jabatan := range s.jabatanOptions(r.Context()) {
		if strings.EqualFold(jabatan, model.JabatanManagement) {
			continue
		}
		menus := intersectMenus(r.Form["menu_"+jabatan], valid)
		// The HR menu always stays with the HR position, so a save that
		// forgets it cannot lock the page's own editors out.
		if strings.EqualFold(jabatan, "HR") {
			menus = addPosition(menus, "hr")
		}
		// Written against this project alone: the same position may open
		// different menus at another site.
		if err := s.auth.SaveJabatanAccess(r.Context(), s.project.Nama, jabatan, menus); err != nil {
			return err
		}
	}
	return nil
}

// configurableMenuKeys is the closed set of menus the matrix may grant. Only
// the settings screen is left out: it configures the app itself and stays with
// Management, so putting it on the table would only offer something the server
// refuses.
//
// The HR module is on the table like any other. A site may want somebody
// besides HR in there; what it may not do is take it away from HR, and that is
// held one row at a time rather than by removing the column.
func configurableMenuKeys() []string {
	keys := make([]string, 0, len(navItems))
	for _, item := range navItems {
		if _, restricted := menuAccess[item.Key]; !restricted {
			continue
		}
		if item.Key == projectSettingsKey {
			continue
		}
		keys = append(keys, item.Key)
	}
	return keys
}

// intersectMenus keeps only the menu keys that actually exist, dropping any
// value a tampered form might carry.
func intersectMenus(got, valid []string) []string {
	allowed := make(map[string]bool, len(valid))
	for _, key := range valid {
		allowed[key] = true
	}
	menus := make([]string, 0, len(got))
	for _, menu := range got {
		menu = strings.TrimSpace(menu)
		if allowed[menu] {
			menus = append(menus, menu)
		}
	}
	return menus
}

// accessMenuChoices lists every top-level menu the matrix may grant, in
// sidebar order. It reads the sidebar's own list, so a module added to the app
// shows up here on its own.
//
// The settings screen is not among them. It belongs to Management whatever the
// matrix says, so a column of checkboxes that changed nothing would only invite
// somebody to tick one and believe it.
func accessMenuChoices() []MenuChoice {
	choices := make([]MenuChoice, 0, len(navItems))
	for _, item := range navItems {
		if _, restricted := menuAccess[item.Key]; !restricted {
			continue
		}
		if item.Key == projectSettingsKey {
			continue
		}
		locked := false
		choices = append(choices, MenuChoice{
			Key:    item.Key,
			Label:  item.Label,
			Lede:   moduleLede(item),
			Locked: locked,
		})
	}
	return choices
}

func (s *Server) renderUserManagement(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, errMessage, success string, status int) {
	s.renderUserManagementWith(w, r, user, sessionValue, errMessage, success, "", status)
}

// renderUserManagementWith is the same page holding what was typed into the
// add-position form, so a refused name comes back rather than being lost.
func (s *Server) renderUserManagementWith(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, errMessage, success, jabatanBaru string, status int) {
	users, err := s.projects.AllUsers(r.Context())
	if err != nil {
		log.Printf("list users: %v", err)
		if errMessage == "" {
			errMessage = "Daftar karyawan gagal dimuat"
		}
	}
	projects, err := s.projects.List(r.Context())
	if err != nil {
		log.Printf("list projects: %v", err)
	}

	actorOptions := s.jabatanOptionsFor(r.Context(), user)
	members := make([]UserManagementMember, 0, len(users))
	for _, member := range membersOf(users, s.project.Nama, firstProjectName(projects)) {
		options := actorOptions
		if !positionListed(member.Jabatan, options) {
			// The row's current position has to be in the dropdown even when
			// the acting person may not assign it, so the table shows where
			// the person stands rather than a blank.
			options = append([]string{member.Jabatan}, options...)
		}
		members = append(members, UserManagementMember{ProjectMember: member, Options: options})
	}

	rules := s.accessRules(r.Context())
	menus := accessMenuChoices()
	positions := s.jabatanOptions(r.Context())
	rows := make([]JabatanAccessRow, 0, len(positions))
	for _, jabatan := range positions {
		if strings.EqualFold(jabatan, model.JabatanManagement) {
			continue
		}
		row := JabatanAccessRow{Jabatan: jabatan}
		for _, menu := range menus {
			// HR keeps the HR module whatever is ticked: this is the screen
			// that edits these rights, and a save that dropped it would lock
			// its own editors out. Every other cell of that column is open.
			locked := menu.Locked || (strings.EqualFold(jabatan, "HR") && menu.Key == "hr")
			choice := MenuChoice{
				Key:    menu.Key,
				Label:  menu.Label,
				Lede:   menu.Lede,
				Locked: locked,
				Aktif:  locked || positionListed(jabatan, rules[menu.Key]),
			}
			row.Menus = append(row.Menus, choice)
		}
		rows = append(rows, row)
	}

	s.render(w, "user_management", UserManagementPageData{
		ShellPageData:   s.shellData(user, sessionValue, "hr-user-management"),
		Members:         members,
		SemuaProyekMark: model.ProjectSemua,
		AccessMenus:     menus,
		AccessRows:      rows,
		ProjectNama:     s.project.Nama,
		JabatanBaru:     jabatanBaru,
		NamaJabatanMax:  service.JabatanNameMaxLength,
		Error:           errMessage,
		Success:         success,
	}, status)
}
