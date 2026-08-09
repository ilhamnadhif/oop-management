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

(() => {
  // Groups start collapsed; only the one holding the current page is open, and
  // the server renders it that way. This remembers the ones the user expanded on
  // top of that, because navigation reloads the page and a group opened to look
  // around would otherwise snap shut on the next click.
  //
  // The active group is left alone: closing it would hide where you are.
  const groups = Array.from(document.querySelectorAll(".sidebar-group[data-group]"));
  if (!groups.length) return;

  const storage = (() => {
    try {
      const probe = "__opp__";
      window.localStorage.setItem(probe, probe);
      window.localStorage.removeItem(probe);
      return window.localStorage;
    } catch (error) {
      return null;
    }
  })();
  if (!storage) return;

  // Expanded groups are stored, not collapsed ones, so the default with nothing
  // saved is collapsed.
  const KEY = "opp.sidebar.expanded";
  const load = () => {
    try {
      return new Set(JSON.parse(storage.getItem(KEY)) || []);
    } catch (error) {
      return new Set();
    }
  };

  const expanded = load();
  groups.forEach((group) => {
    const name = group.dataset.group;
    if (group.classList.contains("active")) return;
    group.open = expanded.has(name);

    group.addEventListener("toggle", () => {
      const next = load();
      if (group.open) next.add(name);
      else next.delete(name);
      storage.setItem(KEY, JSON.stringify(Array.from(next)));
    });
  });
})();

(() => {
  // A <details> menu stays open until its own summary is clicked again, which
  // leaves the account panel hanging over the page while you work elsewhere.
  const menus = Array.from(document.querySelectorAll("details.account-menu"));
  if (!menus.length) return;

  const closeAll = (except) => {
    menus.forEach((menu) => {
      if (menu !== except) menu.open = false;
    });
  };

  document.addEventListener("click", (event) => {
    const openMenu = menus.find((menu) => menu.open);
    if (!openMenu) return;
    // A click inside the menu is either the summary toggling it or the logout
    // button submitting; neither should be intercepted here.
    if (openMenu.contains(event.target)) {
      closeAll(openMenu);
      return;
    }
    closeAll(null);
  });

  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    const openMenu = menus.find((menu) => menu.open);
    if (!openMenu) return;
    openMenu.open = false;
    const summary = openMenu.querySelector("summary");
    if (summary) summary.focus();
  });
})();
