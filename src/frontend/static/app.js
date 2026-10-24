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
    showToast("Login belum tersedia: backend baru menyediakan skema users dan sessions, belum handler autentikasi.");
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

function setupExplorer() {
  const explorer = one(".explorer-shell");
  if (!explorer) return;

  const search = one("[data-table-search]", explorer);
  const tableItems = all("[data-table-name]", explorer);
  const searchEmpty = one("[data-table-search-empty]", explorer);
  const objectName = one("[data-object-name]", explorer);
  const tabs = one("[data-object-tabs]", explorer);
  const panels = all("[data-tab-panel]", explorer);
  const unavailable = one("[data-object-unavailable]", explorer);
  const unavailableTable = one("[data-unavailable-table]", explorer);

  all("[data-tree-toggle]", explorer).forEach((toggle) => {
    toggle.addEventListener("click", () => {
      const target = document.getElementById(toggle.getAttribute("aria-controls"));
      if (!target) return;
      const expanded = toggle.getAttribute("aria-expanded") === "true";
      toggle.setAttribute("aria-expanded", String(!expanded));
      target.hidden = expanded;
      one(".tree__chevron", toggle)?.classList.toggle("tree__chevron--open", !expanded);
    });
  });

  search?.addEventListener("input", () => {
    const query = search.value.trim().toLowerCase();
    let visible = 0;
    tableItems.forEach((item) => {
      const matches = item.dataset.tableName.toLowerCase().includes(query);
      item.hidden = !matches;
      if (matches) visible += 1;
    });
    searchEmpty.hidden = visible !== 0;

    const tableGroup = one("#public-tables", explorer);
    const publicToggle = one('[aria-controls="public-tables"]', explorer);
    if (query && tableGroup?.hidden) {
      tableGroup.hidden = false;
      publicToggle?.setAttribute("aria-expanded", "true");
      one(".tree__chevron", publicToggle)?.classList.add("tree__chevron--open");
    }
  });

  tableItems.forEach((item) => {
    item.addEventListener("click", () => {
      tableItems.forEach((candidate) => candidate.classList.toggle("is-active", candidate === item));
      objectName.textContent = item.dataset.tableName;

      const ready = item.dataset.tableReady === "true";
      tabs.hidden = !ready;
      unavailable.hidden = ready;
      unavailableTable.textContent = item.dataset.tableName;

      if (ready) {
        const activeTab = one("[data-tab].is-active", tabs) || one("[data-tab]", tabs);
        panels.forEach((panel) => {
          panel.hidden = panel.id !== activeTab.dataset.tab;
        });
      } else {
        panels.forEach((panel) => {
          panel.hidden = true;
        });
      }
    });
  });
}

function setupMigration() {
  const analyze = one("[data-analyze]");
  if (!analyze) return;

  analyze.addEventListener("click", () => {
    showToast("Analyze belum tersedia: backend belum memiliki endpoint validasi SQL atau migration analysis.");
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
setupExplorer();
setupMigration();
setupPermissionSelects();
setupToastTriggers();
