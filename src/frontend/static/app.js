"use strict";

const one = (selector, root = document) => root.querySelector(selector);
const all = (selector, root = document) => [...root.querySelectorAll(selector)];

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
  window.addEventListener("keydown", (event) => { if (event.key === "Escape") setOpen(false); });
}

function setupPassword() {
  const password = one("[data-password]");
  one("[data-password-toggle]")?.addEventListener("click", (event) => {
    const show = password.type === "password";
    password.type = show ? "text" : "password";
    event.currentTarget.setAttribute("aria-label", show ? "Sembunyikan password" : "Tampilkan password");
  });
}

function setupSearch() {
  const input = one("[data-search-input]");
  const items = all("[data-filter-item]");
  input?.addEventListener("input", () => {
    const query = input.value.trim().toLowerCase();
    let visible = 0;
    items.forEach((item) => {
      item.hidden = !item.dataset.search.toLowerCase().includes(query);
      if (!item.hidden) visible++;
    });
    const empty = one("[data-filter-empty]");
    if (empty) empty.hidden = visible !== 0;
  });
  const tableSearch = one("[data-table-search]");
  tableSearch?.addEventListener("input", () => {
    let visible = 0;
    all("[data-table-name]").forEach((item) => {
      item.hidden = !item.dataset.tableName.toLowerCase().includes(tableSearch.value.trim().toLowerCase());
      if (!item.hidden) visible++;
    });
    const empty = one("[data-table-search-empty]");
    if (empty) empty.hidden = visible !== 0;
  });
}

function setupTabs() {
  const tabs = all("[data-tab]");
  const panels = all("[data-tab-panel]");
  const activate = (tab) => {
    tabs.forEach((candidate) => {
      const selected = candidate === tab;
      candidate.classList.toggle("is-active", selected);
      candidate.setAttribute("aria-selected", String(selected));
      candidate.tabIndex = selected ? 0 : -1;
    });
    panels.forEach((panel) => { panel.hidden = panel.id !== tab.dataset.tab; });
  };
  tabs.forEach((tab, index) => {
    tab.addEventListener("click", () => activate(tab));
    tab.addEventListener("keydown", (event) => {
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      event.preventDefault();
      const next = tabs[(index + (event.key === "ArrowRight" ? 1 : -1) + tabs.length) % tabs.length];
      activate(next); next.focus();
    });
  });
  const initial = tabs.find((tab) => tab.dataset.tab === new URLSearchParams(location.search).get("tab"));
  if (initial) activate(initial);
}

function setupMigration() {
  const form = one("[data-migration-form]");
  if (!form) return;
  const sql = one("[data-sql-editor]", form);
  const review = one("[data-review]", form);
  const execute = one("[data-execute]", form);
  const confirmed = one("[data-execute-confirm]", form);
  const nonAtomic = one("[data-non-atomic]", form);
  let stale = !review;
  const update = () => { if (execute) execute.disabled = stale || !confirmed.checked || (nonAtomic && !nonAtomic.checked); };
  sql.addEventListener("input", () => {
    if (review) {
      stale = true;
      one("[data-review-stale]").hidden = false;
      const analysis = one("[data-analysis]");
      if (analysis) analysis.hidden = true;
    }
    update();
  });
  confirmed?.addEventListener("change", update);
  nonAtomic?.addEventListener("change", update);
  form.addEventListener("submit", (event) => {
    if (event.submitter?.value === "execute" && (stale || !confirmed.checked || (nonAtomic && !nonAtomic.checked))) event.preventDefault();
  });
  update();
}

