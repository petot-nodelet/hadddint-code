(() => {
  const form = document.querySelector("[data-builder-form]");
  if (!form) return;

  const status = form.querySelector("[data-save-status]");
  const restoreButton = form.querySelector("[data-restore-draft]");
  const storageKey = form.dataset.storageKey;
  const versionInput = form.elements.namedItem("draft_version");
  let timer;
  let saving = false;
  let dirty = false;
  let blocked = false;
  let inFlight;

  const persistRecovery = () => {
    try {
      const values = {};
      for (const [name, value] of new FormData(form)) values[name] = value;
      sessionStorage.setItem(storageKey, JSON.stringify(values));
      return true;
    } catch (error) {
      console.error("Could not preserve local invitation draft", error);
      return false;
    }
  };

  const updateItems = () => {
    const container = form.querySelector("[data-builder-items]");
    if (!container) return;
    const items = [...container.querySelectorAll(".builder-item")];
    const limit = Number(container.dataset.itemLimit);
    const count = form.elements.namedItem("item_count");
    const add = container.querySelector("[data-item-add]");
    count.value = items.length;
    add.disabled = items.length >= limit;
    container.querySelector("[data-item-quota]").textContent = `${items.length} / ${limit} item`;
    items.forEach((item, index) => {
      item.querySelector("legend").textContent = `Item ${index + 1}`;
      item.querySelectorAll("[name]").forEach((input) => {
        input.name = input.name.replace(/item_\d+_/, `item_${index}_`);
      });
      item.querySelector("[data-item-up]").disabled = index === 0;
      item.querySelector("[data-item-down]").disabled = index === items.length - 1;
    });
  };

  const save = () => {
    if (!dirty || blocked) return inFlight || Promise.resolve();
    if (saving) return inFlight;
    saving = true;
    dirty = false;
    status.textContent = "Menyimpan…";
    let failed = false;
    inFlight = (async () => {
      try {
        const response = await fetch(form.action, {
          method: "POST",
          body: new URLSearchParams(new FormData(form)),
          headers: {
            "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
            "X-CSRF-Token": form.elements.namedItem("csrf_token").value,
            "X-Builder-Autosave": "true",
          },
          credentials: "same-origin",
        });
        if (!response.ok) {
          if (response.status === 409) {
            blocked = true;
            const preserved = persistRecovery();
            status.textContent = preserved
              ? "Konflik versi: perubahan lokal disimpan di sesi ini. Muat ulang untuk melihat versi terbaru, lalu pulihkan perubahan lokal bila diperlukan."
              : "Konflik versi dan penyimpanan lokal browser tidak tersedia. Salin perubahan, muat ulang, lalu masukkan kembali sebelum menyimpan.";
          } else {
            throw new Error(`HTTP ${response.status}`);
          }
        } else {
          const result = await response.json();
          versionInput.value = String(result.version);
          form.dataset.draftVersion = String(result.version);
          sessionStorage.removeItem(storageKey);
          status.textContent = "Tersimpan.";
        }
      } catch (error) {
        failed = true;
        dirty = true;
        const preserved = persistRecovery();
        status.textContent = preserved
          ? "Gagal menyimpan otomatis. Perubahan lokal tetap tersedia; gunakan Simpan modul untuk mencoba lagi."
          : "Gagal menyimpan otomatis dan penyimpanan lokal browser tidak tersedia. Gunakan Simpan modul sebelum navigasi.";
        console.error("Invitation draft autosave failed", error);
      } finally {
        saving = false;
        inFlight = undefined;
        if (dirty && !blocked && !failed) save();
      }
    })();
    return inFlight;
  };

  const markDirty = () => {
    dirty = true;
    status.textContent = "Ada perubahan…";
    clearTimeout(timer);
    timer = setTimeout(save, 700);
  };

  form.addEventListener("input", markDirty);
  form.addEventListener("change", markDirty);
  form.addEventListener("submit", (event) => {
    if (!dirty && !saving) return;
    event.preventDefault();
    clearTimeout(timer);
    const submitAfterSave = () => save().then(() => {
      if (blocked) return;
      if ((dirty || saving) && !status.textContent.startsWith("Gagal")) {
        submitAfterSave();
        return;
      }
      form.submit();
    });
    submitAfterSave();
  });
  form.addEventListener("click", (event) => {
    const button = event.target.closest("button");
    if (!button) return;
    const container = form.querySelector("[data-builder-items]");
    if (!container) return;
    const item = button.closest(".builder-item");
    if (button.matches("[data-item-add]")) {
      const template = form.querySelector("[data-item-template]");
      if (container.querySelectorAll(".builder-item").length >= Number(container.dataset.itemLimit)) return;
      const newItem = template.content.firstElementChild.cloneNode(true);
      newItem.querySelectorAll("input, textarea, select").forEach((input) => {
        if (input instanceof HTMLSelectElement) input.selectedIndex = 0;
        else input.value = "";
      });
      container.insertBefore(newItem, button);
      updateItems();
      markDirty();
    } else if (button.matches("[data-item-remove]")) {
      item.remove();
      updateItems();
      markDirty();
    } else if (button.matches("[data-item-up]") && item.previousElementSibling && item.previousElementSibling.matches(".builder-item")) {
      container.insertBefore(item, item.previousElementSibling);
      updateItems();
      markDirty();
    } else if (button.matches("[data-item-down]") && item.nextElementSibling && item.nextElementSibling.matches(".builder-item")) {
      container.insertBefore(item.nextElementSibling, item);
      updateItems();
      markDirty();
    }
  });

  restoreButton.addEventListener("click", () => {
    const saved = sessionStorage.getItem(storageKey);
    if (!saved) return;
    let values;
    try {
      values = JSON.parse(saved);
    } catch (error) {
      sessionStorage.removeItem(storageKey);
      status.textContent = "Perubahan lokal tidak dapat dibaca.";
      console.error("Could not read local invitation draft", error);
      return;
    }
    const container = form.querySelector("[data-builder-items]");
    if (container && Number.isInteger(Number(values.item_count))) {
      const desiredCount = Math.min(Number(values.item_count), Number(container.dataset.itemLimit));
      const template = form.querySelector("[data-item-template]");
      while (container.querySelectorAll(".builder-item").length < desiredCount) {
        container.insertBefore(template.content.firstElementChild.cloneNode(true), container.querySelector("[data-item-add]"));
      }
      while (container.querySelectorAll(".builder-item").length > desiredCount) {
        container.querySelector(".builder-item:last-of-type").remove();
      }
    }
    updateItems();
    for (const [name, value] of Object.entries(values)) {
      const control = form.elements.namedItem(name);
      if (control && "value" in control) control.value = value;
    }
    sessionStorage.removeItem(storageKey);
    restoreButton.classList.add("hidden");
    blocked = false;
    markDirty();
  });

  if (sessionStorage.getItem(storageKey)) restoreButton.classList.remove("hidden");
  updateItems();

  document.addEventListener("click", (event) => {
    const link = event.target.closest("a[href]");
    if (!link || !dirty || blocked || link.target || event.defaultPrevented) return;
    const destination = new URL(link.href, location.href);
    if (destination.origin !== location.origin) return;
    event.preventDefault();
    clearTimeout(timer);
    const navigateAfterSave = () => save().then(() => {
      if (blocked || status.textContent.startsWith("Gagal")) return;
      if (dirty || saving) {
        navigateAfterSave();
        return;
      }
      location.assign(destination.href);
    });
    navigateAfterSave();
  });

  window.addEventListener("beforeunload", (event) => {
    if (dirty || saving) {
      persistRecovery();
      event.preventDefault();
      event.returnValue = "";
    }
  });
})();
