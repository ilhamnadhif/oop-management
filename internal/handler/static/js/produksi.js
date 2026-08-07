(() => {
  const form = document.querySelector("[data-produksi-form]");
  if (!form) return;

  const nopol = form.querySelector("#nopol");
  const tt = form.querySelector("#tt");
  const list = form.querySelector("#unitList");
  const fields = {};
  form.querySelectorAll("[data-unit-field]").forEach((field) => {
    fields[field.dataset.unitField] = field;
  });
  const calc = {};
  form.querySelectorAll("[data-calc]").forEach((cell) => {
    calc[cell.dataset.calc] = cell;
  });

  // Mirrors the server formula: TF is shown as calculated, only the volume is
  // scaled down to cubic metres.
  const VOLUME_DIVISOR = 1e6;
  const VOLUME_OPP = { "DT KECIL": 10, "DT BESAR": 28 };

  const number = (value) => {
    const parsed = Number.parseFloat(String(value).replace(",", "."));
    return Number.isFinite(parsed) ? parsed : 0;
  };

  const selectedUnit = () => {
    if (!list || !nopol.value) return null;
    const wanted = nopol.value.trim().toUpperCase();
    return Array.from(list.options).find((option) => option.value.toUpperCase() === wanted) || null;
  };

  const fillUnit = () => {
    const option = selectedUnit();
    Object.entries(fields).forEach(([key, field]) => {
      field.value = option ? option.dataset[key] || "" : "";
    });
    return option;
  };

  const recalculate = () => {
    const option = fillUnit();
    if (!option) {
      calc.tf.textContent = "0.00 m";
      calc.volume.textContent = "0.0000 m³";
      calc.opp.textContent = "0 m³";
      calc.deviasi.textContent = "0.0000 m³";
      return;
    }
    const panjang = number(option.dataset.panjang);
    const lebar = number(option.dataset.lebar);
    const tinggi = number(option.dataset.tinggi);
    const tambahan = number(tt.value);

    const tf = tinggi + tambahan / 2;
    const volume = (panjang * lebar * tf) / VOLUME_DIVISOR;
    const opp = VOLUME_OPP[(option.dataset.jenis || "").trim().toUpperCase()] || 0;

    calc.tf.textContent = `${tf.toFixed(2)} m`;
    calc.volume.textContent = `${volume.toFixed(4)} m³`;
    calc.opp.textContent = `${opp} m³`;
    calc.deviasi.textContent = `${(volume - opp).toFixed(4)} m³`;
  };

  nopol.addEventListener("input", recalculate);
  nopol.addEventListener("change", recalculate);
  tt.addEventListener("input", recalculate);
  recalculate();
})();