function setupVisualBuilder() {
  const form = one("[data-visual-form]");
  if (!form) return;
  const hidden = one("[data-operations]", form);
  const list = one("[data-operation-list]", form);
  const kind = one("[data-op-kind]", form);
  const schema = one("[data-op-schema]", form);
  const table = one("[data-op-table]", form);
  const error = one("[data-builder-error]", form);
  const addButton = one("[data-operation-add]", form);
  let operations = [];
  let editing = -1;
  try { const parsed = JSON.parse(hidden.value || "[]"); if (Array.isArray(parsed)) operations = parsed; } catch { error.textContent = "The previous change set could not be loaded."; error.hidden = false; }
  const button = (label, callback) => {
    const result = document.createElement("button");
    result.type = "button"; result.className = "button button--secondary button--small";
    result.textContent = label; result.addEventListener("click", callback); return result;
  };
  const field = (label, type = "text", value = "") => {
    const wrapper = document.createElement("label"); wrapper.className = "field";
    const title = document.createElement("span"); title.textContent = label;
    const input = document.createElement("input"); input.type = type;
    if (type === "checkbox") input.checked = Boolean(value); else input.value = value ?? "";
    wrapper.append(title, input); return { wrapper, input };
  };
  const addColumn = (column = {}, primary = false) => {
    const row = document.createElement("div"); row.className = "builder-row form-grid";
    const items = [field("Name", "text", column.Name), field("SQL type", "text", column.DataType), field("Default SQL expression", "text", column.Default), field("Nullable", "checkbox", column.Nullable ?? true), field("Primary key", "checkbox", primary)];
    items.forEach(({ wrapper }) => row.append(wrapper));
    row.append(button("Remove", () => row.remove())); one("[data-column-list]", form).append(row);
    row.read = () => ({ column: { Name: items[0].input.value.trim(), DataType: items[1].input.value.trim(), Default: items[2].input.value.trim() || null, Nullable: items[4].input.checked ? false : items[3].input.checked }, primary: items[4].input.checked });
  };
  const addPair = (target, name = "", value = "") => {
    const row = document.createElement("div"); row.className = "builder-row form-grid";
    const column = field("Column", "text", name);
    const input = field("Value", "text", typeof value === "object" && value !== null ? JSON.stringify(value) : value);
    const select = document.createElement("select"); select.setAttribute("aria-label", "Value type");
    ["text", "number", "boolean", "null"].forEach((type) => { const option = document.createElement("option"); option.value = type; option.textContent = type; select.append(option); });
    select.value = value === null ? "null" : typeof value === "boolean" ? "boolean" : typeof value === "number" ? "number" : "text";
    const typeField = document.createElement("label"); typeField.className = "field";
    const typeLabel = document.createElement("span"); typeLabel.textContent = "Value type"; typeField.append(typeLabel, select);
    row.append(column.wrapper, input.wrapper, typeField, button("Remove", () => row.remove()));
    one(`[data-pairs="${target}"]`, form).append(row);
    row.read = () => {
      const key = column.input.value.trim(); if (!key) throw new Error("Every value and filter needs a column name.");
      let result = input.input.value;
      if (select.value === "null") result = null;
      if (select.value === "number") {
        if (!/^-?(?:\d+)(?:\.\d+)?(?:[eE][+-]?\d+)?$/.test(result.trim()) || !Number.isFinite(Number(result))) throw new Error("Enter a valid finite number.");
        result = Number(result);
        if (Number.isInteger(result) && !Number.isSafeInteger(result)) throw new Error("This integer exceeds browser precision. Enter it as text to preserve the value.");
      }
      if (select.value === "boolean") { if (result !== "true" && result !== "false") throw new Error("Boolean values must be true or false."); result = result === "true"; }
      return [key, result];
    };
  };
  const pairs = (target) => {
    const result = Object.create(null);
    all(".builder-row", one(`[data-pairs="${target}"]`, form)).forEach((row) => {
      const [key, value] = row.read(); if (Object.hasOwn(result, key)) throw new Error("Duplicate column in row values or filter."); result[key] = value;
    });
    return result;
  };
  const refreshFields = () => {
    all("[data-op-fields]", form).forEach((panel) => { panel.hidden = !panel.dataset.opFields.split(" ").includes(kind.value); });
    all("[data-column-definition]", form).forEach((item) => { item.hidden = kind.value === "drop_column"; });
    all("[data-index-definition]", form).forEach((item) => { item.hidden = kind.value === "drop_index"; });
  };
  const edit = (index) => {
    const op = operations[index]; editing = index;
    kind.value = op.Kind; schema.value = op.Schema; table.value = op.Table;
    const column = op.Column || {};
    one("[data-column-name]", form).value = column.Name || "";
    one("[data-column-type]", form).value = column.DataType || "";
    one("[data-column-default]", form).value = column.Default || "";
    one("[data-column-nullable]", form).checked = Boolean(column.Nullable);
    const indexDefinition = op.Index || {};
    one("[data-index-name]", form).value = indexDefinition.Name || "";
    one("[data-index-columns]", form).value = (indexDefinition.Columns || []).map((item) => item.Name).join(", ");
    one("[data-index-unique]", form).checked = Boolean(indexDefinition.Unique);
    one("[data-column-list]", form).replaceChildren();
    const definition = op.Definition || {};
    const primary = (definition.Constraints || []).find((constraint) => constraint.Type === "p");
    (definition.Columns || []).forEach((item) => addColumn(item, primary?.Columns.includes(item.Name)));
    if (!definition.Columns?.length) addColumn();
    ["values", "where"].forEach((target) => {
      one(`[data-pairs="${target}"]`, form).replaceChildren();
      Object.entries(op[target === "values" ? "Values" : "Where"] || {}).forEach(([key, value]) => addPair(target, key, value));
    });
    addButton.textContent = "Save operation changes"; refreshFields(); kind.focus();
  };
  const render = () => {
    hidden.value = JSON.stringify(operations); list.replaceChildren();
    operations.forEach((op, index) => {
      const item = document.createElement("li");
      const title = document.createElement("strong"); title.textContent = `${index + 1}. ${op.Kind} · ${op.Schema}.${op.Table}`;
      const actions = document.createElement("div"); actions.className = "heading-actions";
      const up = button("Up", () => { [operations[index - 1], operations[index]] = [operations[index], operations[index - 1]]; editing = -1; addButton.textContent = "Add operation to change set"; render(); }); up.disabled = index === 0;
      const down = button("Down", () => { [operations[index + 1], operations[index]] = [operations[index], operations[index + 1]]; editing = -1; addButton.textContent = "Add operation to change set"; render(); }); down.disabled = index === operations.length - 1;
      actions.append(up, down, button("Edit", () => edit(index)), button("Remove", () => { operations.splice(index, 1); editing = -1; addButton.textContent = "Add operation to change set"; render(); }));
      item.append(title, actions); list.append(item);
    });
  };
  kind.addEventListener("change", refreshFields);
  one("[data-column-add]", form).addEventListener("click", () => addColumn());
  all("[data-pair-add]", form).forEach((trigger) => trigger.addEventListener("click", () => addPair(trigger.dataset.pairAdd)));
  addButton.addEventListener("click", () => {
    try {
      const op = { Kind: kind.value, Schema: schema.value.trim(), Table: table.value.trim() };
      if (!op.Schema || !op.Table) throw new Error("Enter the schema and table name.");
      if (op.Kind === "create_table") {
        const rows = all(".builder-row", one("[data-column-list]", form)).map((row) => row.read());
        if (!rows.length || rows.some((row) => !row.column.Name || !row.column.DataType)) throw new Error("Every table needs columns with names and SQL types.");
        op.Definition = { Columns: rows.map((row) => row.column), Constraints: [] };
        const primary = rows.filter((row) => row.primary).map((row) => row.column.Name);
        if (primary.length) op.Definition.Constraints.push({ Name: op.Table + "_pkey", Type: "p", Columns: primary });
      }
      if (op.Kind.endsWith("_column")) {
        op.Column = { Name: one("[data-column-name]", form).value.trim() };
        if (!op.Column.Name) throw new Error("Enter a column name.");
        if (op.Kind !== "drop_column") {
          op.Column.DataType = one("[data-column-type]", form).value.trim();
          op.Column.Nullable = one("[data-column-nullable]", form).checked;
          op.Column.Default = one("[data-column-default]", form).value.trim() || null;
          if (!op.Column.DataType) throw new Error("Enter the SQL type.");
        }
      }
      if (op.Kind.endsWith("_index")) {
        op.Index = { Name: one("[data-index-name]", form).value.trim(), Method: "btree" };
        if (!op.Index.Name) throw new Error("Enter an index name.");
        if (op.Kind === "add_index") {
          op.Index.Unique = one("[data-index-unique]", form).checked;
          op.Index.Columns = one("[data-index-columns]", form).value.split(",").map((name) => ({ Name: name.trim() }));
          if (op.Index.Columns.some((item) => !item.Name)) throw new Error("Enter index columns.");
        }
      }
      if (op.Kind === "insert_row" || op.Kind === "update_row") op.Values = pairs("values");
      if (op.Kind === "update_row") { op.Where = pairs("where"); if (!Object.keys(op.Where).length) throw new Error("Updates require at least one filter."); }
      if (editing < 0) operations.push(op); else operations[editing] = op;
      editing = -1; addButton.textContent = "Add operation to change set"; error.hidden = true; render();
    } catch (problem) { error.textContent = problem.message; error.hidden = false; }
  });
  form.addEventListener("submit", (event) => {
    if (!operations.length || editing >= 0) { event.preventDefault(); error.textContent = editing >= 0 ? "Save the edited operation before analysis." : "Add at least one operation before analysis."; error.hidden = false; }
    hidden.value = JSON.stringify(operations);
  });
  addColumn(); addPair("values"); addPair("where"); refreshFields(); render();
}

