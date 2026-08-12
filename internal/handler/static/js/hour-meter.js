(() => {
  // Three fields follow from what is already on the form: the unit name that
  // belongs to the chosen id, the opening reading that machine finished on last
  // time, and the minutes between the two readings. The server derives the name
  // and the total for itself, so this script switched off costs typing, not
  // correctness.
  const form = document.querySelector("[data-hm-form]");
  if (!form) return;

  const unit = form.querySelector("[data-hm-unit]");
  const namaUnit = form.querySelector("[data-hm-nama-unit]");
  const awal = form.querySelector("[data-hm-awal-field]");
  const akhir = form.querySelector("[data-hm-akhir-field]");
  const total = form.querySelector("[data-hm-total]");

  // An Indonesian keyboard produces a decimal comma. The server accepts either,
  // and so does this preview.
  const decimal = (value) => Number.parseFloat(String(value || "").replace(",", "."));

  const syncTotal = () => {
    if (!awal || !akhir || !total) return;
    const from = decimal(awal.value);
    const to = decimal(akhir.value);
    if (!Number.isFinite(from) || !Number.isFinite(to) || to < from) {
      total.value = "";
      return;
    }
    total.value = String(Math.round((to - from) * 100) / 100);
  };

  // Only fills an empty box: a reading already typed is the operator's, and a
  // machine with no history leaves the field alone to be read off the alat.
  const applyUnit = (prefillReading) => {
    if (!unit) return;
    const selected = unit.options[unit.selectedIndex];
    if (namaUnit) namaUnit.value = (selected && selected.dataset.namaUnit) || "";
    if (prefillReading && awal && selected && selected.dataset.hmAwal !== undefined) {
      awal.value = selected.dataset.hmAwal;
      syncTotal();
    }
  };

  if (unit) unit.addEventListener("change", () => applyUnit(true));
  if (awal) awal.addEventListener("input", syncTotal);
  if (akhir) akhir.addEventListener("input", syncTotal);

  applyUnit(awal ? awal.value === "" : false);
  syncTotal();

  // Standby and breakdown are the same widget twice: a list of reasons with
  // minutes against them, a running total, and rows that can be added or
  // dropped. The server totals both for itself, so this is display only.
  const durationList = (name, satuan) => {
    const list = form.querySelector(`[data-${name}-list]`);
    const totalCell = form.querySelector(`[data-${name}-total]`);
    if (!list) return;

    const rows = () => list.querySelectorAll(`[data-${name}-row]`);

    const syncListTotal = () => {
      let minutes = 0;
      rows().forEach((row) => {
        const field = row.querySelector(`[name='${name}_menit']`);
        const value = decimal(field && field.value);
        if (Number.isFinite(value) && value > 0) minutes += value;
      });
      if (totalCell) {
        totalCell.textContent = `${Math.round(minutes * 100) / 100} ${satuan}`;
      }
    };

    // The rows are numbered, so the numbers have to follow a row being removed.
    const renumber = () => {
      rows().forEach((row, index) => {
        const number = row.querySelector(`[data-${name}-number]`);
        if (number) number.textContent = String(index + 1);
      });
    };

    const blank = (row) => {
      row.querySelectorAll("input").forEach((input) => { input.value = ""; });
      row.querySelectorAll("select").forEach((select) => { select.selectedIndex = 0; });
    };

    const addRow = () => {
      const first = list.querySelector(`[data-${name}-row]`);
      if (!first) return;
      const row = first.cloneNode(true);
      blank(row);
      list.appendChild(row);
      renumber();
      syncListTotal();
      const variable = row.querySelector(`[name='${name}_variable']`);
      if (variable) variable.focus();
    };

    // The last row is emptied rather than removed: a list with no rows leaves
    // nothing to clone the next one from.
    const removeRow = (row) => {
      if (rows().length > 1) row.remove();
      else blank(row);
      renumber();
      syncListTotal();
    };

    form.addEventListener("click", (event) => {
      const add = event.target.closest(`[data-add-${name}]`);
      if (add) {
        event.preventDefault();
        addRow();
        return;
      }
      const remove = event.target.closest(`[data-remove-${name}]`);
      if (remove) {
        event.preventDefault();
        const row = remove.closest(`[data-${name}-row]`);
        if (row) removeRow(row);
      }
    });

    list.addEventListener("input", (event) => {
      if (event.target.name === `${name}_menit`) syncListTotal();
    });

    syncListTotal();

    return {
      total: () => {
        let minutes = 0;
        rows().forEach((row) => {
          const field = row.querySelector(`[name='${name}_menit']`);
          const value = decimal(field && field.value);
          if (Number.isFinite(value) && value > 0) minutes += value;
        });
        return Math.round(minutes * 100) / 100;
      },
      clear: () => {
        rows().forEach((row, index) => {
          if (index > 0) row.remove();
          else blank(row);
        });
        renumber();
        syncListTotal();
      },
      onChange: (handler) => {
        list.addEventListener("input", handler);
        list.addEventListener("change", handler);
        form.addEventListener("click", (event) => {
          if (event.target.closest(`[data-add-${name}]`) || event.target.closest(`[data-remove-${name}]`)) {
            handler();
          }
        });
      },
    };
  };

  const standby = durationList("standby", "menit");
  const breakdown = durationList("breakdown", "menit");

  // The shift is either worked or accounted for. Once the hour meter covers the
  // whole shift there is nothing left to explain, so both sections go away and
  // their rows are emptied - the server refuses a reading that carries standby
  // it has no room for. Short of that, the remainder is spelled out and counted
  // down as it is filled in.
  const workMinutes = Number.parseInt(form.dataset.workMinutes || "", 10);
  const sections = form.querySelector("[data-idle-sections]");
  const shortNote = form.querySelector("[data-idle-short]");
  const filledNote = form.querySelector("[data-idle-filled]");
  const remainingCell = form.querySelector("[data-idle-remaining]");
  const done = form.querySelector("[data-idle-done]");

  const syncIdle = () => {
    if (!Number.isFinite(workMinutes) || !sections) return;
    const hours = decimal(total ? total.value : "");
    const idle = Number.isFinite(hours)
      ? Math.max(0, Math.round((workMinutes - hours * 60) * 100) / 100)
      : workMinutes;

    sections.hidden = idle === 0;
    if (done) done.hidden = idle !== 0;
    if (idle === 0) {
      if (standby) standby.clear();
      if (breakdown) breakdown.clear();
      return;
    }
    // The sentence counts down as the two sections are filled, and gives way to
    // the settled one the moment they add up.
    const filled = (standby ? standby.total() : 0) + (breakdown ? breakdown.total() : 0);
    const short = Math.round((idle - filled) * 100) / 100;
    if (remainingCell) remainingCell.textContent = String(short > 0 ? short : idle);
    if (shortNote) {
      shortNote.hidden = short === 0;
      // Overshooting is as wrong as falling short, and reads the same way.
      shortNote.classList.toggle("short", short < 0);
    }
    if (filledNote) filledNote.hidden = short !== 0;
  };

  // The three figures the shift is read by. Worked out here so they move as the
  // form is filled in; the server computes them again before storing.
  const figures = form.querySelector("[data-figure-grid]");
  const targets = {
    pa: Number.parseFloat(form.dataset.paTarget || ""),
    bd: Number.parseFloat(form.dataset.bdTarget || ""),
    ua: Number.parseFloat(form.dataset.uaTarget || ""),
  };

  const syncFigures = () => {
    if (!figures || !Number.isFinite(workMinutes) || workMinutes <= 0) return;
    const hours = decimal(total ? total.value : "");
    const worked = Number.isFinite(hours) ? hours * 60 : NaN;
    const bd = breakdown ? breakdown.total() : 0;
    const available = Math.max(0, workMinutes - bd);

    const round = (value) => Math.round(value * 100) / 100;
    const values = {
      pa: round((available / workMinutes) * 100),
      bd: round((bd / workMinutes) * 100),
      ua: available > 0 && Number.isFinite(worked) ? round(Math.min(100, (worked / available) * 100)) : 0,
    };
    const met = {
      pa: values.pa >= targets.pa,
      bd: values.bd <= targets.bd,
      ua: values.ua >= targets.ua,
    };

    figures.querySelectorAll("[data-figure]").forEach((tile) => {
      const name = tile.dataset.figure;
      const cell = tile.querySelector("[data-figure-value]");
      // Nothing typed yet: a figure invented from an empty form would read as a
      // verdict on a shift nobody has entered.
      const known = name !== "ua" || Number.isFinite(worked);
      if (cell) cell.textContent = known ? `${values[name]}%` : "—";
      tile.classList.toggle("good", known && met[name]);
      tile.classList.toggle("short", known && !met[name]);
    });
  };

  if (breakdown) breakdown.onChange(syncFigures);
  [awal, akhir].forEach((field) => {
    if (field) field.addEventListener("input", syncFigures);
  });
  if (unit) unit.addEventListener("change", syncFigures);
  syncFigures();

  if (standby) standby.onChange(syncIdle);
  if (breakdown) breakdown.onChange(syncIdle);
  [awal, akhir].forEach((field) => {
    if (field) field.addEventListener("input", syncIdle);
  });
  if (unit) unit.addEventListener("change", syncIdle);
  syncIdle();
})();
