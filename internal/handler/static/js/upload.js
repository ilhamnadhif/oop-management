(() => {
  // On a phone, a file input either opens the camera or opens the file list,
  // and which one it does is up to the browser. `capture` forces the camera and
  // hides the gallery; leaving it off buries the camera behind a menu that
  // differs on every device. So the choice is asked here instead: one sheet,
  // two buttons, the same on any phone.
  //
  // A mouse and keyboard get nothing: there the file picker already is the
  // browse dialog, and there is usually no camera to offer.
  const touchScreen = window.matchMedia("(hover: none) and (pointer: coarse)");
  if (!touchScreen.matches) return;

  const inputs = Array.from(document.querySelectorAll('input[type="file"]'));
  if (!inputs.length) return;

  // Only offer the camera for fields that want a picture. A field that accepts
  // a PDF has nothing to photograph.
  const takesPhotos = (input) => {
    const accept = (input.getAttribute("accept") || "").toLowerCase();
    return accept === "" || accept.includes("image");
  };

  const sheet = document.createElement("dialog");
  sheet.className = "modal upload-sheet";
  sheet.innerHTML = `
    <div class="modal-head">
      <div><h2 class="upload-sheet-title">Ambil dari mana?</h2></div>
    </div>
    <div class="upload-sheet-actions">
      <button class="button primary" type="button" data-upload-camera>Ambil foto</button>
      <button class="button subtle" type="button" data-upload-browse>Pilih file</button>
      <button class="button subtle upload-sheet-cancel" type="button" data-upload-cancel>Batal</button>
    </div>`;
  document.body.appendChild(sheet);

  const camera = sheet.querySelector("[data-upload-camera]");
  const browse = sheet.querySelector("[data-upload-browse]");
  const cancel = sheet.querySelector("[data-upload-cancel]");

  // The field the sheet was opened for, and the flag that lets its second click
  // through: reopening the sheet from inside its own handler would loop.
  let target = null;
  let passThrough = false;

  const close = () => {
    if (sheet.open) sheet.close();
    target = null;
  };

  // Opening the picker has to happen inside the button's own gesture, or the
  // browser drops it as an unprompted dialog.
  const openPicker = (wantsCamera) => {
    const input = target;
    if (!input) return;
    close();
    if (wantsCamera) input.setAttribute("capture", "environment");
    else input.removeAttribute("capture");
    passThrough = true;
    input.click();
    passThrough = false;
  };

  camera.addEventListener("click", () => openPicker(true));
  browse.addEventListener("click", () => openPicker(false));
  cancel.addEventListener("click", close);
  sheet.addEventListener("cancel", close);
  sheet.addEventListener("click", (event) => {
    // The backdrop is the dialog itself; a click on it lands outside the panel.
    if (event.target === sheet) close();
  });

  inputs.forEach((input) => {
    // The attribute is dropped up front: with the sheet in charge, a field
    // marked `capture` would still open the camera on the pass-through click.
    input.removeAttribute("capture");
    input.addEventListener("click", (event) => {
      if (passThrough || !takesPhotos(input)) return;
      if (typeof sheet.showModal !== "function") return;
      event.preventDefault();
      target = input;
      sheet.showModal();
    });
  });
})();
