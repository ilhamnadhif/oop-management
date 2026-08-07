(() => {
  const dashboard = document.querySelector("[data-attendance-dashboard]");
  if (!dashboard) return;

  const video = document.querySelector("#cameraPreview");
  const canvas = document.createElement("canvas");
  const placeholder = document.querySelector("#cameraPlaceholder");
  const status = document.querySelector("#cameraStatus");
  const indicator = document.querySelector("#cameraIndicator");
  const action = document.querySelector("[data-attendance-action]");
  const csrfToken = dashboard.dataset.csrfToken;
  let stream;

  const setStatus = (message, isError = false) => {
    status.textContent = message;
    status.style.color = isError ? "#b33a3a" : "";
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
    if (action) action.disabled = false;
  };

  const getLocation = () => new Promise((resolve, reject) => {
    if (!navigator.geolocation) {
      reject(new Error("Browser ini tidak mendukung lokasi."));
      return;
    }
    setStatus("Mengambil lokasi…");
    navigator.geolocation.getCurrentPosition(resolve, () => reject(new Error("Akses lokasi diperlukan untuk absensi.")), {
      enableHighAccuracy: true,
      timeout: 10000,
      maximumAge: 0,
    });
  });

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
    if (!action) return;
    action.disabled = true;
    try {
      const [position, blob] = await Promise.all([getLocation(), capturePhoto()]);
      const form = new FormData();
      form.append("face_photo", blob, "selfie.jpg");
      form.append("latitude", String(position.coords.latitude));
      form.append("longitude", String(position.coords.longitude));
      if (Number.isFinite(position.coords.accuracy)) {
        form.append("accuracy", String(position.coords.accuracy));
      }
      const response = await fetch(action.dataset.attendanceAction, {
        method: "POST",
        body: form,
        headers: { "Accept": "application/json", "X-CSRF-Token": csrfToken },
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok || !payload.ok) {
        throw new Error(payload.error || "Absensi gagal disimpan.");
      }
      setStatus(payload.message || "Absensi berhasil disimpan.");
      window.location.reload();
    } catch (error) {
      setStatus(error.message || "Absensi gagal disimpan.", true);
      action.disabled = false;
    }
  };

  if (action) action.addEventListener("click", submitAttendance);
  startCamera().catch((error) => {
    indicator.textContent = "Kamera belum tersedia";
    setStatus(error.message || "Akses kamera diperlukan.", true);
  });

  window.addEventListener("beforeunload", () => {
    if (stream) stream.getTracks().forEach((track) => track.stop());
  });
})();
