(() => {
  // The shortfall only exists when the delivery does not match the note, so the
  // field follows the keterangan. The server enforces the same rule: with this
  // script switched off the page still renders the field for a mismatched
  // delivery, and a matching one stores a zero whatever was typed.
  const form = document.querySelector("[data-fuel-form]");
  if (!form) return;

  const keterangan = form.querySelector("[data-fuel-keterangan]");
  const block = form.querySelector("[data-fuel-selisih]");
  if (!keterangan || !block) return;

  const field = block.querySelector("input");

  const sync = () => {
    const mismatched = keterangan.value === "tidak sesuai";
    block.hidden = !mismatched;
    if (!field) return;
    field.required = mismatched;
    if (!mismatched) field.value = "";
  };

  keterangan.addEventListener("change", sync);
  sync();
})();
