// Selects that submit their form the moment they change.
//
// This exists as a file rather than an onchange attribute because the app sends
// script-src 'self' with no 'unsafe-inline': an inline handler is not blocked
// loudly, it simply never runs, and the control silently does nothing.
//
// Every such select carries a submit button of its own for the no-script case.
// The button is removed here rather than hidden with CSS, so it is gone exactly
// when something is around to replace it.
(function () {
  const selects = document.querySelectorAll("select[data-auto-submit]");
  if (!selects.length) return;

  selects.forEach((select) => {
    const form = select.form;
    if (!form) return;

    const fallback = form.querySelector("[data-auto-submit-fallback]");
    if (fallback) fallback.remove();

    let submitting = false;
    select.addEventListener("change", () => {
      // A second change while the page is already navigating would submit the
      // form twice.
      if (submitting) return;
      submitting = true;
      select.disabled = true;
      // Disabled controls are left out of a submission, so the value travels in
      // a hidden field instead.
      const carried = document.createElement("input");
      carried.type = "hidden";
      carried.name = select.name;
      carried.value = select.value;
      form.appendChild(carried);
      form.submit();
    });
  });
})();
