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
  const fromInput = root.querySelector("[data-ritase-from]");
  const toInput = root.querySelector("[data-ritase-to]");
  let range = { from: root.dataset.from || "", to: root.dataset.to || "" };
  let day = root.dataset.day || "";
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
      if (rekap && view.rekapHtml) rekap.innerHTML = view.rekapHtml;
    }
    if (detail && view.detailHtml) {
      detail.innerHTML = view.detailHtml;
    }
    // Whatever is still waiting on a spinner could not be read; say so, and
    // the next tick tries again. Tables already drawn are left as they are.
    if (!view.ritaseHtml || !view.detailHtml) {
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
      const query = new URLSearchParams({ ...range, day });
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
    reload([ritase, rekap]);
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

  // The page came without the summary; fetch it now.
  refresh();

  setInterval(() => {
    // A hidden tab has nobody reading it; it catches up when shown again.
    if (!document.hidden) refresh();
  }, interval);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refresh();
  });
})();
