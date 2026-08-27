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
  const counts = panel.querySelector("[data-scan-counts]");
  const date = panel.querySelector("[data-scan-date]");
  const payload = panel.querySelector("[data-scan-rows]");
  const csrf = panel.querySelector("[name='csrf_token']");
  if (!file || !button || !dialog || !body || !save || !payload || !counts) return;

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

  // The two columns a misread actually lands in are the two that can be typed
  // over. The rest stays as text: a hundred rows retyped in a dialog is the work
  // this feature exists to remove, and a plate or a height is a keystroke.
  const NOPOL_KOSONG = "Nopol wajib diisi";
  const NOPOL_FORMAT = "Format nopol harus seperti B 1234 ABC";
  const NOPOL_BELUM_TERDAFTAR = "Nopol belum terdaftar di Unit DT";
  const TT_MINUS = "TT tidak boleh minus";
  // The register's own shape: one or two letters, up to four digits, up to
  // three letters, single spaces between.
  const NOPOL_SHAPE = /^[A-Z]{1,2} [0-9]{1,4} [A-Z]{1,3}$/;
  const NOPOL_UNSPACED = /^([A-Z]{1,2}) *([0-9]{1,4}) *([A-Z]{1,3})$/;

  // What the server does to a plate before it looks it up: upper case, single
  // spaces. Doing the same here is what lets the dialog agree with the result.
  const settle = (value) => String(value || "").trim().toUpperCase().replace(/\s+/g, " ");

  // A plate read off a photograph often arrives with its spaces missing, and the
  // groups are unambiguous once the shape is known. Putting them back is the
  // difference between correcting a row and retyping it. Anything that does not
  // fit is left exactly as typed, for the operator to see and fix.
  const tidyPlate = (value) => {
    const settled = settle(value);
    if (NOPOL_SHAPE.test(settled)) return settled;
    const parts = NOPOL_UNSPACED.exec(settled.replace(/\s+/g, ""));
    return parts ? `${parts[1]} ${parts[2]} ${parts[3]}` : settled;
  };

  // The plates the register knows, read off the picker the entry form already
  // renders. The server judges every row again regardless; this only lets the
  // dialog say so before the sheet is filed.
  const registered = new Set(
    Array.from(document.querySelectorAll("#unitList option")).map((option) => settle(option.value)).filter(Boolean),
  );

  let rows = [];

  // The checks run in the order the server runs them, and name the same
  // reasons, so the dialog and the result never disagree about a row.
  const judge = (row) => {
    if (!Number.isFinite(row.tt) || row.tt < 0) return TT_MINUS;
    const plate = settle(row.nopol);
    if (!plate) return NOPOL_KOSONG;
    if (!NOPOL_SHAPE.test(plate)) return NOPOL_FORMAT;
    if (!registered.has(plate)) return NOPOL_BELUM_TERDAFTAR;
    return "";
  };

  const textCell = (text, numeric) => {
    const cell = document.createElement("td");
    if (numeric) cell.className = "numeric";
    cell.textContent = text;
    return cell;
  };

  const renderRows = () => {
    body.textContent = "";
    rows.forEach((row, index) => {
      const tr = document.createElement("tr");
      tr.appendChild(textCell(String(row.no || ""), false));

      const plateCell = document.createElement("td");
      const plate = document.createElement("input");
      plate.type = "text";
      plate.className = "produksi-scan-edit";
      plate.value = row.nopol || "";
      plate.setAttribute("list", "unitList");
      plate.setAttribute("autocomplete", "off");
      plate.setAttribute("aria-label", `Nopol baris ${row.no || index + 1}`);
      plateCell.appendChild(plate);
      tr.appendChild(plateCell);

      tr.appendChild(textCell(row.lokasi || "", false));
      tr.appendChild(textCell(row.layer || "", false));

      const heightCell = document.createElement("td");
      heightCell.className = "numeric";
      const height = document.createElement("input");
      height.type = "number";
      height.step = "0.01";
      height.min = "0";
      height.className = "produksi-scan-edit numeric";
      height.value = Number.isFinite(row.tt) ? String(row.tt) : "0";
      height.setAttribute("aria-label", `TT baris ${row.no || index + 1}`);
      heightCell.appendChild(height);
      tr.appendChild(heightCell);

      const status = textCell("", false);
      tr.appendChild(status);
      body.appendChild(tr);

      const restate = () => {
        row.alasan = judge(row);
        status.textContent = row.alasan || "Siap";
        tr.className = row.alasan ? "produksi-scan-skipped" : "";
        refreshSave();
      };
      plate.addEventListener("input", () => {
        row.nopol = plate.value;
        restate();
      });
      // Tidied when the field is left rather than as it is typed: rewriting
      // under someone's cursor moves it, and half a plate is not a plate yet.
      plate.addEventListener("blur", () => {
        const tidied = tidyPlate(plate.value);
        if (tidied === plate.value) return;
        plate.value = tidied;
        row.nopol = tidied;
        restate();
      });
      height.addEventListener("input", () => {
        // An emptied box is a level load, which is what a blank cell on the
        // paper means too.
        const typed = height.value.trim();
        row.tt = typed === "" ? 0 : Number(typed);
        restate();
      });
      restate();
    });
  };

  const showResult = (result) => {
    rows = result.rows;
    renderRows();

    summary.textContent = `AI membaca ${rows.length} baris dari foto.`;
    const warnings = Array.isArray(result.warnings)
      ? result.warnings.map((warning) => String(warning).trim()).filter(Boolean).slice(0, 3)
      : [];
    note.textContent = "Betulkan nopol atau TT yang salah baca langsung di tabel."
      + (warnings.length ? ` Catatan AI: ${warnings.join(" • ")}` : "");

    refreshSave();
    if (typeof dialog.showModal === "function") dialog.showModal();
  };

  // Nothing is filed without a date, and the button says which of the two is
  // missing rather than sitting greyed out for an unstated reason.
  const refreshSave = () => {
    const ready = rows.filter((row) => !row.alasan).length;
    const skipped = rows.length - ready;
    const dated = date && date.value.trim() !== "";
    if (rows.length > 0) {
      const parts = [`${ready} siap simpan`];
      if (skipped > 0) parts.push(`${skipped} dilewati`);
      counts.textContent = parts.join(" · ");
    }
    if (ready === 0) {
      save.textContent = "Tidak ada yang bisa disimpan";
    } else if (!dated) {
      save.textContent = "Isi tanggal dulu";
    } else {
      save.textContent = `Simpan ${ready} baris`;
    }
    save.disabled = ready === 0 || !dated;
  };
  if (date) date.addEventListener("input", refreshSave);

  // The payload is written at the moment of submitting, from the rows as they
  // now read. Every row goes back, rejected ones included: the server judges
  // them all again and reports what it left behind, so the page never has to
  // decide which rows were storable.
  panel.addEventListener("submit", () => {
    payload.value = JSON.stringify(rows);
  });

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
    // The server states its own budget, so the browser cannot give up on a scan
    // that is still running and leave the operator thinking it failed.
    const budget = Number(panel.dataset.scanTimeout) || 150000;
    const timeout = window.setTimeout(() => controller.abort(), budget + 5000);
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

  // The field opens on today, so the common case needs no typing at all and the
  // empty box that used to read as a format hint is gone.
  refreshSave();

  // A photo swapped after a scan would be filed against the previous reading,
  // so the confirmed rows are dropped with it.
  file.addEventListener("change", () => {
    rows = [];
    payload.value = "";
    body.textContent = "";
    counts.textContent = "";
  });
})();
