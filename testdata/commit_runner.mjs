// commit_runner — exercises client.js's form-control commit logic
// under jsdom: commitOps (the ops reporting a control's committed
// state, and the attribute writes that keep the live DOM matching the
// server's reconstruction), revertControl (the local convergence for
// controls nobody listens to), and hasEditHandler (the test deciding
// between the two). It imports the production functions (kept internal
// to client.js) by copying the source to a temp module that re-exports
// them, and exits non-zero with a message on the first failure. Run by
// TestClientCommit, which skips when bun is absent.

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
const tmp = join(tmpdir(), `domi-commit-${process.pid}.mjs`);
await writeFile(tmp, src + '\nexport { commitOps, revertControl, hasEditHandler, editing };');
const { commitOps, revertControl, hasEditHandler, editing } = await import(pathToFileURL(tmp).href);
await unlink(tmp);

let failures = 0;
function check(cond, msg) {
  if (!cond) {
    console.error('FAIL:', msg);
    failures++;
  }
}

const root = document.createElement('domi-root');
document.body.appendChild(root);

// fresh resets the root to the given HTML and returns its first child.
function fresh(html) {
  root.innerHTML = html;
  return root.firstChild;
}

// json renders a mutation set as JSON for comparison.
const json = (muts) => JSON.stringify(muts);

// A text input's commit is a setvalue op naming the control, and the
// committed value lands in the live attribute alongside.
{
  const input = fresh('<input value="A">');
  input.value = 'B'; // user typed
  editing.add(input); // as the input listener would have
  const muts = commitOps(root, input);
  check(
    json(muts) === json([{ Op: 'setvalue', Path: [0], Value: 'B' }]),
    `input commit = ${json(muts)}`,
  );
  check(input.getAttribute('value') === 'B', 'input commit: attribute not written');
  check(!editing.has(input), 'commit must clear the editing mark');
}

// A control nested under a keyed ancestor is addressed through the key,
// so the path survives sibling reordering — same addressing as moves.
{
  fresh('<ul><li domi-key="row"><input></li></ul>');
  const input = root.querySelector('input');
  input.value = 'B';
  const muts = commitOps(root, input);
  check(
    json(muts) === json([{ Op: 'setvalue', Path: [0, 'row', 0], Value: 'B' }]),
    `keyed-path commit = ${json(muts)}`,
  );
}

// A textarea's commit is a settext op, and the committed value becomes
// the text content.
{
  const ta = fresh('<textarea>A</textarea>');
  ta.value = 'B'; // user typed
  const muts = commitOps(root, ta);
  check(
    json(muts) === json([{ Op: 'settext', Path: [0], Value: 'B' }]),
    `textarea commit = ${json(muts)}`,
  );
  check(ta.textContent === 'B', 'textarea commit: text content not written');
}

// A checkbox's commit records checkedness in the op and the attribute,
// in both directions.
{
  const box = fresh('<input type="checkbox" checked>');
  box.checked = false; // user unchecked
  let muts = commitOps(root, box);
  check(
    json(muts) === json([{ Op: 'setchecked', Path: [0], Checked: false }]),
    `uncheck commit = ${json(muts)}`,
  );
  check(!box.hasAttribute('checked'), 'uncheck commit: attribute not removed');
  box.checked = true; // user re-checked
  muts = commitOps(root, box);
  check(json(muts) === json([{ Op: 'setchecked', Path: [0], Checked: true }]), `check commit = ${json(muts)}`);
  check(box.getAttribute('checked') === '', 'check commit: attribute not written');
}

// A radio reports its whole name group: checking one silently unchecks
// the rest, so every member's checkedness is committed.
{
  fresh('<div><input type="radio" name="g" checked><input type="radio" name="g"></div>');
  const [a, b] = root.querySelectorAll('input');
  a.checked = false;
  b.checked = true; // user picked b
  const muts = commitOps(root, b);
  check(
    json(muts) ===
      json([
        { Op: 'setchecked', Path: [0, 0], Checked: false },
        { Op: 'setchecked', Path: [0, 1], Checked: true },
      ]),
    `radio group commit = ${json(muts)}`,
  );
  check(!a.hasAttribute('checked') && b.hasAttribute('checked'), 'radio group commit: attributes not moved');
}