function setupSQLViewer() {
  const form = one("[data-viewer-form]");
  if (!form) return;
  const sql = one("[data-viewer-sql]", form);
  const results = one("[data-viewer-results]");
  sql.addEventListener("input", () => {
    const notice = one("[data-viewer-result-stale]", results || document);
    if (notice) notice.hidden = false;
  });
  sql.addEventListener("keydown", (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key === "Enter") {
      event.preventDefault();
      form.requestSubmit(one("[data-viewer-run]", form));
    }
  });
  const filters = one("[data-read-filters]", form);
  if (!filters) return;
  const columns = JSON.parse(filters.dataset.columns || "[]");
  const makeSelect = (name, label, options, value) => {
    const field = document.createElement("label"); field.className = "field";
    const caption = document.createElement("span"); caption.textContent = label;
    const select = document.createElement("select"); select.name = name;
    options.forEach(([key, text]) => { const option = document.createElement("option"); option.value = key; option.textContent = text; select.append(option); });
    select.value = value; field.append(caption, select);
    return { field, select };
  };
  const addFilter = (initial = {}) => {
    if (filters.children.length >= 20) return;
    const row = document.createElement("div"); row.className = "viewer-filter-row";
    const column = makeSelect("filter_column", "Column", [["", "Choose column"], ...columns.map((c) => [c.Name, c.Name])], initial.Column || "");
    const operator = makeSelect("filter_operator", "Operator", ["=", "!=", "<", "<=", ">", ">=", "LIKE", "NOT LIKE", "IS NULL", "IS NOT NULL"].map((op) => [op, op]), initial.Operator || "=");
    const type = makeSelect("filter_type", "Value type", [["text", "Text"], ["number", "Number"], ["boolean", "Boolean (true/false)"]], initial.Type || "text");
    const valueField = document.createElement("label"); valueField.className = "field";
    const caption = document.createElement("span"); caption.textContent = "Value";
    const input = document.createElement("input"); input.name = "filter_value"; input.value = initial.Value || ""; input.maxLength = 8192;
    valueField.append(caption, input);
    const remove = document.createElement("button"); remove.type = "button"; remove.className = "button button--secondary button--small"; remove.textContent = "Remove";
    remove.addEventListener("click", () => row.remove());
    const update = () => {
      const nullCheck = operator.select.value === "IS NULL" || operator.select.value === "IS NOT NULL";
      input.readOnly = nullCheck; type.select.setAttribute("aria-disabled", String(nullCheck));
      input.placeholder = nullCheck ? "No value needed" : type.select.value === "boolean" ? "true or false" : "Filter value";
    };
    operator.select.addEventListener("change", update); type.select.addEventListener("change", update);
    row.append(column.field, operator.field, type.field, valueField, remove); filters.append(row); update();
  };
  (JSON.parse(filters.dataset.filters || "null") || []).forEach(addFilter);
  one("[data-read-filter-add]", form)?.addEventListener("click", () => addFilter());
}

