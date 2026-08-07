(() => {
  // Shared behaviour for the login and register forms. Everything is opt-in
  // through data attributes, so each page only gets what its markup asks for.

  // Safari in private mode throws on localStorage access, and dead storage must
  // never take a form down with it.
  const storage = (() => {
    try {
      const probe = "__opp__";
      window.localStorage.setItem(probe, probe);
      window.localStorage.removeItem(probe);
      return window.localStorage;
    } catch (error) {
      return null;
    }
  })();

  const setupPasswordToggles = () => {
    document.querySelectorAll("[data-password-toggle]").forEach((toggle) => {
      const field = document.getElementById(toggle.dataset.passwordToggle);
      if (!field) return;
      toggle.addEventListener("click", () => {
        const visible = field.type === "password";
        field.type = visible ? "text" : "password";
        toggle.setAttribute("aria-pressed", String(visible));
        const label = visible ? "Sembunyikan password" : "Tampilkan password";
        toggle.setAttribute("aria-label", label);
        toggle.setAttribute("title", label);
        const eye = toggle.querySelector(".icon-eye");
        const eyeOff = toggle.querySelector(".icon-eye-off");
        if (eye) eye.hidden = visible;
        if (eyeOff) eyeOff.hidden = !visible;
        field.focus();
      });
    });
  };

  const setupDigitsOnly = () => {
    document.querySelectorAll("[data-digits-only]").forEach((field) => {
      field.addEventListener("input", () => {
        const digits = field.value.replace(/\D/g, "");
        if (digits !== field.value) field.value = digits;
      });
    });
  };

  // Native constraint validation blocks submission on mismatch, so the loading
  // state never starts for a form the browser will reject.
  const setupConfirmFields = () => {
    document.querySelectorAll("[data-confirm-for]").forEach((confirmField) => {
      const source = document.getElementById(confirmField.dataset.confirmFor);
      if (!source) return;
      const message = confirmField.dataset.confirmMessage || "Konfirmasi tidak sama.";
      const check = () => {
        confirmField.setCustomValidity(confirmField.value === source.value ? "" : message);
      };
      confirmField.addEventListener("input", check);
      source.addEventListener("input", check);
      check();
    });
  };

  const setupLoadingForms = () => {
    document.querySelectorAll("[data-loading-form]").forEach((form) => {
      const button = form.querySelector("[data-submit-button]");
      if (!button) return;
      const label = button.querySelector("[data-submit-label]");
      const spinner = button.querySelector(".spinner");
      const idleText = label ? label.textContent : "";
      let submitting = false;

      const reset = () => {
        submitting = false;
        button.disabled = false;
        if (label) label.textContent = idleText;
        if (spinner) spinner.hidden = true;
      };

      form.addEventListener("submit", (event) => {
        if (submitting) {
          event.preventDefault();
          return;
        }
        submitting = true;
        if (label && button.dataset.loadingLabel) label.textContent = button.dataset.loadingLabel;
        if (spinner) spinner.hidden = false;
        // Disabling a submit button inside its own handler can cancel the
        // submission, so hand the browser a turn first.
        window.setTimeout(() => { button.disabled = true; }, 0);
      });

      // Returning through the back/forward cache restores the page with the
      // button still disabled and the loading label still showing.
      window.addEventListener("pageshow", (event) => {
        if (event.persisted) reset();
      });
    });
  };

  const setupRememberMe = () => {
    const identifier = document.querySelector("#identifier");
    const rememberMe = document.querySelector("#rememberMe");
    const form = document.querySelector("#loginForm");
    if (!identifier || !rememberMe || !form || !storage) return;

    const STORAGE_KEY = "opp.login.identifier";
    const saved = storage.getItem(STORAGE_KEY);
    if (saved) {
      rememberMe.checked = true;
      // A server-rendered value wins: it is what the user just typed on a
      // failed attempt, so overwriting it would undo their correction.
      if (!identifier.value) {
        identifier.value = saved;
        const password = document.querySelector("#password");
        if (password) password.focus();
      }
    }

    form.addEventListener("submit", () => {
      if (rememberMe.checked && identifier.value.trim()) {
        storage.setItem(STORAGE_KEY, identifier.value.trim());
      } else {
        storage.removeItem(STORAGE_KEY);
      }
    });
  };

  setupPasswordToggles();
  setupDigitsOnly();
  setupConfirmFields();
  setupLoadingForms();
  setupRememberMe();
})();
