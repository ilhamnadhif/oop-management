package handler

// NavItem is one entry in the dashboard sidebar. Icon names the inline SVG the
// "icon" template renders.
type NavItem struct {
	Key   string
	Label string
	Path  string
	Icon  string
}

// navItems is the whole sidebar, in display order. Absensi stays first because
// every role needs it. When menus become per-jabatan, filter this one slice
// rather than editing each template.
var navItems = []NavItem{
	{Key: "absensi", Label: "Absensi", Path: "/dashboard", Icon: "clock"},
	{Key: "produksi", Label: "Produksi", Path: "/produksi", Icon: "chart"},
	{Key: "produksi-overview", Label: "Produksi Overview", Path: "/produksi/overview", Icon: "activity"},
	{Key: "unit-dt", Label: "Unit DT", Path: "/unit-dt", Icon: "truck"},
	{Key: "unit-a2b", Label: "Unit A2B", Path: "/unit-a2b", Icon: "cube"},
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
