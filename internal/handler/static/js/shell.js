(() => {
  const sidebar = document.querySelector("#appSidebar");
  const toggle = document.querySelector("#menuToggle");
  const scrim = document.querySelector("#sidebarScrim");
  if (!sidebar || !toggle) return;

  const setOpen = (open) => {
    document.body.classList.toggle("sidebar-open", open);
    toggle.setAttribute("aria-expanded", String(open));
    toggle.setAttribute("aria-label", open ? "Tutup menu" : "Buka menu");
    if (scrim) scrim.hidden = !open;
  };

  toggle.addEventListener("click", () => {
    setOpen(!document.body.classList.contains("sidebar-open"));
  });
  if (scrim) scrim.addEventListener("click", () => setOpen(false));
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") setOpen(false);
  });

  // The drawer only exists at narrow widths. Resizing back to desktop while it
  // is open would otherwise leave the scrim covering the page.
  const desktop = window.matchMedia("(min-width: 901px)");
  const closeOnDesktop = (event) => {
    if (event.matches) setOpen(false);
  };
  if (desktop.addEventListener) {
    desktop.addEventListener("change", closeOnDesktop);
  }

  setOpen(false);
})();