// A select reports every option's selectedness — the deselected option
// has no event of its own, and multiple-selects need the full picture.
{
  const sel = fresh('<select><option selected>a</option><option>b</option></select>');
  const [a, b] = sel.children;
  b.selected = true; // user picked b; the browser deselects a
  const muts = commitOps(root, sel);
  check(
    json(muts) ===
      json([
        { Op: 'setselected', Path: [0, 0], Selected: false },
        { Op: 'setselected', Path: [0, 1], Selected: true },
      ]),
    `select commit = ${json(muts)}`,
  );
  check(!a.hasAttribute('selected') && b.hasAttribute('selected'), 'select commit: attributes not moved');
}

// File inputs and opaque controls commit nothing.
{
  const file = fresh('<input type="file">');
  check(json(commitOps(root, file)) === '[]', 'file input must not commit');
  fresh('<div domi-key="w" domi-opaque><input value="A"></div>');
  const opaque = root.querySelector('input');
  opaque.value = 'B';
  check(json(commitOps(root, opaque)) === '[]', 'opaque input must not commit');
  check(opaque.value === 'B' && opaque.getAttribute('value') === 'A', 'opaque input must be left alone');
  revertControl(root, opaque);
  check(opaque.value === 'B', 'opaque input must not revert');
}

// revertControl converges an unhandled control to its rendered state:
// the attributes and text the server last sent.
{
  const input = fresh('<input value="A">');
  input.value = 'B'; // user typed
  editing.add(input); // as the input listener would have
  revertControl(root, input);
  check(input.value === 'A', `input revert: value = ${JSON.stringify(input.value)}, want "A"`);
  check(!editing.has(input), 'revert must clear the editing mark');

  const ta = fresh('<textarea>A</textarea>');
  ta.value = 'B';
  revertControl(root, ta);
  check(ta.value === 'A', `textarea revert: value = ${JSON.stringify(ta.value)}, want "A"`);

  fresh('<div><input type="radio" name="g" checked><input type="radio" name="g"></div>');
  const [a, b] = root.querySelectorAll('input');
  a.checked = false;
  b.checked = true; // user picked b
  revertControl(root, b);
  check(a.checked && !b.checked, 'radio revert must restore the whole group');

  const sel = fresh('<select><option selected>a</option><option>b</option></select>');
  sel.children[1].selected = true;
  revertControl(root, sel);
  check(sel.children[0].selected && !sel.children[1].selected, 'select revert must restore options');
}

// hasEditHandler spans both commit events — a change-only handler
// covers keystrokes too — and nothing else: a click handler never
// carries a commit, so counting it would leave a declined toggle
// diverged.
{
  fresh('<div domi-msg-change="k:s"><input></div>');
  check(hasEditHandler(root, root.querySelector('input')), 'change handler on ancestor must count');
  fresh('<div><input></div>');
  check(!hasEditHandler(root, root.querySelector('input')), 'no handler must not count');
  fresh('<input type="checkbox" domi-msg-click="k:s">');
  check(!hasEditHandler(root, root.firstChild), 'a click handler must not count, even on a checkbox');
}

// An opaque option inside a plain select is neither reported nor
// touched — not by commit, not by revert. (Fresh trees for each half:
// a commit rewrites the rendered state, so reverting after it would
// correctly restore the committed values, proving nothing.)
{
  const html =
    '<select multiple><option selected>a</option><option domi-key="w" domi-opaque>b</option></select>';

  let sel = fresh(html);
  let [a, b] = sel.children;
  a.selected = false;
  b.selected = true; // user's edits touch both
  const muts = commitOps(root, sel);
  check(
    json(muts) === json([{ Op: 'setselected', Path: [0, 0], Selected: false }]),
    `opaque-option commit = ${json(muts)}`,
  );
  check(!b.hasAttribute('selected'), 'opaque option attributes must be left alone by commit');

  sel = fresh(html);
  [a, b] = sel.children;
  a.selected = false;
  b.selected = true;
  revertControl(root, sel);
  check(a.selected, 'plain option must revert to its rendered selectedness');
  check(b.selected, 'opaque option must not revert');
}

if (failures) {
  console.error(`${failures} check(s) failed`);
  process.exit(1);
}
console.log('ok');
