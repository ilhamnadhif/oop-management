// The sidebar keeps where it was scrolled to. Every page here is a fresh
// server render, so following a link rebuilds the menu from the top and throws
// away the place somebody had scrolled to - which on a long menu means hunting
// for the same spot after every click.
//
// The position is kept per tab rather than per browser: two tabs open on
// different parts of the app should not drag each other's menu about. It is
// forgotten when the tab closes, so a fresh visit starts at the top.
(() => {
  const sidebar = document.querySelector("#appSidebar");
  if (!sidebar) return;

  // Which element scrolls depends on the width: the nav takes the overflow on
  // a desktop, the whole sidebar on a narrow screen. Both are remembered, and
  // whichever is not scrolling simply stays at nought.
  const panes = [
    { key: "sidebar-scroll", node: sidebar },
    { key: "sidebar-nav-scroll", node: sidebar.querySelector(".sidebar-nav") },
  ].filter((pane) => pane.node);

  // Storage is unavailable in some privacy modes, and a menu that will not
  // scroll is a worse failure than a menu that forgets.
  const read = (key) => {
    try {
      return Number.parseInt(window.sessionStorage.getItem(key) || "", 10);
    } catch (error) {
      return Number.NaN;
    }
  };
  const write = (key, value) => {
    try {
      window.sessionStorage.setItem(key, String(value));
    } catch (error) {
      /* nothing to do: the position is a convenience, not the page */
    }
  };

  const restore = () => {
    for (const pane of panes) {
      const saved = read(pane.key);
      if (Number.isFinite(saved) && saved > 0) pane.node.scrollTop = saved;
    }
  };
  const remember = () => {
    for (const pane of panes) write(pane.key, pane.node.scrollTop);
  };

  restore();
  // Coming back through the history cache restores the whole page, including
  // the scroll, so this only has to cover the case where it does not.
  window.addEventListener("pageshow", restore);

  let queued = false;
  for (const pane of panes) {
    pane.node.addEventListener(
      "scroll",
      () => {
        if (queued) return;
        queued = true;
        window.requestAnimationFrame(() => {
          queued = false;
          remember();
        });
      },
      { passive: true }
    );
  }
  // A click may leave before the next frame runs, so the position is written
  // once more on the way out.
  window.addEventListener("pagehide", remember);
})();

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
