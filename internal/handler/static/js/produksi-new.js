// Produksi New keeps its figures current while the page is open: it asks the
// server again every interval and writes the answer into the cards. The server
// caches the scanner, so asking costs nothing when nothing has changed.
(() => {
  const root = document.querySelector("[data-widya]");
  if (!root) return;

  const interval = Number(root.dataset.refreshMs) || 30000;
  const errorBox = root.querySelector("[data-widya-error]");
  const updated = root.querySelector("[data-widya-updated]");
  const total = root.querySelector("[data-widya-total]");
  const list = root.querySelector("[data-widya-materials]");
  const empty = root.querySelector("[data-widya-materials-empty]");

  const materialRow = (material) => {
    const item = document.createElement("li");
    const head = document.createElement("div");
    head.className = "share-head";
    const name = document.createElement("span");
    name.textContent = material.name;
    const volume = document.createElement("strong");
    volume.textContent = material.volume;
    head.append(name, volume);

    const track = document.createElement("div");
    track.className = "share-track";
    const bar = document.createElement("progress");
    bar.className = "share-progress";
    bar.max = 100;
    bar.value = Number(material.percent) || 0;
    bar.textContent = `${material.percent}%`;
    track.append(bar);

    const note = document.createElement("p");
    note.className = "share-note";
    note.textContent = `${material.percent}% dari total volume`;
    item.append(head, track, note);
    return item;
  };

  const apply = (view) => {
    errorBox.textContent = view.error || "";
    errorBox.hidden = !view.error;
    if (!view.available) return;
    updated.textContent = view.updatedAt;
    for (const stat of view.stats || []) {
      const cell = root.querySelector(`[data-widya-stat="${stat.key}"]`);
      if (cell) cell.textContent = stat.value;
    }
    total.textContent = view.totalVolume || "0";
    const materials = view.materials || [];
    list.replaceChildren(...materials.map(materialRow));
    empty.hidden = materials.length > 0;
  };

  const refresh = async () => {
    // A hidden tab has nobody reading it; it catches up when shown again.
    if (document.hidden) return;
    try {
      const response = await fetch("/produksi-new/overview/data", {
        headers: { Accept: "application/json" },
        credentials: "same-origin",
      });
      if (!response.ok) throw new Error(`status ${response.status}`);
      apply(await response.json());
    } catch {
      errorBox.textContent = "Gagal memperbarui data. Angka di bawah adalah pembacaan terakhir.";
      errorBox.hidden = false;
    }
  };

  setInterval(refresh, interval);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refresh();
  });
})();
