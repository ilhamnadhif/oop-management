(() => {
  // Upgrades every `<input list=...>` into a real creatable combobox: a list
  // that opens on click, filters as you type, and offers to create whatever is
  // not in it yet.
  //
  // It enhances the native markup rather than replacing it, so the page still
  // works if this script fails to load - the browser's own datalist remains.
  // Written by hand because the CSP allows same-origin assets only, and a
  // combobox library would have to be vendored for one widget.

  const fields = Array.from(document.querySelectorAll("input[list]"));
  if (!fields.length) return;

  let openCombobox = null;

  const closeOpen = (except) => {
    if (openCombobox && openCombobox !== except) openCombobox.close();
  };

  document.addEventListener("click", (event) => {
    if (openCombobox && !openCombobox.root.contains(event.target)) openCombobox.close();
  });

  fields.forEach((input, index) => {
    const datalist = document.getElementById(input.getAttribute("list"));
    if (!datalist) return;

    const values = Array.from(datalist.options).map((option) => option.value).filter(Boolean);
    // Dropping the `list` attribute is enough to stop the browser drawing its
    // own suggestions on top of ours. The element itself stays: other scripts
    // read it - produksi.js pulls each unit's dimensions from its options - and
    // removing it would leave them with nothing to look up.
    input.removeAttribute("list");

    const root = document.createElement("div");
    root.className = "combobox";
    input.parentNode.insertBefore(root, input);
    root.appendChild(input);
    input.classList.add("combobox-input");
    input.setAttribute("autocomplete", "off");
    input.setAttribute("role", "combobox");
    input.setAttribute("aria-expanded", "false");
    input.setAttribute("aria-autocomplete", "list");

    const toggle = document.createElement("button");
    toggle.type = "button";
    toggle.className = "combobox-toggle";
    toggle.tabIndex = -1;
    toggle.setAttribute("aria-label", "Buka pilihan");
    toggle.innerHTML =
      '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6"></path></svg>';
    root.appendChild(toggle);

    const listbox = document.createElement("ul");
    listbox.className = "combobox-list";
    listbox.id = `combobox-list-${index}`;
    listbox.setAttribute("role", "listbox");
    listbox.hidden = true;
    root.appendChild(listbox);
    input.setAttribute("aria-controls", listbox.id);

    // Some fields must stay closed sets. Nopol is one: the row it produces
    // carries the unit's dimensions, so a plate that is not in the register has
    // nothing to attach and the server refuses it anyway. Offering to create one
    // would promise something the save cannot honour.
    const allowCreate = !input.hasAttribute("data-no-create");
    const emptyText = input.getAttribute("data-empty-text") || "Tidak ada pilihan";

    let items = [];
    let activeIndex = -1;
    // Set while the value is being written programmatically. The input event we
    // dispatch for other scripts would otherwise land on our own handler and
    // reopen the list we just closed.
    let settingValue = false;

    const isOpen = () => !listbox.hidden;

    const setActive = (next) => {
      activeIndex = next;
      items.forEach((item, i) => {
        item.element.classList.toggle("active", i === activeIndex);
        item.element.setAttribute("aria-selected", i === activeIndex ? "true" : "false");
      });
      if (activeIndex >= 0) {
        input.setAttribute("aria-activedescendant", items[activeIndex].element.id);
        items[activeIndex].element.scrollIntoView({ block: "nearest" });
      } else {
        input.removeAttribute("aria-activedescendant");
      }
    };

    const choose = (value) => {
      settingValue = true;
      input.value = value;
      close();
      input.dispatchEvent(new Event("input", { bubbles: true }));
      input.dispatchEvent(new Event("change", { bubbles: true }));
      settingValue = false;
    };

    const render = () => {
      const typed = input.value.trim();
      const needle = typed.toLowerCase();
      const matches = needle ? values.filter((v) => v.toLowerCase().includes(needle)) : values.slice();
      const exact = values.some((v) => v.toLowerCase() === needle);

      listbox.textContent = "";
      items = [];

      matches.forEach((value, i) => {
        const element = document.createElement("li");
        element.id = `${listbox.id}-option-${i}`;
        element.className = "combobox-option";
        element.setAttribute("role", "option");
        element.textContent = value;
        element.addEventListener("mousedown", (event) => {
          // mousedown, not click: blur would close the list first.
          event.preventDefault();
          choose(value);
        });
        listbox.appendChild(element);
        items.push({ element, value });
      });

      if (allowCreate && typed && !exact) {
        const element = document.createElement("li");
        element.id = `${listbox.id}-option-create`;
        element.className = "combobox-option combobox-create";
        element.setAttribute("role", "option");
        element.textContent = `Buat "${typed}"`;
        element.addEventListener("mousedown", (event) => {
          event.preventDefault();
          choose(typed);
        });
        listbox.appendChild(element);
        items.push({ element, value: typed });
      }

      if (!items.length) {
        const empty = document.createElement("li");
        empty.className = "combobox-empty";
        empty.textContent = emptyText;
        listbox.appendChild(empty);
      }
      setActive(items.length ? 0 : -1);
    };

    const open = () => {
      closeOpen(api);
      render();
      listbox.hidden = false;
      input.setAttribute("aria-expanded", "true");
      root.dataset.open = "true";
      openCombobox = api;
    };

    function close() {
      listbox.hidden = true;
      input.setAttribute("aria-expanded", "false");
      input.removeAttribute("aria-activedescendant");
      delete root.dataset.open;
      activeIndex = -1;
      if (openCombobox === api) openCombobox = null;
    }

    const api = { root, close };

    input.addEventListener("focus", open);
    // Focus alone does not fire again once the field already has it, so a click
    // has to reopen the list after Escape or a selection.
    input.addEventListener("click", () => {
      if (!isOpen()) open();
    });
    input.addEventListener("input", () => {
      if (settingValue) return;
      if (isOpen()) render();
      else open();
    });
    toggle.addEventListener("mousedown", (event) => {
      event.preventDefault();
      if (isOpen()) {
        close();
      } else {
        input.focus();
        open();
      }
    });

    input.addEventListener("keydown", (event) => {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        if (!isOpen()) {
          open();
          return;
        }
        if (!items.length) return;
        const step = event.key === "ArrowDown" ? 1 : -1;
        setActive((activeIndex + step + items.length) % items.length);
        return;
      }
      if (event.key === "Enter") {
        // Only swallow Enter while a choice is highlighted; otherwise the key
        // must still submit the form.
        if (isOpen() && activeIndex >= 0) {
          event.preventDefault();
          choose(items[activeIndex].value);
        }
        return;
      }
      if (event.key === "Escape") {
        if (isOpen()) {
          event.preventDefault();
          close();
        }
        return;
      }
      if (event.key === "Tab") close();
    });
  });
})();
