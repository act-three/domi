// patch_runner — exercises client.js's patch applier against form
// controls carrying user state under jsdom. Once a user has interacted
// with a control, its dirty flags stop the content attributes (and, for
// textarea, the text content) from reflecting into what's displayed, so
// the applier must sync the properties itself; these scenarios pin that
// behavior, including the focused-element guard. It imports the
// production applyPatch (kept internal to client.js) by copying the
// source to a temp module that re-exports it, and exits non-zero with a
// message on the first failure. Run by TestClientApplyPatchFormState,
// which skips when bun is absent.

import { JSDOM } from 'jsdom';
import { readFile, unlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const dom = new JSDOM('<!doctype html><html><body></body></html>');
globalThis.window = dom.window;
globalThis.document = dom.window.document;
globalThis.Node = dom.window.Node;

const src = await readFile(new URL('../client.js', import.meta.url), 'utf8');
const tmp = join(tmpdir(), `domi-patch-${process.pid}.mjs`);
await writeFile(tmp, src + '\nexport { applyPatch, editing };');
const { applyPatch, editing } = await import(pathToFileURL(tmp).href);
await unlink(tmp);

let failures = 0;
function check(cond, msg) {
  if (!cond) {
    console.error('FAIL:', msg);
    failures++;
  }
}

// In the document so elements are focusable; the focused-guard
// scenarios below depend on it.
const root = document.createElement('domi-root');
document.body.appendChild(root);

// fresh resets the root to the given HTML and returns its first child.
function fresh(html) {
  root.innerHTML = html;
  return root.firstChild;
}

// Dirty input: the user's typing sets the dirty value flag, after
// which SetAttr must update the display through the value property.
{
  const input = fresh('<input value="A">');
  input.value = 'B'; // user typed
  applyPatch(root, { Op: 'SetAttr', Path: [0], Name: 'value', Value: 'C' });
  check(input.value === 'C', `dirty input SetAttr: value = ${JSON.stringify(input.value)}, want "C"`);
  check(input.getAttribute('value') === 'C', 'dirty input SetAttr: attribute not written');
}

// The wire omits Value for the empty string; a dirty input must
// still blank out — the shape of the act3 stale-field report.
{
  const input = fresh('<input value="2022">');
  input.value = '2023'; // user typed
  applyPatch(root, { Op: 'SetAttr', Path: [0], Name: 'value' });
  check(input.value === '', `dirty input SetAttr empty: value = ${JSON.stringify(input.value)}, want ""`);
}

// RemoveAttr value on a dirty input clears the display.
{
  const input = fresh('<input value="A">');
  input.value = 'B'; // user typed
  applyPatch(root, { Op: 'RemoveAttr', Path: [0], Name: 'value' });
  check(input.value === '', `dirty input RemoveAttr: value = ${JSON.stringify(input.value)}, want ""`);
}

// A focused field marked editing holds typing that has not reached a
// commit point: the attribute lands but the property — and so the
// display — is left alone, so a server-initiated patch can't clobber
// in-flight typing. (The listener marks the control on every input
// event; the runner marks it by hand.)
{
  const input = fresh('<input value="A">');
  input.focus();
  input.value = 'B'; // user typed, still focused
  editing.add(input); // not yet committed
  applyPatch(root, { Op: 'SetAttr', Path: [0], Name: 'value', Value: 'C' });
  check(input.value === 'B', `focused editing input SetAttr: value = ${JSON.stringify(input.value)}, want user's "B"`);
  check(input.getAttribute('value') === 'C', 'focused editing input SetAttr: attribute not written');
  input.blur();
  editing.delete(input);
}

// A focused field not marked editing — its last edit committed, as
// after commitOps — takes a correction even while focused: having
// passed the frame base check, the patch is based on exactly what the
// field committed. The Enter-committed field the server normalizes
// must not stay stale until blur.
{
  const input = fresh('<input value="B">');
  input.focus();
  input.value = 'B'; // committed: the mark was cleared at the commit
  applyPatch(root, { Op: 'SetAttr', Path: [0], Name: 'value', Value: 'C' });
  check(input.value === 'C', `focused committed input SetAttr: value = ${JSON.stringify(input.value)}, want "C"`);
  input.blur();
}

// The guard trusts the editing mark, not DOM equality: a patch that
// coincidentally sets the attribute to the user's uncommitted text
// must not launder the next patch into an overwrite of in-flight
// typing.
{
  const input = fresh('<input value="A">');
  input.focus();
  input.value = 'B'; // user typed, still focused
  editing.add(input); // not yet committed
  applyPatch(root, { Op: 'SetAttr', Path: [0], Name: 'value', Value: 'B' }); // coincidence
  applyPatch(root, { Op: 'SetAttr', Path: [0], Name: 'value', Value: 'C' });
  check(input.value === 'B', `spoofed input SetAttr: value = ${JSON.stringify(input.value)}, want user's "B"`);
  check(input.getAttribute('value') === 'C', 'spoofed input SetAttr: attribute not written');
  input.blur();
  editing.delete(input);
}

// A file input is excluded from value sync: its value property is
// in filename mode, where a non-empty assignment throws — which
// would abort the rest of the patch frame — and an empty one
// discards the user's selected file. The attribute still lands.
{
  const input = fresh('<input type="file">');
  applyPatch(root, { Op: 'SetAttr', Path: [0], Name: 'value', Value: 'x' });
  check(input.value === '', `file input SetAttr: value = ${JSON.stringify(input.value)}, want ""`);
  check(input.getAttribute('value') === 'x', 'file input SetAttr: attribute not written');
}

// Dirty checkedness: the user's toggle detaches the checked
// attribute; SetAttr/RemoveAttr must sync the property both ways.
{
  const box = fresh('<input type="checkbox" checked>');
  box.checked = false; // user unchecked
  applyPatch(root, { Op: 'SetAttr', Path: [0], Name: 'checked' });
  check(box.checked === true, 'dirty checkbox SetAttr: not re-checked');
  box.checked = true; // user re-checked
  applyPatch(root, { Op: 'RemoveAttr', Path: [0], Name: 'checked' });
  check(box.checked === false, 'dirty checkbox RemoveAttr: not unchecked');
}

// Dirty selectedness: the user picked b; the server's render moves
// the selected attribute back to a, and the single-select invariant
// drops b when a's property is set.
{
  const select = fresh('<select><option selected>a</option><option>b</option></select>');
  const [a, b] = select.children;
  b.selected = true; // user picked b
  applyPatch(root, { Op: 'RemoveAttr', Path: [0, 1], Name: 'selected' });
  applyPatch(root, { Op: 'SetAttr', Path: [0, 0], Name: 'selected' });
  check(a.selected === true, 'dirty option SetAttr: a not selected');
  check(b.selected === false, 'dirty option: b still selected');
}

// Dirty textarea, SetText: the text content is only the default
// value, so the applier must copy it into the value property.
{
  const ta = fresh('<textarea>A</textarea>');
  ta.value = 'B'; // user typed
  applyPatch(root, { Op: 'SetText', Path: [0, 0], Value: 'C' });
  check(ta.value === 'C', `dirty textarea SetText: value = ${JSON.stringify(ta.value)}, want "C"`);
  check(ta.textContent === 'C', 'dirty textarea SetText: text content not written');
}

// A blanked-out textarea arrives as RemoveChild (empty text is
// canonicalized away on the Go side), a filled-in one as
// InsertChild; both must reach the display of a dirty textarea.
{
  const ta = fresh('<textarea>A</textarea>');
  ta.value = 'B'; // user typed
  applyPatch(root, { Op: 'RemoveChild', Path: [0], Index: 0 });
  check(ta.value === '', `dirty textarea RemoveChild: value = ${JSON.stringify(ta.value)}, want ""`);
  ta.value = 'D'; // user typed again
  applyPatch(root, { Op: 'InsertChild', Path: [0], Index: 0, HTML: 'E' });
  check(ta.value === 'E', `dirty textarea InsertChild: value = ${JSON.stringify(ta.value)}, want "E"`);
}

// The focused-textarea guard, same rationale as the focused input:
// typing marked editing is protected, an unmarked (committed) value
// takes the correction.
{
  const ta = fresh('<textarea>A</textarea>');
  ta.focus();
  ta.value = 'B'; // user typed, still focused
  editing.add(ta); // not yet committed
  applyPatch(root, { Op: 'SetText', Path: [0, 0], Value: 'C' });
  check(ta.value === 'B', `focused editing textarea SetText: value = ${JSON.stringify(ta.value)}, want user's "B"`);
  check(ta.textContent === 'C', 'focused editing textarea SetText: text content not written');
  ta.blur();
  editing.delete(ta);
}
{
  const ta = fresh('<textarea>B</textarea>');
  ta.focus();
  ta.value = 'B'; // committed: the mark was cleared at the commit
  applyPatch(root, { Op: 'SetText', Path: [0, 0], Value: 'C' });
  check(ta.value === 'C', `focused committed textarea SetText: value = ${JSON.stringify(ta.value)}, want "C"`);
  ta.blur();
}

if (failures) {
  console.error(`${failures} check(s) failed`);
  process.exit(1);
}
console.log('ok');
