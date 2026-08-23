(() => {
  const panel = document.querySelector("[data-produksi-scan]");
  if (!panel) return;

  const file = panel.querySelector("[data-scan-file]");
  const button = panel.querySelector("[data-scan-button]");
  const label = panel.querySelector("[data-scan-label]");
  const spinner = panel.querySelector("[data-scan-spinner]");
  const status = panel.querySelector("[data-scan-status]");
  const dialog = panel.querySelector("[data-scan-dialog]");
  const summary = panel.querySelector("[data-scan-summary]");
  const note = panel.querySelector("[data-scan-note]");
  const body = panel.querySelector("[data-scan-rows-body]");
  const save = panel.querySelector("[data-scan-save]");
  const payload = panel.querySelector("[data-scan-rows]");
  const csrf = panel.querySelector("[name='csrf_token']");
  if (!file || !button || !dialog || !body || !save || !payload) return;

  const setStatus = (message, tone) => {
    if (!status) return;
    status.textContent = message;
    status.dataset.tone = tone || "";
  };

  const setBusy = (busy) => {
    button.disabled = busy;
    if (spinner) spinner.hidden = !busy;
    if (label) label.textContent = busy ? "Membaca lembar…" : "Baca lembar dengan AI";
  };

  // The sheet is confirmed, not edited: a hundred rows retyped in a dialog is
  // the work this feature exists to remove. A misread is fixed by retaking the
  // photo, so the table is written as text and nothing in it is an input.
  const renderRows = (rows) => {
    body.textContent = "";
    rows.forEach((row) => {
      const tr = document.createElement("tr");
      const storable = !row.alasan;
      if (!storable) tr.className = "produksi-scan-skipped";
      [
        String(row.no || ""),
        row.tanggal || "",
        row.nopol || "",
        row.lokasi || "",
        row.layer || "",
        typeof row.tt === "number" ? String(row.tt) : "",
        storable ? "Siap" : row.alasan,
      ].forEach((text, index) => {
        const cell = document.createElement("td");
        if (index === 5) cell.className = "numeric";
        cell.textContent = text;
        tr.appendChild(cell);
      });
      body.appendChild(tr);
    });
  };

  const showResult = (result) => {
    // Every row goes back, rejected ones included. The server judges them all
    // again and reports what it left behind, so the page never has to decide
    // which rows were storable.
    payload.value = JSON.stringify(result.rows);
    renderRows(result.rows);

    const siap = Number(result.siap || 0);
    const ditolak = Number(result.ditolak || 0);
    summary.textContent = `AI membaca ${result.rows.length} baris dari foto.`;
    const parts = [`${siap} siap simpan`];
    if (ditolak > 0) parts.push(`${ditolak} dilewati`);
    const warnings = Array.isArray(result.warnings)
      ? result.warnings.map((warning) => String(warning).trim()).filter(Boolean).slice(0, 3)
      : [];
    note.textContent = parts.join(" · ") + (warnings.length ? ` · Catatan AI: ${warnings.join(" • ")}` : "");

    save.textContent = siap > 0 ? `Simpan ${siap} baris` : "Tidak ada yang bisa disimpan";
    save.disabled = siap === 0;
    if (typeof dialog.showModal === "function") dialog.showModal();
  };

  button.addEventListener("click", async () => {
    if (panel.dataset.scanEnabled !== "true") return;
    const picked = file.files && file.files[0];
    if (!picked) {
      file.click();
      return;
    }

    const form = new FormData();
    form.append("lembar", picked, picked.name || "lembar");
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 28000);
    setBusy(true);
    setStatus("AI sedang membaca seluruh baris pada lembar…");

    try {
      const response = await fetch("/produksi/scan", {
        method: "POST",
        body: form,
        credentials: "same-origin",
        headers: {
          Accept: "application/json",
          "X-CSRF-Token": csrf ? csrf.value : "",
        },
        signal: controller.signal,
      });
      let result;
      try {
        result = await response.json();
      } catch (error) {
        throw new Error("Layanan scan mengirim respons yang tidak dapat dibaca.");
      }
      if (!response.ok || !result || result.ok !== true || !Array.isArray(result.rows)) {
        throw new Error(result && result.error ? String(result.error) : "Scan lembar gagal.");
      }
      showResult(result);
      setStatus(`${result.rows.length} baris terbaca. Periksa daftarnya sebelum disimpan.`, "success");
    } catch (error) {
      const message = error && error.name === "AbortError"
        ? "Scan terlalu lama. Silakan coba lagi atau isi form secara manual."
        : String(error && error.message ? error.message : "Scan lembar gagal. Silakan coba lagi.");
      setStatus(message, "error");
    } finally {
      window.clearTimeout(timeout);
      setBusy(false);
    }
  });

  // A photo swapped after a scan would be filed against the previous reading,
  // so the confirmed rows are dropped with it.
  file.addEventListener("change", () => {
    payload.value = "";
    body.textContent = "";
  });
})();
