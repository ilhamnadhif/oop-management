(() => {
  const form = document.querySelector("[data-nota-form]");
  if (!form) return;

  const metode = form.querySelector("#metode");
  const kategori = form.querySelector("#kategori");
  const subKategori = form.querySelector("#sub_kategori");
  const jenisField = form.querySelector("[data-perjalanan-field]");
  const jenis = form.querySelector("#jenis_perjalanan");
  const reimburseField = form.querySelector("[data-reimburse-field]");
  const penerima = form.querySelector("#penerima_reimburse");
  const transferField = form.querySelector("[data-transfer-field]");
  const transfer = form.querySelector("#bukti_transfer");
  const status = form.querySelector("[data-pay-status]");
  const statusLabel = form.querySelector("[data-pay-status-label]");
  const list = form.querySelector("[data-item-list]");
  const totalCell = form.querySelector("[data-nota-total]");

  const PERJALANAN_DINAS = "Perjalanan Dinas";

  const rupiah = (value) =>
    "Rp " + Math.round(value).toLocaleString("id-ID");

  const decimal = (value) => {
    const parsed = Number.parseFloat(String(value).replace(",", "."));
    return Number.isFinite(parsed) ? parsed : 0;
  };

  const digitsOf = (value) => String(value).replace(/\D/g, "");

  const group = (digits) => digits.replace(/\B(?=(\d{3})+(?!\d))/g, ".");

  // Reformats while typing and puts the caret back where it was, counted in
  // digits: counting characters would drift every time a separator appears or
  // disappears ahead of the cursor.
  const formatMoney = (input) => {
    const before = digitsOf(input.value.slice(0, input.selectionStart ?? input.value.length)).length;
    const digits = digitsOf(input.value).replace(/^0+(?=\d)/, "");
    input.value = digits ? group(digits) : "";
    let position = 0;
    let seen = 0;
    while (position < input.value.length && seen < before) {
      if (/\d/.test(input.value[position])) seen += 1;
      position += 1;
    }
    if (input.selectionStart !== null) {
      try {
        input.setSelectionRange(position, position);
      } catch (error) {
        // A field that lost focus mid-edit cannot take a selection; the value
        // is already correct, which is what matters.
      }
    }
  };

  // A hidden field must not be required, or the browser refuses to submit a
  // form while pointing at a control nobody can see.
  const toggle = (field, input, visible) => {
    if (!field) return;
    field.hidden = !visible;
    if (input) input.required = visible;
    if (input && !visible) input.value = "";
  };

  const applyMetode = () => {
    const isCA = metode && metode.value === "CA";
    toggle(reimburseField, penerima, !isCA);
    // A file input keeps whatever the person already chose; clearing it on
    // every re-render would silently drop an attachment they picked first.
    if (transferField) {
      transferField.hidden = !isCA;
      if (transfer) transfer.required = isCA;
    }
    if (status) status.classList.toggle("paid", isCA);
    if (statusLabel) statusLabel.textContent = isCA ? "SUDAH DIBAYAR" : "BELUM DIBAYAR";
  };

  // The sub category list is rendered whole so the page works without this
  // script; here it is narrowed to the category actually chosen.
  const applyKategori = () => {
    if (!kategori || !subKategori) return;
    const chosen = kategori.value;
    Array.from(subKategori.querySelectorAll("optgroup")).forEach((optgroup) => {
      optgroup.hidden = Boolean(chosen) && optgroup.dataset.kategori !== chosen;
      optgroup.disabled = optgroup.hidden;
    });
    const selected = subKategori.selectedOptions[0];
    if (selected && selected.dataset.kategori && selected.dataset.kategori !== chosen) {
      subKategori.value = "";
    }
    applySubKategori();
  };

  const applySubKategori = () => {
    const isTrip = subKategori && subKategori.value === PERJALANAN_DINAS;
    toggle(jenisField, jenis, isTrip);
  };

  const recalculate = () => {
    let total = 0;
    form.querySelectorAll("[data-item-row]").forEach((row) => {
      const volume = decimal(row.querySelector("[name='item_volume']").value);
      // The price carries grouping dots, so only its digits are a number.
      const harga = decimal(digitsOf(row.querySelector("[name='item_harga']").value));
      const subtotal = volume * harga;
      total += subtotal;
      const cell = row.querySelector("[data-item-total]");
      if (cell) cell.textContent = rupiah(subtotal);
    });
    if (totalCell) totalCell.textContent = rupiah(total);
  };

  const addRow = () => {
    if (!list) return;
    const first = list.querySelector("[data-item-row]");
    if (!first) return;
    const row = first.cloneNode(true);
    row.querySelectorAll("input").forEach((input) => {
      input.value = "";
    });
    const cell = row.querySelector("[data-item-total]");
    if (cell) cell.textContent = rupiah(0);
    list.appendChild(row);
    const nama = row.querySelector("[name='item_nama']");
    if (nama) nama.focus();
  };

  // The last row is emptied rather than removed: a nota needs at least one
  // line, and a list with no rows leaves nothing to clone from.
  const removeRow = (row) => {
    const rows = form.querySelectorAll("[data-item-row]");
    if (rows.length > 1) {
      row.remove();
    } else {
      row.querySelectorAll("input").forEach((input) => {
        input.value = "";
      });
    }
    recalculate();
  };

  form.addEventListener("click", (event) => {
    if (event.target.closest("[data-add-item]")) {
      addRow();
      return;
    }
    const remove = event.target.closest("[data-remove-item]");
    if (remove) removeRow(remove.closest("[data-item-row]"));
  });

  form.addEventListener("input", (event) => {
    if (event.target.matches("[data-money]")) formatMoney(event.target);
    if (event.target.matches("[name='item_volume'], [name='item_harga']")) recalculate();
  });

  // The separators are a reading aid only. They are stripped on the way out so
  // the sheet stores a plain number.
  form.addEventListener("submit", () => {
    form.querySelectorAll("[data-money]").forEach((input) => {
      input.value = digitsOf(input.value);
    });
  });

  if (metode) metode.addEventListener("change", applyMetode);
  if (kategori) kategori.addEventListener("change", applyKategori);
  if (subKategori) subKategori.addEventListener("change", applySubKategori);

  applyMetode();
  applyKategori();
  // A submission that came back with errors returns plain digits; group them
  // again so the field reads the same as it did before it was sent.
  form.querySelectorAll("[data-money]").forEach(formatMoney);
  recalculate();
})();
