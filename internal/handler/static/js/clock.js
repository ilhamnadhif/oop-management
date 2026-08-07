(() => {
  const clockNow = document.querySelector("#clockNow");
  if (!clockNow) return;

  const start = (clockNow.dataset.clockStart || "").split(":");
  if (start.length !== 2) return;

  // Anchor the display to the server clock, then let it advance locally. A
  // device with a skewed clock would otherwise show a time that disagrees with
  // what gets recorded.
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
})();
