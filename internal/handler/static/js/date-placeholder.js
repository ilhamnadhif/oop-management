// The "dd/mm/yyyy" an empty date field shows on a phone or a tablet.
//
// Safari on iOS and iPadOS, and Chrome on Android, draw an empty date field
// as a blank box, where the desktop shows dd/mm/yyyy. The template prints the
// hint in every field and marks the empty ones, and the CSS shows it only on
// a touch screen, so the desktop's own hint is never doubled. This keeps the
// mark in step as a date is picked or cleared.
(function () {
  const fields = document.querySelectorAll("[data-date-field]");
  fields.forEach((field) => {
    const input = field.querySelector("input");
    if (!input) return;
    const sync = () => field.toggleAttribute("data-empty", input.value === "");
    input.addEventListener("input", sync);
    input.addEventListener("change", sync);
    sync();
  });
})();
