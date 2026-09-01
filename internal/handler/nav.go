package handler

import (
	"strings"

	"opp-management/internal/model"
)

// NavItem is one entry in the dashboard sidebar. An item with children is a
// group heading: it has no page of its own, only the pages beneath it. Icon
// names the inline SVG the "icon" template renders.
type NavItem struct {
	Key   string
	Label string
	Path  string
	Icon  string
	// Lede is the sentence printed under the page title. It says what the page
	// is for in the words someone would use to ask for it.
	Lede     string
	Children []NavItem
}

// navItems is the whole sidebar, in display order. Absensi stays first and
// ungrouped because every role needs it. When menus become per-jabatan, filter
// this one slice rather than editing each template.
var navItems = []NavItem{
	{Key: "beranda", Label: "Dashboard", Path: "/dashboard", Icon: "home",
		Lede: "Ringkasan kehadiran Anda sendiri bulan ini."},
	{Key: "absensi", Label: "Absensi", Path: "/absensi", Icon: "calendar",
		Lede: "Catat kehadiran hari ini lengkap dengan lokasi dan foto."},
	{Key: "leave-request", Label: "Request Leave", Path: "/leave/request", Icon: "clock",
		Lede: "Ajukan cuti atau izin dan pantau proses persetujuannya."},
	// Project settings belongs to no project: it is where projects are set up,
	// and it is the one menu whose visibility a project cannot switch off. It
	// sits with the ungrouped pages rather than among the modules, because it
	// configures them rather than being one of them.
	{Key: "project-settings", Label: "Project", Path: "/project/settings", Icon: "pin",
		Lede: "Kelola project, menu yang aktif di masing-masing, dan penugasan penggunanya."},
	{Key: "hr", Label: "HR", Icon: "users", Children: []NavItem{
		{Key: "hr-overview", Label: "Overview", Path: "/hr/overview", Icon: "activity",
			Lede: "Ringkasan karyawan, kehadiran, dan pengajuan leave."},
		{Key: "hr-karyawan", Label: "Input Karyawan", Path: "/hr/karyawan", Icon: "users",
			Lede: "Daftarkan karyawan baru ke project ini. Akun dibuat dengan password awal."},
		{Key: "hr-user-management", Label: "User Management", Path: "/hr/user-management", Icon: "user",
			Lede: "Ubah jabatan karyawan dan atur menu yang bisa dibuka setiap jabatan."},
		{Key: "hr-approval-leave", Label: "Approval Leave", Path: "/hr/approval-leave", Icon: "check",
			Lede: "Tinjau dan putuskan pengajuan cuti atau izin karyawan."},
		{Key: "hr-export", Label: "Export Data", Path: "/hr/export", Icon: "save",
			Lede: "Unduh rekap absensi bulanan karyawan dalam Excel."},
	}},
	{Key: "produksi", Label: "Produksi", Icon: "chart", Children: []NavItem{
		{Key: "produksi-overview", Label: "Overview", Path: "/produksi/overview", Icon: "activity",
			Lede: "Ringkasan volume, ritase, dan unit yang beroperasi."},
		{Key: "produksi-input", Label: "Input Data", Path: "/produksi", Icon: "list",
			Lede: "Kelola dan catat data produksi harian dengan mudah dan akurat."},
		{Key: "produksi-plan", Label: "Input Plan", Path: "/produksi/plan", Icon: "clipboard-list",
			Lede: "Tetapkan volume rencana per lokasi, yang jadi pembanding capaian di overview."},
		{Key: "produksi-export", Label: "Export Data", Path: "/produksi/export", Icon: "save",
			Lede: "Unduh laporan produksi bertanda tangan dalam XLSX atau PDF."},
	}},
	{Key: "nota", Label: "Nota", Icon: "receipt", Children: []NavItem{
		{Key: "nota-overview", Label: "Overview", Path: "/nota/overview", Icon: "activity",
			Lede: "Ringkasan pengeluaran dan nota yang masih menunggu pembayaran."},
		{Key: "nota-input", Label: "Input Data", Path: "/nota", Icon: "list",
			Lede: "Catat nota belanja beserta rincian item dan bukti pengeluarannya."},
		{Key: "nota-rekonsiliasi", Label: "Rekonsiliasi", Path: "/nota/rekonsiliasi", Icon: "wallet",
			Lede: "Tandai reimburse yang sudah dibayar perusahaan beserta buktinya."},
		{Key: "nota-export", Label: "Export Data", Path: "/nota/export", Icon: "save",
			Lede: "Unduh laporan nota bertanda tangan dalam XLSX atau PDF."},
	}},
	{Key: "unit", Label: "Unit", Icon: "truck", Children: []NavItem{
		{Key: "unit-overview", Label: "Overview", Path: "/unit/overview", Icon: "activity",
			Lede: "Ringkasan isi daftar unit DT dan alat berat A2B."},
		{Key: "unit-dt", Label: "Unit DT", Path: "/unit-dt", Icon: "truck",
			Lede: "Daftarkan dump truck beserta ukuran bak dan drivernya."},
		{Key: "unit-export", Label: "Export Data", Path: "/unit/export", Icon: "save",
			Lede: "Unduh daftar unit DT dalam XLSX atau PDF."},
	}},
	// Alat berat has its own menu: hour meters and fuel are recorded per
	// machine, which a dump truck register has nothing to say about.
	{Key: "a2b", Label: "A2B", Icon: "clipboard-list", Children: []NavItem{
		{Key: "a2b-overview", Label: "Overview", Path: "/a2b/overview", Icon: "activity",
			Lede: "Ringkasan alat berat: jumlah, sebaran lokasi, dan mereknya."},
		{Key: "a2b-unit", Label: "Unit A2B", Path: "/unit-a2b", Icon: "cube",
			Lede: "Daftarkan alat berat beserta kapasitas dan konsumsi bahan bakarnya."},
		{Key: "a2b-hm", Label: "Input HM", Path: "/a2b/hm", Icon: "clock",
			Lede: "Catat pembacaan hour meter setiap alat berat."},
		// The tank has two sides: what a vendor delivers into it, and what is
		// pumped out of it into a machine.
		{Key: "a2b-fuel-masuk", Label: "Fuel Masuk", Path: "/a2b/fuel-masuk", Icon: "fuel",
			Lede: "Catat kiriman fuel dari vendor lengkap dengan foto bukti bongkar."},
		{Key: "a2b-fuel-keluar", Label: "Fuel Keluar", Path: "/a2b/fuel-keluar", Icon: "fuel",
			Lede: "Catat pengisian bahan bakar tiap alat berat lewat pembacaan flow meter."},
		{Key: "a2b-export", Label: "Export Data", Path: "/a2b/export", Icon: "save",
			Lede: "Unduh daftar alat berat dalam XLSX atau PDF."},
	}},
}

