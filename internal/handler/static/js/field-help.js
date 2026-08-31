// Field help bubbles open on click as well as on hover.
//
// Hover and :focus-within already cover a mouse and a keyboard, and on iOS the
// first tap fires a hover. What they do not cover is a click in Safari and
// Firefox on the desktop, which do not focus a button when it is clicked. So
// the click is handled here, and the CSS keeps working on its own if this
// script never loads.
(function () {
  const marks = document.querySelectorAll(".field-help-mark");
  if (!marks.length) return;

  const closeAll = (except) => {
    document.querySelectorAll(".field-help[data-open]").forEach((help) => {
      if (help === except) return;
      delete help.dataset.open;
      const mark = help.querySelector(".field-help-mark");
      if (mark) mark.setAttribute("aria-expanded", "false");
    });
  };

  marks.forEach((mark) => {
    mark.setAttribute("aria-expanded", "false");
    mark.addEventListener("click", (event) => {
      // Inside a form, a bare button would submit it.
      event.preventDefault();
      const help = mark.closest(".field-help");
      if (!help) return;
      const open = help.dataset.open !== undefined;
      closeAll(help);
      if (open) {
        delete help.dataset.open;
      } else {
        help.dataset.open = "";
      }
      mark.setAttribute("aria-expanded", open ? "false" : "true");
    });
  });

  // A bubble left open would follow the eye around the page.
  document.addEventListener("click", (event) => {
    if (!event.target.closest(".field-help")) closeAll(null);
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") closeAll(null);
  });
})();
