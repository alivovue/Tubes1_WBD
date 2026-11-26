import assert from "node:assert/strict";
import { test } from "node:test";
import { renderToStaticMarkup as render } from "react-dom/server";
import { Form } from "../../../shared/frontend/src/components/Form";
import { Table } from "../../../shared/frontend/src/components/Table";
import { Button, Notice } from "../../../shared/frontend/src/components/primitives";
import { initials, permission, pretty, stamp, url } from "../../../shared/frontend/src/utils/format";

test("URLs encode values, keep zero, and omit missing or empty parameters", () => {
  const result = new URL(url("/explorer", { db: 7, table: "a & b/東京", offset: 0, schema: undefined, sort: "" }), "http://localhost");
  assert.deepEqual(Object.fromEntries(result.searchParams), { db: "7", table: "a & b/東京", offset: "0" });
  assert.equal(url("/dashboard"), "/dashboard");
});
test("user-facing formats cover permissions, names, missing timestamps and JSON", () => {
  assert.deepEqual(["view_edit", "view_only", "", "unknown"].map(permission), ["View + Edit", "View Only", "No Access", "No Access"]);
  assert.equal(initials("  Ada   Lovelace Byron "), "AL");
  assert.equal(initials(""), "");
  assert.equal(stamp(""), "—");
  assert.equal(pretty({ active: true }), '{\n  "active": true\n}');
});
test("tables preserve escaping and make an empty result span all columns", () => {
  assert.match(render(<Table headers={["a", "b"]} rows={[]} empty="Nothing found" />), /colSpan="2">Nothing found/);
  const html = render(<Table headers={["Value"]} rows={[["<script>bad()</script>"]]} />);
  assert.match(html, /&lt;script&gt;/);
  assert.equal(html.includes("<script>"), false);
});
test("controls expose action intent, disabled state and accessible error roles", () => {
  const action = render(<Button action="run_query">Run</Button>);
  for (const attribute of ['type="submit"', 'name="action"', 'value="run_query"']) assert(action.includes(attribute));
  assert.match(render(<Button onClick={() => {}} disabled>Local</Button>), /type="button" disabled/);
  assert.match(render(<Notice error>Failed</Notice>), /role="alert"/);
});
async function submitForm(options: { busy?: boolean; confirm?: string; accepted?: boolean; success?: boolean } = {}) {
  let prevented = false, collected = 0, submitted = 0, succeeded = 0, confirmed = 0;
  const fields = { action: "save_group", name: "Editors" };
  const form = { id: "real-event-target" } as HTMLFormElement;
  const button = {} as HTMLElement;
  const previous = globalThis.window;
  globalThis.window = { confirm: () => { confirmed++; return options.accepted ?? true; } } as any;
  try {
    const element = Form({ busy: options.busy ?? false, confirm: options.confirm, children: null,
      collectFields: (target, submitter) => { collected++; assert.equal(target, form); assert.equal(submitter, button); return fields; },
      submit: async value => { submitted++; assert.equal(value, fields); return options.success ?? true; },
      onSuccess: () => { succeeded++; } });
    await element.props.onSubmit({ preventDefault: () => { prevented = true; }, currentTarget: form, nativeEvent: { submitter: button } });
    return { prevented, collected, submitted, succeeded, confirmed };
  } finally { if (previous === undefined) delete globalThis.window; else globalThis.window = previous; }
}
test("forms pass the clicked action and invoke success only after a successful mutation", async () => {
  assert.deepEqual(await submitForm(), { prevented: true, collected: 1, submitted: 1, succeeded: 1, confirmed: 0 });
  assert.equal((await submitForm({ success: false })).succeeded, 0);
});
test("busy forms and rejected confirmations never submit", async () => {
  assert.equal((await submitForm({ busy: true })).submitted, 0);
  const declined = await submitForm({ confirm: "Delete group?", accepted: false });
  assert.equal(declined.confirmed, 1); assert.equal(declined.collected, 0);
  assert.equal((await submitForm({ confirm: "Delete group?" })).submitted, 1);
  const html = render(<Form busy submit={async () => true} collectFields={() => ({})}>Content</Form>);
  assert.match(html, /fieldset[^>]+disabled/);
});