// JabatanManagement sees every menu. It is the one position defined by what it
// may reach rather than by what it does.
const JabatanManagement = model.JabatanManagement

// menuAccess lists the positions that may open each top-level menu. A menu
// missing from this map is open to everyone, which is how Dashboard and Absensi
// stay reachable: attendance is the one thing every employee has to do.
//
// These are the defaults. JabatanAccess rows from the master spreadsheet
// replace them for the positions they name, which is what the User Management
// screen edits. defaultMenuRules is what a position gets when nothing stored
// overrides it.
var menuAccess = map[string][]string{
	"hr":       {"HR"},
	"produksi": {"Surveyor", "Produksi", "SPV"},
	"unit":     {"Surveyor", "Produksi", "SPV", "Logistik"},
	// Alat berat is the same fleet seen from another angle, so it is open to
	// the same positions as the unit register.
	"a2b":  {"Surveyor", "Produksi", "SPV", "Logistik"},
	"nota": {"HR"},
	// Nobody but Management, which CanAccess lets through before this map is
	// consulted. An empty list is how a menu is closed to every other position.
	"project-settings": {},
}

// menuRules is the effective per-menu position list, defaults overlaid with
// whatever the User Management screen has stored.
type menuRules map[string][]string

// defaultMenuRules returns a copy of the built-in menuAccess, so callers can
// never mutate the shared default.
func defaultMenuRules() menuRules {
	rules := make(menuRules, len(menuAccess))
	for menu, positions := range menuAccess {
		rules[menu] = append([]string(nil), positions...)
	}
	return rules
}

// effectiveMenuRules layers stored jabatan access over the defaults. A position
// with a stored row gets exactly the menus that row lists, replacing its
// defaults; a position without one keeps the defaults.
//
// Two menus are exempt from the stored rules. HR always keeps the hr menu, or
// the screen that edits these very rights could lock itself out, and
// project-settings stays Management-only, for the same reason it is locked in
// its own editor.
func effectiveMenuRules(stored []model.JabatanAccess) menuRules {
	rules := defaultMenuRules()
	for _, access := range stored {
		jabatan := strings.TrimSpace(access.Jabatan)
		if strings.EqualFold(jabatan, JabatanManagement) {
			continue
		}
		allowed := make(map[string]bool, len(access.MenuAktif))
		for _, menu := range access.MenuAktif {
			allowed[strings.TrimSpace(menu)] = true
		}
		for menu := range rules {
			rules[menu] = removePosition(rules[menu], jabatan)
			if allowed[menu] {
				rules[menu] = addPosition(rules[menu], jabatan)
			}
		}
	}
	rules["hr"] = addPosition(rules["hr"], "HR")
	rules[projectSettingsKey] = nil
	return rules
}

