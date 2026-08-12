(() => {
  // Two read-only fields: the unit name that belongs to the chosen id, and the
  // litres between the two flowmeter readings. Both are shown for the person
  // filling the form; the server derives its own values from the id and the
  // readings, so this script switched off costs display, not correctness.
  const form = document.querySelector("[data-fuel-keluar-form]");
  if (!form) return;

  const unit = form.querySelector("[data-fuel-unit]");
  const namaUnit = form.querySelector("[data-fuel-nama-unit]");
  const awal = form.querySelector("[data-fuel-awal]");
  const akhir = form.querySelector("[data-fuel-akhir]");
  const liter = form.querySelector("[data-fuel-liter]");

  const syncNamaUnit = () => {
    if (!unit || !namaUnit) return;
    const selected = unit.options[unit.selectedIndex];
    namaUnit.value = (selected && selected.dataset.namaUnit) || "";
  };

  // An Indonesian keyboard produces a decimal comma. The server accepts either,
  // and so does this preview.
  const decimal = (value) => Number.parseFloat(String(value || "").replace(",", "."));

  const syncLiter = () => {
    if (!awal || !akhir || !liter) return;
    const from = decimal(awal.value);
    const to = decimal(akhir.value);
    if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from) {
      liter.value = "";
      return;
    }
    // Two decimals, without the trailing zeros a fixed format would add.
    liter.value = String(Math.round((to - from) * 100) / 100);
  };

  if (unit) unit.addEventListener("change", syncNamaUnit);
  if (awal) awal.addEventListener("input", syncLiter);
  if (akhir) akhir.addEventListener("input", syncLiter);

  syncNamaUnit();
  syncLiter();
})();
