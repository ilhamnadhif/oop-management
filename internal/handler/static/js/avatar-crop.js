(() => {
  // An avatar is shown in a circle everywhere in the app, so a portrait picked
  // whole gets its sides cut off by CSS and nobody gets to say where. This asks
  // instead: drag to place the face, pinch or slide to zoom, and what is inside
  // the circle is what gets stored - as a square, so the crop survives being
  // shown anywhere else.
  //
  // The file input is rewritten with the cropped image, so the form posts and
  // the server stores exactly what was previewed. A browser that cannot rebuild
  // a file list keeps the original picture: worse framing, not a broken upload.
  const fields = Array.from(document.querySelectorAll('input[name="foto_profil"]'));
  if (!fields.length) return;

  const canRewrite = typeof DataTransfer === "function" && typeof File === "function";
  const OUTPUT = 512;
  const VIEW = 288;

  const dialog = document.createElement("dialog");
  dialog.className = "modal avatar-crop";
  dialog.innerHTML = `
    <div class="modal-head">
      <div><h2 class="avatar-crop-title">Atur foto profil</h2></div>
    </div>
    <p class="hint">Geser untuk menempatkan wajah, lalu atur perbesaran. Bagian di dalam lingkaran yang disimpan.</p>
    <div class="avatar-crop-stage">
      <canvas class="avatar-crop-canvas" width="${VIEW}" height="${VIEW}"></canvas>
    </div>
    <label class="avatar-crop-zoom">
      <span class="field-label">Perbesaran</span>
      <input type="range" min="1" max="4" step="0.01" value="1" data-avatar-zoom>
    </label>
    <div class="avatar-crop-actions">
      <button class="button primary" type="button" data-avatar-save>Simpan potongan</button>
      <button class="button subtle" type="button" data-avatar-cancel>Batal</button>
    </div>`;
  document.body.appendChild(dialog);

  const canvas = dialog.querySelector(".avatar-crop-canvas");
  const context = canvas.getContext("2d");
  const zoom = dialog.querySelector("[data-avatar-zoom]");
  const save = dialog.querySelector("[data-avatar-save]");
  const cancel = dialog.querySelector("[data-avatar-cancel]");

  let field = null;
  let image = null;
  // baseScale makes the picture just cover the circle; zoom multiplies it.
  let baseScale = 1;
  let offsetX = 0;
  let offsetY = 0;

  const scale = () => baseScale * Number.parseFloat(zoom.value || "1");

  // Keep the picture covering the circle: panning must never expose the edge.
  const clamp = () => {
    const factor = scale();
    const width = image.naturalWidth * factor;
    const height = image.naturalHeight * factor;
    const limitX = Math.max(0, (width - VIEW) / 2);
    const limitY = Math.max(0, (height - VIEW) / 2);
    offsetX = Math.min(limitX, Math.max(-limitX, offsetX));
    offsetY = Math.min(limitY, Math.max(-limitY, offsetY));
  };

  const draw = () => {
    if (!image) return;
    clamp();
    const factor = scale();
    const width = image.naturalWidth * factor;
    const height = image.naturalHeight * factor;
    context.clearRect(0, 0, VIEW, VIEW);
    context.drawImage(image, (VIEW - width) / 2 + offsetX, (VIEW - height) / 2 + offsetY, width, height);
  };

  const close = () => {
    if (dialog.open) dialog.close();
    image = null;
    field = null;
  };

  // Cancelling clears the field: a picture nobody framed should not be posted
  // just because it was opened.
  const abandon = () => {
    if (field) field.value = "";
    close();
  };

  // The picture is read as a data URL rather than an object URL: the page's
  // content security policy allows img-src data: but not blob:, and a blob
  // would be blocked before it ever reached the canvas.
  const open = (input, file) => {
    field = input;
    image = null;
    const reader = new FileReader();
    reader.onerror = close;
    reader.onload = () => {
      show(String(reader.result || ""));
    };
    reader.readAsDataURL(file);
  };

  const show = (source) => {
    const loaded = new Image();
    loaded.onload = () => {
      image = loaded;
      baseScale = Math.max(VIEW / loaded.naturalWidth, VIEW / loaded.naturalHeight);
      zoom.value = "1";
      offsetX = 0;
      offsetY = 0;
      draw();
      // Picking a second file before the first was settled must not throw on an
      // already-open dialog.
      if (dialog.open) dialog.close();
      if (typeof dialog.showModal === "function") dialog.showModal();
    };
    loaded.onerror = () => {
      // Unreadable here means unreadable on the server too, but that is the
      // server's refusal to give, with a message this cannot improve on.
      close();
    };
    loaded.src = source;
  };

  // Dragging: pointer events cover mouse, pen and a single finger alike.
  let dragging = false;
  let lastX = 0;
  let lastY = 0;
  canvas.addEventListener("pointerdown", (event) => {
    if (!image) return;
    dragging = true;
    lastX = event.clientX;
    lastY = event.clientY;
    canvas.setPointerCapture(event.pointerId);
  });
  canvas.addEventListener("pointermove", (event) => {
    if (!dragging) return;
    offsetX += event.clientX - lastX;
    offsetY += event.clientY - lastY;
    lastX = event.clientX;
    lastY = event.clientY;
    draw();
  });
  const endDrag = () => { dragging = false; };
  canvas.addEventListener("pointerup", endDrag);
  canvas.addEventListener("pointercancel", endDrag);
  zoom.addEventListener("input", draw);

  const previewFor = (input) => {
    const form = input.closest("form");
    return form ? form.querySelector(".profile-photo-preview") : null;
  };

  save.addEventListener("click", () => {
    if (!image || !field) return close();
    const input = field;

    // Same framing as the preview, drawn at the size the avatar is stored in.
    const out = document.createElement("canvas");
    out.width = OUTPUT;
    out.height = OUTPUT;
    const ratio = OUTPUT / VIEW;
    const factor = scale() * ratio;
    const width = image.naturalWidth * factor;
    const height = image.naturalHeight * factor;
    out.getContext("2d").drawImage(
      image,
      (OUTPUT - width) / 2 + offsetX * ratio,
      (OUTPUT - height) / 2 + offsetY * ratio,
      width,
      height,
    );

    out.toBlob((blob) => {
      if (!blob || !canRewrite) {
        close();
        return;
      }
      const cropped = new File([blob], "foto-profil.jpg", { type: "image/jpeg" });
      const list = new DataTransfer();
      list.items.add(cropped);
      input.files = list.files;

      const preview = previewFor(input);
      if (preview) {
        const shown = new Image();
        shown.alt = "Pratinjau foto profil";
        shown.src = out.toDataURL("image/jpeg", 0.9);
        preview.textContent = "";
        preview.appendChild(shown);
      }
      close();
    }, "image/jpeg", 0.9);
  });

  cancel.addEventListener("click", abandon);
  dialog.addEventListener("cancel", (event) => {
    event.preventDefault();
    abandon();
  });

  fields.forEach((input) => {
    input.addEventListener("change", () => {
      const file = input.files && input.files[0];
      if (!file || !file.type.startsWith("image/")) return;
      open(input, file);
    });
  });
})();
