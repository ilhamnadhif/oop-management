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
  // Groups start collapsed. Only the group holding the current page arrives
  // open, and the server renders it that way, so the menu is right before any
  // of this runs.
  //
  // Opening one closes the rest: the sidebar is a place to find the next page,
  // not a list to keep unfolded, and a stack of open groups pushes the lower
  // ones off a phone screen. Nothing is remembered between page loads, so a
  // fresh sign-in always starts tidy.
  const groups = Array.from(document.querySelectorAll(".sidebar-group[data-group]"));
  if (!groups.length) return;

  const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
  const DURATION = 220;
  const running = new WeakMap();

  const sublist = (group) => group.querySelector(".sidebar-sublist");

  const settle = (list) => {
    list.style.overflow = "";
    list.style.height = "";
    list.style.opacity = "";
  };

  const setOpen = (group, open) => {
    const list = sublist(group);
    const current = running.get(group);
    if (current) current.cancel();

    if (!list || reduceMotion.matches || typeof list.animate !== "function") {
      group.open = open;
      return;
    }
    if (group.open === open) return;

    // A closed <details> renders nothing, so the element has to be open for the
    // whole animation and is only closed once the collapse has finished.
    group.open = true;
    const height = list.scrollHeight;
    list.style.overflow = "hidden";

    const animation = list.animate(
      [
        { height: (open ? 0 : height) + "px", opacity: open ? 0 : 1 },
        { height: (open ? height : 0) + "px", opacity: open ? 1 : 0 },
      ],
      { duration: DURATION, easing: "cubic-bezier(0.4, 0, 0.2, 1)" }
    );
    running.set(group, animation);
    animation.addEventListener("finish", () => {
      running.delete(group);
      group.open = open;
      settle(list);
    });
    animation.addEventListener("cancel", () => settle(list));
  };

  groups.forEach((group) => {
    const summary = group.querySelector("summary");
    if (!summary) return;
    summary.addEventListener("click", (event) => {
      // The browser would toggle instantly; the animation needs to own it.
      event.preventDefault();
      const opening = !group.open;
      if (opening) groups.forEach((other) => other !== group && setOpen(other, false));
      setOpen(group, opening);
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