function setupWorkflowScroll() {
  const target = one("[data-auto-scroll]");
  if (!target) return;
  const reveal = () => window.requestAnimationFrame(() => {
    target.focus({ preventScroll: true });
    target.scrollIntoView({
      behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth",
      block: "start",
    });
  });
  if (document.readyState === "complete") reveal();
  else window.addEventListener("load", reveal, { once: true });
}

function showGroupFeedback(message, failed = false) {
  const region = one("[data-toast-region]");
  if (!region) return;
  const notice = document.createElement("div");
  notice.className = failed ? "toast toast--error" : "toast";
  notice.setAttribute("role", failed ? "alert" : "status");
  notice.textContent = message;
  region.replaceChildren(notice);
  window.setTimeout(() => notice.remove(), 6000);
}

function setupGroupEditors(cards = all("[data-group-card]")) {
  cards.forEach((card) => {
    const triggers = all("[data-group-edit]", card);
    const panels = all("[data-group-panel]", card);
    const show = (name) => {
      triggers.forEach((trigger) => trigger.setAttribute("aria-expanded", String(trigger.dataset.groupEdit === name)));
      panels.forEach((panel) => { panel.hidden = panel.dataset.groupPanel !== name; });
    };
    triggers.forEach((trigger) => trigger.addEventListener("click", () => {
      const name = trigger.getAttribute("aria-expanded") === "true" ? "" : trigger.dataset.groupEdit;
      show(name);
      if (name) one("h3", panels.find((panel) => panel.dataset.groupPanel === name))?.focus({ preventScroll: true });
    }));
    panels.forEach((panel) => one("[data-group-close]", panel).addEventListener("click", () => {
      show("");
      triggers.find((trigger) => trigger.dataset.groupEdit === panel.dataset.groupPanel)?.focus({ preventScroll: true });
    }));
    all("[data-group-save]", card).forEach((form) => form.addEventListener("submit", async (event) => {
      event.preventDefault();
      if (card.dataset.saving === "true") return;
      const body = new URLSearchParams(new FormData(form));
      const controls = all("button, input, select", card).filter((control) => !control.disabled);
      const editor = form.closest("[data-group-panel]").dataset.groupPanel;
      card.dataset.saving = "true";
      controls.forEach((control) => { control.disabled = true; });
      try {
        const response = await fetch(form.getAttribute("action"), { method: "POST", body, credentials: "same-origin" });
        if (new URL(response.url).pathname === "/login") {
          window.location.assign(response.url);
          return;
        }
        const page = new DOMParser().parseFromString(await response.text(), "text/html");
        if (!response.ok) {
          showGroupFeedback(one('[role="alert"]', page)?.textContent.trim() || "Changes could not be saved. Please try again.", true);
          return;
        }
        const replacement = page.getElementById(card.id);
        if (!replacement) throw new Error("Group response is unavailable.");
        const position = { left: window.scrollX, top: window.scrollY, behavior: "instant" };
        replacement.removeAttribute("data-auto-scroll");
        card.replaceWith(replacement);
        setupGroupEditors([replacement]);
        setupForms(replacement);
        one(`[data-group-panel="${editor}"] button[type="submit"]`, replacement)?.focus({ preventScroll: true });
        window.scrollTo(position);
        showGroupFeedback("Changes saved");
      } catch {
        showGroupFeedback("Could not confirm the save. Your input is still here; check the connection before trying again.", true);
      } finally {
        delete card.dataset.saving;
        controls.forEach((control) => { control.disabled = false; });
      }
    }));
  });
}

function setupForms(root = document) {
  all("[data-confirm]", root).forEach((form) => form.addEventListener("submit", (event) => { if (!window.confirm(form.dataset.confirm)) event.preventDefault(); }));
  all(".permission-select", root).forEach((select) => {
    const update = () => {
      select.classList.toggle("permission-select--view", select.value === "view_only");
      select.classList.toggle("permission-select--edit", select.value === "view_edit");
    };
    select.addEventListener("change", update); update();
  });
}

setupSidebar();
setupPassword();
setupSearch();
setupTabs();
setupMigration();
setupVisualBuilder();
setupSQLViewer();
setupGroupEditors();
setupForms();
setupWorkflowScroll();