// removePosition drops one position from a list, case-insensitively.
func removePosition(positions []string, jabatan string) []string {
	result := make([]string, 0, len(positions))
	for _, position := range positions {
		if !strings.EqualFold(strings.TrimSpace(position), strings.TrimSpace(jabatan)) {
			result = append(result, position)
		}
	}
	return result
}

// addPosition appends one position when it is not already listed.
func addPosition(positions []string, jabatan string) []string {
	if positionListed(jabatan, positions) {
		return positions
	}
	return append(positions, jabatan)
}

// menuKeyFor reports which top-level menu a page belongs to, since permission
// is granted over a menu rather than over each page under it.
func menuKeyFor(key string) string {
	for _, top := range navItems {
		if top.Key == key {
			return top.Key
		}
		for _, child := range top.Children {
			if child.Key == key {
				return top.Key
			}
		}
	}
	return key
}

// projectSettingsKey is the one page that belongs to no project. It is the
// screen projects are configured from, so gating it behind a project's own menu
// list would let a project switch off the way back to its settings.
const projectSettingsKey = "project-settings"

// CanAccess reports whether a position may open a page, ignoring the project.
// It is the jabatan half of the rule and exists on its own because the sidebar
// and the guards both need it; almost every caller wants CanReach instead.
//
// An unknown page is refused rather than allowed: a route added without a rule
// should be unreachable, not open to everyone.
func CanAccess(rules menuRules, jabatan, key string) bool {
	if strings.EqualFold(strings.TrimSpace(jabatan), JabatanManagement) {
		return true
	}
	menu := menuKeyFor(key)
	allowed, restricted := rules[menu]
	if !restricted {
		// Only pages that exist in the menu are open by default.
		if _, _, found := navItemByKey(key); !found {
			return false
		}
		return true
	}
	return positionListed(jabatan, allowed)
}

// CanReach is the whole rule: a page opens when the position may see it and the
// project it is being opened in actually runs that module.
//
// The project half binds everyone, Management included. A menu switched off has
// no rows behind it, so showing it would only lead to pages that are empty by
// construction.
func CanReach(rules menuRules, jabatan string, project model.Project, key string) bool {
	if !CanAccess(rules, jabatan, key) {
		return false
	}
	return projectRuns(project, key)
}

// projectRuns reports whether a project has the module a page belongs to.
//
// Only modules are gated. Dashboard, Absensi and Request Leave are open to
// every position and belong to no module, so a project that lists its modules
// does not thereby switch off the pages every employee has to reach. The
// settings screen is exempt for a different reason: it is how the list is
// edited, and a project that switched it off could never switch anything on.
func projectRuns(project model.Project, key string) bool {
	menu := menuKeyFor(key)
	if menu == projectSettingsKey {
		return true
	}
	if _, isModule := menuAccess[menu]; !isModule {
		return true
	}
	return project.HasMenu(menu)
}

func positionListed(jabatan string, allowed []string) bool {
	for _, position := range allowed {
		if strings.EqualFold(strings.TrimSpace(jabatan), position) {
			return true
		}
	}
	return false
}

// navItemsFor returns the menu a position may see. A group whose pages are all
// out of reach is dropped entirely rather than shown as a heading that opens
// onto nothing.
func navItemsFor(rules menuRules, jabatan string, project model.Project) []NavItem {
	visible := make([]NavItem, 0, len(navItems))
	for _, item := range navItems {
		if len(item.Children) == 0 {
			if CanReach(rules, jabatan, project, item.Key) {
				visible = append(visible, item)
			}
			continue
		}
		children := make([]NavItem, 0, len(item.Children))
		for _, child := range item.Children {
			if CanReach(rules, jabatan, project, child.Key) {
				children = append(children, child)
			}
		}
		if len(children) == 0 {
			continue
		}
		group := item
		group.Children = children
		visible = append(visible, group)
	}
	return visible
}

// navItemByKey finds a page anywhere in the tree and reports the group holding
// it, which is what the breadcrumb needs to name the section.
func navItemByKey(key string) (item NavItem, parent NavItem, found bool) {
	for _, top := range navItems {
		if top.Key == key {
			return top, NavItem{}, true
		}
		for _, child := range top.Children {
			if child.Key == key {
				return child, top, true
			}
		}
	}
	return NavItem{}, NavItem{}, false
}

// IsActiveGroup reports whether the page currently open lives inside this
// group, which is how a group shows where you are.
func (n NavItem) IsActiveGroup(activeKey string) bool {
	for _, child := range n.Children {
		if child.Key == activeKey {
			return true
		}
	}
	return false
}
