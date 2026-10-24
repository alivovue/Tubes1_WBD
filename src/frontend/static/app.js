"use strict";

const one = (selector, root = document) => root.querySelector(selector);
const all = (selector, root = document) => [...root.querySelectorAll(selector)];

function showToast(message) {
  const region = one("[data-toast-region]");
  if (!region) return;

  const toast = document.createElement("div");
  toast.className = "toast";
  toast.setAttribute("role", "status");
  toast.innerHTML = '<svg aria-hidden="true"><use href="#icon-check"></use></svg>';
  toast.append(document.createTextNode(message));
  region.append(toast);
  window.setTimeout(() => toast.remove(), 4200);
}

function setupSidebar() {
  const sidebar = one("[data-sidebar]");
  const toggle = one("[data-sidebar-toggle]");
  if (!sidebar || !toggle) return;

  const setOpen = (open) => {
    sidebar.classList.toggle("is-open", open);
    toggle.setAttribute("aria-expanded", String(open));
    document.body.style.overflow = open ? "hidden" : "";
  };

  toggle.addEventListener("click", () => setOpen(!sidebar.classList.contains("is-open")));
  all("[data-sidebar-close]").forEach((button) => button.addEventListener("click", () => setOpen(false)));
  window.addEventListener("keydown", (event) => {
    if (event.key === "Escape") setOpen(false);
  });
}

function setupLogin() {
  const form = one("[data-demo-login]");
  const password = one("[data-password]");
  const passwordToggle = one("[data-password-toggle]");

  passwordToggle?.addEventListener("click", () => {
    const show = password.type === "password";
    password.type = show ? "text" : "password";
    passwordToggle.setAttribute("aria-label", show ? "Sembunyikan password" : "Tampilkan password");
  });

  form?.addEventListener("submit", (event) => {
    event.preventDefault();
    const submit = one('[type="submit"]', form);
    submit.disabled = true;
    submit.textContent = "Signing in…";
    window.setTimeout(() => window.location.assign("/dashboard"), 450);
  });
}

function setupDatabaseFilter() {
  const container = one("[data-filter-items]");
  if (!container) return;

  const input = one("[data-search-input]");
  const buttons = all("[data-filter]", one("[data-filter-group]"));
  const items = all("[data-filter-item]", container);
  const empty = one("[data-filter-empty]", container);
  let environment = "all";

  const apply = () => {
    const query = input.value.trim().toLowerCase();
    let visible = 0;
    items.forEach((item) => {
      const matchesEnvironment = environment === "all" || item.dataset.environment === environment;
      const matchesQuery = !query || item.dataset.search.includes(query);
      const show = matchesEnvironment && matchesQuery;
      item.hidden = !show;
      if (show) visible += 1;
    });
    empty.hidden = visible !== 0;
  };

  input.addEventListener("input", apply);
  buttons.forEach((button) => {
    button.addEventListener("click", () => {
      environment = button.dataset.filter;
      buttons.forEach((candidate) => candidate.classList.toggle("is-active", candidate === button));
      apply();
    });
  });
}

function setupTabs() {
  const tabs = all("[data-tab]");
  if (!tabs.length) return;
  const panels = all("[data-tab-panel]");

  const activate = (tab) => {
    tabs.forEach((candidate) => {
      const selected = candidate === tab;
      candidate.classList.toggle("is-active", selected);
      candidate.setAttribute("aria-selected", String(selected));
      candidate.tabIndex = selected ? 0 : -1;
    });
    panels.forEach((panel) => {
      panel.hidden = panel.id !== tab.dataset.tab;
    });
  };

  tabs.forEach((tab, index) => {
    tab.addEventListener("click", () => activate(tab));
    tab.addEventListener("keydown", (event) => {
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      event.preventDefault();
      const direction = event.key === "ArrowRight" ? 1 : -1;
      const next = tabs[(index + direction + tabs.length) % tabs.length];
      activate(next);
      next.focus();
    });
  });
}

function setupMigration() {
  const analyze = one("[data-analyze]");
  const analysis = one("[data-analysis]");
  if (!analyze || !analysis) return;

  analyze.addEventListener("click", () => {
    analyze.disabled = true;
    analyze.textContent = "Analyzing…";
    window.setTimeout(() => {
      analysis.hidden = false;
      all("[data-step]").forEach((step, index) => {
        step.classList.toggle("is-complete", index === 0);
        step.classList.toggle("is-active", index === 1);
      });
      analyze.disabled = false;
      analyze.textContent = "Analyze again";
      analysis.scrollIntoView({ behavior: "smooth", block: "start" });
    }, 550);
  });

  const modal = one("#execute-modal");
  const confirm = one("[data-execute-confirm]", modal);
  const execute = one("[data-execute]", modal);
  const close = () => {
    modal.hidden = true;
    document.body.style.overflow = "";
  };

  all('[data-modal-open="execute-modal"]').forEach((button) => {
    button.addEventListener("click", () => {
      modal.hidden = false;
      document.body.style.overflow = "hidden";
      confirm.focus();
    });
  });
  all("[data-modal-close]", modal).forEach((button) => button.addEventListener("click", close));
  confirm.addEventListener("change", () => {
    execute.disabled = !confirm.checked;
  });
  execute.addEventListener("click", () => {
    close();
    all("[data-step]").forEach((step, index) => {
      step.classList.toggle("is-complete", index < 2);
      step.classList.toggle("is-active", index === 2);
    });
    showToast("Prototype frontend: migration siap diteruskan ke endpoint backend.");
  });
  window.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && !modal.hidden) close();
  });
}

function setupPermissionSelects() {
  all(".permission-select").forEach((select) => {
    const update = () => {
      select.classList.remove("permission-select--view", "permission-select--edit");
      if (select.value === "View Only") select.classList.add("permission-select--view");
      if (select.value === "View + Edit") select.classList.add("permission-select--edit");
    };
    select.addEventListener("change", update);
  });
}

function setupToastTriggers() {
  all("[data-toast]").forEach((trigger) => {
    trigger.addEventListener("click", () => showToast(trigger.dataset.toast));
  });
}

setupSidebar();
setupLogin();
setupDatabaseFilter();
setupTabs();
setupMigration();
setupPermissionSelects();
setupToastTriggers();
