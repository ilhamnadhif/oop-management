(() => {
  const form = document.querySelector("[data-leave-form]");

  const parseDate = (value) => {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value || "")) return null;
    const date = new Date(`${value}T00:00:00Z`);
    const [year, month, day] = value.split("-").map(Number);
    if (
      Number.isNaN(date.getTime()) ||
      date.getUTCFullYear() !== year ||
      date.getUTCMonth() + 1 !== month ||
      date.getUTCDate() !== day
    ) return null;
    return date;
  };

  const weekdaysInclusive = (fromValue, toValue) => {
    const from = parseDate(fromValue);
    const to = parseDate(toValue);
    if (!from || !to || from > to) return null;

    const totalDays = Math.floor((to.getTime() - from.getTime()) / 86400000) + 1;
    let total = Math.floor(totalDays / 7) * 5;
    const remainder = totalDays % 7;
    for (let offset = 0; offset < remainder; offset += 1) {
      const day = (from.getUTCDay() + offset) % 7;
      if (day !== 0 && day !== 6) total += 1;
    }
    return total;
  };

  if (form) {
    const type = form.querySelector("#jenis_leave");
    const from = form.querySelector("#tanggal_mulai");
    const to = form.querySelector("#tanggal_selesai");
    const days = form.querySelector("[data-leave-days]");
    const file = form.querySelector("#bukti_pendukung");
    const remove = form.querySelector("#hapus_bukti");
    const attachmentHint = form.querySelector("[data-leave-attachment-hint]");
    const hasExisting = form.dataset.hasAttachment === "true";

    const updateDays = () => {
      if (!days || !from || !to) return;
      const count = weekdaysInclusive(from.value, to.value);
      days.textContent = count === null
        ? "Pilih rentang tanggal yang valid"
        : `${count} hari kerja (Senin–Jumat)`;
      days.dataset.state = count !== null && count > 0 ? "ready" : "idle";
    };

    const updateAttachment = () => {
      if (!type || !file) return;
      const sick = type.value === "Cuti Sakit";
      if (remove) {
        remove.disabled = sick && !file.files.length;
        if (remove.disabled) remove.checked = false;
      }
      const keepingExisting = hasExisting && !(remove && remove.checked);
      file.required = sick && !keepingExisting;
      if (attachmentHint) {
        attachmentHint.textContent = sick
          ? "Wajib untuk Cuti Sakit. Gunakan JPEG, PNG, atau WebP maksimal 2 MB."
          : "Opsional. Gunakan JPEG, PNG, atau WebP maksimal 2 MB.";
      }
    };

    [from, to].forEach((field) => field && field.addEventListener("change", updateDays));
    if (type) type.addEventListener("change", updateAttachment);
    if (file) file.addEventListener("change", updateAttachment);
    if (remove) remove.addEventListener("change", updateAttachment);
    updateDays();
    updateAttachment();
  }

  document.querySelectorAll("[data-confirm-cancel]").forEach((button) => {
    button.addEventListener("click", (event) => {
      if (!window.confirm("Batalkan pengajuan leave ini? Tindakan ini tidak dapat diurungkan.")) {
        event.preventDefault();
      }
    });
  });

  document.querySelectorAll("[data-leave-decision-form]").forEach((decisionForm) => {
    const note = decisionForm.querySelector("[name=catatan_approval]");
    decisionForm.addEventListener("submit", (event) => {
      if (!note) return;
      const decision = event.submitter && event.submitter.value;
      note.required = decision === "reject";
      if (!decisionForm.checkValidity()) {
        event.preventDefault();
        decisionForm.reportValidity();
      }
    });
  });
})();
