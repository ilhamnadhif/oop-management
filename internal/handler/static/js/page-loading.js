// A popup with a spinner while a filter reloads the page.
//
// Applying a filter reloads the whole page, and a page that reads a month of
// a sheet can take a few seconds to answer, during which nothing on screen
// says the click was taken. The popup is held back for a moment, so a page
// that answers at once never flashes it.
//
// It covers the filter forms' Terapkan buttons and their Reset links. Download
// buttons are left alone: a download does not leave the page, so nothing would
// ever take the popup down.
(function () {
  const delay = 250;
  let overlay = null;
  let timer = 0;

  function build() {
    overlay = document.createElement("div");
    overlay.className = "page-loading";
    overlay.setAttribute("role", "status");
    overlay.setAttribute("aria-live", "polite");
    overlay.hidden = true;
    const card = document.createElement("div");
    card.className = "page-loading-card";
    const spinner = document.createElement("span");
    spinner.className = "spinner";
    spinner.setAttribute("aria-hidden", "true");
    const text = document.createElement("span");
    text.textContent = "Memuat data…";
    card.append(spinner, text);
    overlay.appendChild(card);
    document.body.appendChild(overlay);
  }

  function show() {
    if (!overlay) build();
    clearTimeout(timer);
    timer = setTimeout(() => {
      overlay.hidden = false;
    }, delay);
  }

  function hide() {
    clearTimeout(timer);
    if (overlay) overlay.hidden = true;
  }

  // The project switcher submits from script, which fires no submit event.
  window.showPageLoading = show;

  const filters = "form.filter-form, form.approval-filter-form, form[data-page-loading]";

  document.addEventListener("submit", (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement) || !form.matches(filters)) return;
    // Another handler turned the submission down, so the page stays.
    if (event.defaultPrevented) return;
    show();
  });

  document.addEventListener("click", (event) => {
    const link = event.target instanceof Element ? event.target.closest("a[href]") : null;
    if (!link || !link.closest(filters)) return;
    // A click that opens elsewhere, or a download, leaves this page standing.
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    if (link.target && link.target !== "_self") return;
    if (link.hasAttribute("download")) return;
    show();
  });

  // Coming back to the page from the browser's history shows it as it was
  // left, popup and all. pageshow also fires as the page first loads, which
  // is no reason to take anything down.
  window.addEventListener("pageshow", (event) => {
    if (event.persisted) hide();
  });
})();
