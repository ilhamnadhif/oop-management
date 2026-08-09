(() => {
  // The triggers are links that open the page with the dialog already showing,
  // so the feature works with this script switched off. Here they are upgraded
  // to open the dialog in place, without the round trip.
  const openers = document.querySelectorAll("[data-open-dialog]");
  if (!openers.length) return;

  const dialogFor = (element) => document.getElementById(element.dataset.openDialog);

  openers.forEach((opener) => {
    const dialog = dialogFor(opener);
    if (!dialog || typeof dialog.showModal !== "function") return;
    opener.addEventListener("click", (event) => {
      event.preventDefault();
      dialog.showModal();
    });
  });

  document.querySelectorAll("dialog.modal").forEach((dialog) => {
    if (typeof dialog.showModal !== "function") return;

    // A server-rendered dialog is open but not modal, so it has no backdrop and
    // no focus trap. Reopening it as a modal gives it both.
    if (dialog.open) {
      dialog.close();
      dialog.showModal();
    }

    const close = dialog.querySelector("[data-close-dialog]");
    if (close) {
      close.addEventListener("click", (event) => {
        event.preventDefault();
        dialog.close();
      });
    }

    // Clicking the backdrop closes it. The dialog element covers only its own
    // box, so a click landing outside that box came from the backdrop.
    dialog.addEventListener("click", (event) => {
      if (event.target !== dialog) return;
      const box = dialog.getBoundingClientRect();
      const outside =
        event.clientX < box.left || event.clientX > box.right ||
        event.clientY < box.top || event.clientY > box.bottom;
      if (outside) dialog.close();
    });
  });
})();
