(() => {
  const dashboard = document.querySelector("[data-attendance-dashboard]");
  if (!dashboard) return;

  const clockNow = document.querySelector("#clockNow");
  const actionHint = document.querySelector("#actionHint");
  const actionButtons = Array.from(document.querySelectorAll("[data-attendance-action]"));
  const mapContainer = document.querySelector("#attendanceMap");
  const mapPlaceholder = document.querySelector("#mapPlaceholder");
  const locationIndicator = document.querySelector("#locationIndicator");
  const locationChip = document.querySelector("#locationChip");
  const refreshLocation = document.querySelector("#refreshLocation");

  const modal = document.querySelector("#cameraModal");
  const video = document.querySelector("#cameraPreview");
  const placeholder = document.querySelector("#cameraPlaceholder");
  const status = document.querySelector("#cameraStatus");
  const indicator = document.querySelector("#cameraIndicator");
  const confirmCapture = document.querySelector("#confirmCapture");
  const cancelCapture = document.querySelector("#cancelCapture");

  const canvas = document.createElement("canvas");
  const csrfToken = dashboard.dataset.csrfToken;

  // The server decides which action is legal today. Remember that verdict so
  // the location check can only ever disable a button, never enable a forbidden
  // one.
  const allowedButtons = actionButtons.filter((button) => !button.disabled);
  actionButtons.forEach((button) => { button.disabled = true; });

  let stream;
  let lockedPosition = null;
  let pendingAction = null;
  let map;
  let marker;

  const setStatus = (message, isError = false) => {
    status.textContent = message;
    status.style.color = isError ? "#b33a3a" : "";
  };

  const setHint = (message, isError = false) => {
    if (!actionHint) return;
    actionHint.textContent = message;
    actionHint.style.color = isError ? "#b33a3a" : "";
  };

  const syncActionState = () => {
    allowedButtons.forEach((button) => { button.disabled = !lockedPosition; });
  };

  const startClock = () => {
    if (!clockNow) return;
    const start = (clockNow.dataset.clockStart || "").split(":");
    if (start.length !== 2) return;

    // Anchor the display to the server clock, then let it advance locally. A
    // device with a skewed clock would otherwise show a time that disagrees
    // with what gets recorded.
    const loadedAt = new Date();
    const serverAtLoad = new Date(loadedAt);
    serverAtLoad.setHours(Number(start[0]), Number(start[1]), 0, 0);
    const offset = serverAtLoad.getTime() - loadedAt.getTime();

    const pad = (value) => String(value).padStart(2, "0");
    const tick = () => {
      const current = new Date(Date.now() + offset);
      clockNow.textContent = `${pad(current.getHours())}:${pad(current.getMinutes())}`;
    };
    tick();
    window.setInterval(tick, 1000);
  };

  const getLocation = () => new Promise((resolve, reject) => {
    if (!navigator.geolocation) {
      reject(new Error("Browser ini tidak mendukung lokasi."));
      return;
    }
    navigator.geolocation.getCurrentPosition(resolve, () => reject(new Error("Akses lokasi diperlukan untuk absensi.")), {
      enableHighAccuracy: true,
      timeout: 10000,
      maximumAge: 0,
    });
  });

  const renderMap = (latitude, longitude) => {
    if (!mapContainer || typeof L === "undefined") return;
    if (mapPlaceholder) mapPlaceholder.hidden = true;

    if (!map) {
      map = L.map(mapContainer, { zoomControl: true, attributionControl: true }).setView([latitude, longitude], 17);
      L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
        maxZoom: 19,
        attribution: "&copy; OpenStreetMap",
      }).addTo(map);
      marker = L.marker([latitude, longitude]).addTo(map).bindPopup("Lokasi Anda Saat Ini").openPopup();
    } else {
      map.setView([latitude, longitude], map.getZoom());
      marker.setLatLng([latitude, longitude]);
    }

    // Leaflet caches the container size at init. The panel is still settling
    // when the first fix arrives, so re-measure on every render.
    map.invalidateSize();
  };

  const formatCoordinates = (coords) => `${coords.latitude.toFixed(4)}, ${coords.longitude.toFixed(4)}`;

  const lockLocation = async () => {
    if (refreshLocation) refreshLocation.disabled = true;
    if (locationIndicator) locationIndicator.textContent = "Mengambil lokasi…";
    try {
      const position = await getLocation();
      lockedPosition = position;
      renderMap(position.coords.latitude, position.coords.longitude);
      if (locationIndicator) {
        locationIndicator.textContent = "Lokasi siap";
        locationIndicator.classList.add("ready");
      }
      if (locationChip) {
        locationChip.hidden = false;
        locationChip.classList.add("locked");
        locationChip.textContent = `Terkunci (${formatCoordinates(position.coords)})`;
      }
      setHint(allowedButtons.length ? "Lokasi terkunci. Silakan absen." : "Absensi hari ini sudah lengkap.");
    } catch (error) {
      lockedPosition = null;
      if (locationIndicator) {
        locationIndicator.textContent = "Lokasi belum tersedia";
        locationIndicator.classList.remove("ready");
      }
      if (locationChip) {
        locationChip.hidden = false;
        locationChip.classList.remove("locked");
        locationChip.textContent = error.message || "Akses lokasi diperlukan.";
      }
      setHint(error.message || "Akses lokasi diperlukan.", true);
    } finally {
      if (refreshLocation) refreshLocation.disabled = false;
      syncActionState();
    }
  };

  const stopCamera = () => {
    if (!stream) return;
    stream.getTracks().forEach((track) => track.stop());
    stream = undefined;
    video.srcObject = null;
  };

  const closeModal = () => {
    stopCamera();
    pendingAction = null;
    if (modal) modal.hidden = true;
    if (placeholder) placeholder.hidden = false;
    if (confirmCapture) confirmCapture.disabled = true;
    if (indicator) {
      indicator.textContent = "Menyiapkan kamera…";
      indicator.classList.remove("ready");
    }
    syncActionState();
  };

  const startCamera = async () => {
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      throw new Error("Browser ini tidak mendukung akses kamera.");
    }
    stream = await navigator.mediaDevices.getUserMedia({
      video: { facingMode: "user" },
      audio: false,
    });
    video.srcObject = stream;
    await video.play();
    placeholder.hidden = true;
    indicator.textContent = "Kamera siap";
    indicator.classList.add("ready");
    if (confirmCapture) confirmCapture.disabled = false;
  };

  const openModal = async (actionUrl) => {
    if (!lockedPosition) {
      setHint("Lokasi belum terkunci. Tekan Perbarui lokasi.", true);
      return;
    }
    pendingAction = actionUrl;
    if (modal) modal.hidden = false;
    setStatus("Pastikan wajah terlihat jelas sebelum absen.");
    try {
      await startCamera();
    } catch (error) {
      indicator.textContent = "Kamera belum tersedia";
      setStatus(error.message || "Akses kamera diperlukan.", true);
    }
  };

  const capturePhoto = () => new Promise((resolve, reject) => {
    if (!video.videoWidth || !video.videoHeight) {
      reject(new Error("Kamera belum siap. Silakan coba lagi."));
      return;
    }
    const maxDimension = 640;
    const scale = Math.min(1, maxDimension / Math.max(video.videoWidth, video.videoHeight));
    canvas.width = Math.max(1, Math.round(video.videoWidth * scale));
    canvas.height = Math.max(1, Math.round(video.videoHeight * scale));
    const context = canvas.getContext("2d");
    context.save();
    context.scale(-1, 1);
    context.drawImage(video, -canvas.width, 0, canvas.width, canvas.height);
    context.restore();
    canvas.toBlob((blob) => {
      if (!blob) {
        reject(new Error("Gagal mengambil foto."));
        return;
      }
      resolve(blob);
    }, "image/jpeg", 0.72);
  });

  const submitAttendance = async () => {
    if (!pendingAction) return;
    if (confirmCapture) confirmCapture.disabled = true;
    try {
      // Submit the coordinate shown on the map, so what the user sees is what
      // gets recorded.
      if (!lockedPosition) {
        throw new Error("Lokasi belum terkunci. Tekan Perbarui lokasi.");
      }
      const position = lockedPosition;
      const blob = await capturePhoto();
      const form = new FormData();
      form.append("face_photo", blob, "selfie.jpg");
      form.append("latitude", String(position.coords.latitude));
      form.append("longitude", String(position.coords.longitude));
      if (Number.isFinite(position.coords.accuracy)) {
        form.append("accuracy", String(position.coords.accuracy));
      }
      const response = await fetch(pendingAction, {
        method: "POST",
        body: form,
        headers: { "Accept": "application/json", "X-CSRF-Token": csrfToken },
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok || !payload.ok) {
        throw new Error(payload.error || "Absensi gagal disimpan.");
      }
      setStatus(payload.message || "Absensi berhasil disimpan.");
      stopCamera();
      window.location.reload();
    } catch (error) {
      setStatus(error.message || "Absensi gagal disimpan.", true);
      if (confirmCapture) confirmCapture.disabled = false;
    }
  };

  actionButtons.forEach((button) => {
    button.addEventListener("click", () => openModal(button.dataset.attendanceAction));
  });
  if (confirmCapture) confirmCapture.addEventListener("click", submitAttendance);
  if (cancelCapture) cancelCapture.addEventListener("click", closeModal);
  if (refreshLocation) refreshLocation.addEventListener("click", lockLocation);
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && modal && !modal.hidden) closeModal();
  });

  startClock();
  syncActionState();
  lockLocation();

  window.addEventListener("beforeunload", stopCamera);
})();
