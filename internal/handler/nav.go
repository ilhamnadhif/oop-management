package handler

// NavItem is one entry in the dashboard sidebar. An item with children is a
// group heading: it has no page of its own, only the pages beneath it. Icon
// names the inline SVG the "icon" template renders.
type NavItem struct {
	Key      string
	Label    string
	Path     string
	Icon     string
	Children []NavItem
}

// navItems is the whole sidebar, in display order. Absensi stays first and
// ungrouped because every role needs it. When menus become per-jabatan, filter
// this one slice rather than editing each template.
var navItems = []NavItem{
	{Key: "absensi", Label: "Absensi", Path: "/dashboard", Icon: "clock"},
	{Key: "produksi", Label: "Produksi", Icon: "chart", Children: []NavItem{
		{Key: "produksi-overview", Label: "Overview", Path: "/produksi/overview", Icon: "activity"},
		{Key: "produksi-input", Label: "Input Data", Path: "/produksi", Icon: "list"},
		{Key: "produksi-export", Label: "Export Data", Path: "/produksi/export", Icon: "save"},
	}},
	{Key: "nota", Label: "Nota", Icon: "receipt", Children: []NavItem{
		{Key: "nota-overview", Label: "Overview", Path: "/nota/overview", Icon: "activity"},
		{Key: "nota-input", Label: "Input Data", Path: "/nota", Icon: "list"},
		{Key: "nota-rekonsiliasi", Label: "Rekonsiliasi", Path: "/nota/rekonsiliasi", Icon: "wallet"},
		{Key: "nota-export", Label: "Export Data", Path: "/nota/export", Icon: "save"},
	}},
	{Key: "unit", Label: "Unit", Icon: "truck", Children: []NavItem{
		{Key: "unit-dt", Label: "Unit DT", Path: "/unit-dt", Icon: "truck"},
		{Key: "unit-a2b", Label: "Unit A2B", Path: "/unit-a2b", Icon: "cube"},
		{Key: "unit-export", Label: "Export Data", Path: "/unit/export", Icon: "save"},
	}},
}

// navItemsFor returns the menu a user may see. Every role currently sees the
// same list; this is the seam where per-jabatan filtering will go.
func navItemsFor(jabatan string) []NavItem {
	_ = jabatan
	return navItems
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
