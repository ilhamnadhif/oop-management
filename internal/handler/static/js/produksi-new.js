// Produksi New keeps its figures current while the page is open: it asks the
// server again every interval and writes the answer into the cards and the
// ritase summary. The server caches the scanner, so asking costs nothing when
// nothing has changed.
(() => {
  const root = document.querySelector("[data-widya]");
  if (!root) return;

  const interval = Number(root.dataset.refreshMs) || 30000;
  const errorBox = root.querySelector("[data-widya-error]");
  const updated = root.querySelector("[data-widya-updated]");
  const ritase = root.querySelector("[data-ritase]");
  const rekap = root.querySelector("[data-rekap]");
  const detail = root.querySelector("[data-detail]");
  const dayInput = root.querySelector("[data-detail-day]");
  const rit = root.querySelector("[data-rit]");
  const ritDayInput = root.querySelector("[data-rit-day]");
  const rekapFromInput = root.querySelector("[data-rekap-from]");
  const rekapToInput = root.querySelector("[data-rekap-to]");
  const switches = root.querySelector("[data-rit-switches]");
  const expandAll = root.querySelector("[data-rit-expand-all]");
  const fromInput = root.querySelector("[data-ritase-from]");
  const toInput = root.querySelector("[data-ritase-to]");
  let range = { from: root.dataset.from || "", to: root.dataset.to || "" };
  let day = root.dataset.day || "";
  let ritDay = root.dataset.ritDay || "";
  let rekapRange = { rekap_from: root.dataset.rekapFrom || "", rekap_to: root.dataset.rekapTo || "" };
  // The latest request wins: a slow answer for the date before must not
  // overwrite the date just picked.
  let sequence = 0;

  // The date submits the form without script; with it, the summary is swapped
  // in place and the button is not needed.
  root.querySelectorAll("[data-ritase-date-fallback]").forEach((button) => button.remove());

  const apply = (view) => {
    errorBox.textContent = view.error || "";
    errorBox.hidden = !view.error;
    if (view.available) {
      updated.textContent = view.updatedAt;
      for (const stat of view.stats || []) {
        const cell = root.querySelector(`[data-widya-stat="${stat.key}"]`);
        if (cell) cell.textContent = stat.value;
      }
    }
    // The fragment is the server's own template, escaped there.
    if (view.ritaseHtml) {
      ritase.innerHTML = view.ritaseHtml;
    }
    if (rekap && view.rekapHtml) {
      rekap.innerHTML = view.rekapHtml;
    }
    if (detail && view.detailHtml) {
      detail.innerHTML = view.detailHtml;
    }
    if (rit && view.ritHtml) {
      rit.innerHTML = view.ritHtml;
      fold();
    }
    // Whatever is still waiting on a spinner could not be read; say so, and
    // the next tick tries again. Tables already drawn are left as they are.
    if (!view.ritaseHtml || !view.rekapHtml || !view.detailHtml || !view.ritHtml) {
      settleLoading("Tiket belum bisa dibaca. Dicoba lagi otomatis.");
    }
    // The server settles the range - a reversed or too long one comes back
    // corrected - so the inputs show what was actually summarised.
    if (view.from && view.to) {
      range = { from: view.from, to: view.to };
      // Not while somebody is in the middle of picking a date.
      if (fromInput && document.activeElement !== fromInput) fromInput.value = view.from;
      if (toInput && document.activeElement !== toInput) toInput.value = view.to;
    }
    if (view.day) {
      day = view.day;
      if (dayInput && document.activeElement !== dayInput) dayInput.value = view.day;
    }
    if (view.rekapFrom && view.rekapTo) {
      rekapRange = { rekap_from: view.rekapFrom, rekap_to: view.rekapTo };
      if (rekapFromInput && document.activeElement !== rekapFromInput) rekapFromInput.value = view.rekapFrom;
      if (rekapToInput && document.activeElement !== rekapToInput) rekapToInput.value = view.rekapTo;
    }
    if (view.ritDay) {
      ritDay = view.ritDay;
      if (ritDayInput && document.activeElement !== ritDayInput) ritDayInput.value = view.ritDay;
    }
  };

  // The first load replaces the spinner; if it fails the spinner must not spin
  // on, so it is swapped for a message and the next tick tries again.
  const settleLoading = (message) => {
    for (const loading of root.querySelectorAll("[data-ritase-loading]")) {
      const note = document.createElement("p");
      note.className = "panel-empty";
      note.setAttribute("data-ritase-loading", "");
      note.textContent = message;
      loading.replaceWith(note);
    }
  };

  const refresh = async () => {
    const mine = ++sequence;
    try {
      const query = new URLSearchParams({ ...range, ...rekapRange, day, rit_day: ritDay });
      const response = await fetch(`/produksi-new/overview/data?${query}`, {
        headers: { Accept: "application/json" },
        credentials: "same-origin",
      });
      if (!response.ok) throw new Error(`status ${response.status}`);
      const view = await response.json();
      if (mine === sequence) apply(view);
    } catch {
      if (mine !== sequence) return;
      errorBox.textContent = "Gagal memperbarui data. Angka di bawah adalah pembacaan terakhir.";
      errorBox.hidden = false;
      settleLoading("Tiket belum bisa dibaca. Dicoba lagi otomatis.");
    }
  };

  // Keep the address in step, so a reload or a shared link opens the same
  // dates.
  const remember = () => {
    const address = new URL(window.location.href);
    address.searchParams.set("from", range.from);
    address.searchParams.set("to", range.to);
    address.searchParams.set("day", day);
    address.searchParams.set("rit_day", ritDay);
    address.searchParams.set("rekap_from", rekapRange.rekap_from);
    address.searchParams.set("rekap_to", rekapRange.rekap_to);
    window.history.replaceState(null, "", address);
  };

  const reload = (panels) => {
    const busy = panels.filter(Boolean);
    busy.forEach((panel) => panel.classList.add("is-loading"));
    refresh().finally(() => busy.forEach((panel) => panel.classList.remove("is-loading")));
  };
  const pick = () => {
    if (!fromInput.value || !toInput.value) return;
    range = { from: fromInput.value, to: toInput.value };
    remember();
    reload([ritase]);
  };
  if (fromInput && toInput) {
    fromInput.addEventListener("change", pick);
    toInput.addEventListener("change", pick);
  }
  if (dayInput) {
    dayInput.addEventListener("change", () => {
      if (!dayInput.value) return;
      day = dayInput.value;
      remember();
      reload([detail]);
    });
  }


  const pickRekap = () => {
    if (!rekapFromInput.value || !rekapToInput.value) return;
    rekapRange = { rekap_from: rekapFromInput.value, rekap_to: rekapToInput.value };
    remember();
    reload([rekap]);
  };
  if (rekapFromInput && rekapToInput) {
    rekapFromInput.addEventListener("change", pickRekap);
    rekapToInput.addEventListener("change", pickRekap);
  }

  if (ritDayInput) {
    ritDayInput.addEventListener("change", () => {
      if (!ritDayInput.value) return;
      ritDay = ritDayInput.value;
      remember();
      reload([rit]);
    });
  }

  // Detail Rit per Unit folds: a vendor opens onto its trucks, a truck onto
  // its loads, one line at a time by hand or all at once by the switch. The open lines are kept by name, so a refresh that redraws the
  // table leaves them open.
  const open = new Set();
  const stored = (key) => {
    try {
      return window.localStorage.getItem(key) === "1";
    } catch {
      return false;
    }
  };
  const store = (key, on) => {
    try {
      window.localStorage.setItem(key, on ? "1" : "0");
    } catch {
      // Private windows refuse storage; the switch still works for this visit.
    }
  };

  const rows = () => (rit ? [...rit.querySelectorAll("[data-rit-row]")] : []);

  const fold = () => {
    if (expandAll && expandAll.checked) {
      rows().forEach((row) => row.dataset.key && open.add(row.dataset.key));
    }
    const byKey = new Map(rows().filter((row) => row.dataset.key).map((row) => [row.dataset.key, row]));
    for (const row of rows()) {
      let visible = true;
      // A line shows when every line above it is open.
      for (let parent = row.dataset.parent; parent; parent = byKey.get(parent)?.dataset.parent) {
        if (!open.has(parent)) {
          visible = false;
          break;
        }
      }
      row.hidden = !visible;
      const toggle = row.querySelector("[data-rit-toggle]");
      if (toggle) toggle.setAttribute("aria-expanded", String(open.has(row.dataset.key)));
    }
  };

  if (rit) {
    rit.addEventListener("click", (event) => {
      const toggle = event.target.closest("[data-rit-toggle]");
      if (!toggle) return;
      const key = toggle.dataset.ritToggle;
      if (open.has(key)) {
        open.delete(key);
        // Folding a line by hand means not everything is open any more.
        if (expandAll) expandAll.checked = false;
        store("produksiNew.ritExpandAll", false);
      } else {
        open.add(key);
      }
      fold();
    });
  }

  if (switches && expandAll) {
    switches.hidden = false;
    expandAll.checked = stored("produksiNew.ritExpandAll");
    // One switch: on opens every line, off folds them all.
    expandAll.addEventListener("change", () => {
      if (!expandAll.checked) open.clear();
      store("produksiNew.ritExpandAll", expandAll.checked);
      fold();
    });
  }
  fold();

  // The page came without the ticket tables; fetch them now.
  refresh();

  setInterval(() => {
    // A hidden tab has nobody reading it; it catches up when shown again.
    if (!document.hidden) refresh();
  }, interval);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refresh();
  });
})();
