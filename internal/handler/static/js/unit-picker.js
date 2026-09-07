(() => {
  // The machine filter on the export page: a list of ticks with a search box
  // over it, and a summary that says what is picked while the list is closed.
  //
  // This enhances markup that already works. Without it the <details> still
  // opens and closes, the ticks still post, and the report is still filtered -
  // what is lost is the search and the live summary. That is why the search box
  // ships hidden and is unhidden here: a search box that does not search is
  // worse than no search box.
  //
  // Written by hand for the same reason the combobox was: the CSP allows
  // same-origin assets only, and a multi-select library would have to be
  // vendored for one widget.
  const pickers = Array.from(document.querySelectorAll("[data-unit-picker]"));
  if (!pickers.length) return;

  const label = (picked) => {
    if (picked.length === 0) return "Semua unit";
    if (picked.length <= 2) return picked.join(", ");
    return `${picked.length} unit dipilih`;
  };

  for (const picker of pickers) {
    const summary = picker.querySelector("[data-unit-picker-summary]");
    const search = picker.querySelector("[data-unit-picker-search]");
    const empty = picker.querySelector("[data-unit-picker-empty]");
    const options = Array.from(picker.querySelectorAll("[data-unit-option]"));
    if (!options.length) continue;

    if (search) search.hidden = false;

    const retitle = () => {
      if (!summary) return;
      const picked = options
        .filter((option) => option.querySelector("input").checked)
        .map((option) => option.querySelector("input").value);
      summary.textContent = label(picked);
    };

    picker.addEventListener("change", retitle);

    if (search) {
      // The haystack is the id and the name together: people look a machine up
      // by whichever of the two they happen to know.
      const filter = () => {
        const needle = search.value.trim().toLowerCase();
        let shown = 0;
        for (const option of options) {
          const hay = (option.dataset.unitOption || "").toLowerCase();
          // A machine already ticked stays visible whatever is typed, or
          // narrowing the list would hide what the filter currently holds.
          const keep = needle === "" || hay.includes(needle) || option.querySelector("input").checked;
          option.hidden = !keep;
          if (keep) shown += 1;
        }
        if (empty) empty.hidden = shown > 0;
      };
      search.addEventListener("input", filter);
      // Typing Escape clears the search rather than closing the whole list,
      // which is the more common thing to want next.
      search.addEventListener("keydown", (event) => {
        if (event.key !== "Escape" || search.value === "") return;
        event.stopPropagation();
        event.preventDefault();
        search.value = "";
        filter();
      });
      picker.addEventListener("toggle", () => {
        if (picker.open) search.focus();
      });
    }

    retitle();
  }

  // A list left open sits over the buttons below it, so anything that says
  // "done here" closes it: a click outside, and Escape.
  //
  // A click inside is left alone. Ticking a machine is the whole point of
  // having it open, and closing on the first tick would make picking three
  // machines three trips.
  document.addEventListener("click", (event) => {
    for (const picker of pickers) {
      if (picker.open && !picker.contains(event.target)) picker.open = false;
    }
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    for (const picker of pickers) picker.open = false;
  });
})();
