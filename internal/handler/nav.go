package handler

// NavItem is one entry in the dashboard sidebar.
type NavItem struct {
	Key   string
	Label string
	Path  string
}

// navItems is the whole sidebar, in display order. Absensi stays first because
// every role needs it. When menus become per-jabatan, filter this one slice
// rather than editing each template.
var navItems = []NavItem{
	{Key: "absensi", Label: "Absensi", Path: "/dashboard"},
	{Key: "produksi", Label: "Produksi", Path: "/produksi"},
	{Key: "unit-dt", Label: "Unit DT", Path: "/unit-dt"},
}

// navItemsFor returns the menu a user may see. Every role currently sees the
// same list; this is the seam where per-jabatan filtering will go.
func navItemsFor(jabatan string) []NavItem {
	_ = jabatan
	return navItems
}

func navItemByKey(key string) (NavItem, bool) {
	for _, item := range navItems {
		if item.Key == key {
			return item, true
		}
	}
	return NavItem{}, false
}
