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
  const fromInput = root.querySelector("[data-ritase-from]");
  const toInput = root.querySelector("[data-ritase-to]");
  let range = { from: root.dataset.from || "", to: root.dataset.to || "" };
  // The latest request wins: a slow answer for the date before must not
  // overwrite the date just picked.
  let sequence = 0;

  // The date submits the form without script; with it, the summary is swapped
  // in place and the button is not needed.
  const fallback = root.querySelector("[data-ritase-date-fallback]");
  if (fallback) fallback.remove();

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
    } else {
      settleLoading("Tiket rentang ini belum bisa dibaca. Dicoba lagi otomatis.");
    }
    // The server settles the range - a reversed or too long one comes back
    // corrected - so the inputs show what was actually summarised.
    if (view.from && view.to) {
      range = { from: view.from, to: view.to };
      // Not while somebody is in the middle of picking a date.
      if (fromInput && document.activeElement !== fromInput) fromInput.value = view.from;
      if (toInput && document.activeElement !== toInput) toInput.value = view.to;
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
      const query = new URLSearchParams(range);
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
      settleLoading("Tiket rentang ini belum bisa dibaca. Dicoba lagi otomatis.");
    }
  };

  const pick = () => {
    if (!fromInput.value || !toInput.value) return;
    range = { from: fromInput.value, to: toInput.value };
    // Keep the address in step, so a reload or a shared link opens the same range.
    const address = new URL(window.location.href);
    address.searchParams.set("from", range.from);
    address.searchParams.set("to", range.to);
    window.history.replaceState(null, "", address);
    const busy = [ritase, rekap].filter(Boolean);
    busy.forEach((panel) => panel.classList.add("is-loading"));
    refresh().finally(() => busy.forEach((panel) => panel.classList.remove("is-loading")));
  };
  if (fromInput && toInput) {
    fromInput.addEventListener("change", pick);
    toInput.addEventListener("change", pick);
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
