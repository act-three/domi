// applier_runner — bun harness for the diff/apply property test.
//
// Reads newline-delimited JSON requests on stdin:
//   {"tag": "...", "initial": "<div>...</div>...", "patches": [...]}
// Stages each initial child list inside a fresh <domi-root> wrapper —
// the same shape the session patches in production — applies the
// patches to the wrapper, and writes back the resulting tree as
// serialized HTML, echoing the tag so the Go side can detect
// stdin/stdout desync:
//   {"tag": "...", "html": "<domi-root>...</domi-root>"}  — success
//   {"tag": "...", "err":  "..."}                         — failure (caught error)
//
// The Go side parses both this output and its own wrapped render(next)
// through the same HTML parser to compare — no canonicalization here.
//
// Reuses one JSDOM across requests; per-request state lives only in the
// fresh wrapper we recreate each iteration.

import { JSDOM } from 'jsdom';
import * as readline from 'node:readline';

// Set up jsdom globals BEFORE importing the applier: client.js has no
// import side effects, but applyPatch parses patch HTML through
// `document` at call time.
const dom = new JSDOM('<!doctype html><html><body></body></html>');
globalThis.window = dom.window;
globalThis.document = dom.window.document;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Node = dom.window.Node;
globalThis.NodeFilter = dom.window.NodeFilter;
globalThis.DocumentFragment = dom.window.DocumentFragment;

const { applyPatch } = await import('../../../client.js');

const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
for await (const line of rl) {
  if (!line) continue;
  // Parse the envelope first so we can always echo the tag, even on
  // failure — keeps the Go side's desync check honest even when bun
  // returns an error.
  let tag, resp;
  try {
    const req = JSON.parse(line);
    tag = req.tag;
    // The wrapper plays the session's patch root: the initial child
    // list becomes its children, patches address them from it, and the
    // wrapper itself is never a patch target.
    const root = document.createElement('domi-root');
    root.innerHTML = req.initial;
    for (const p of req.patches) applyPatch(root, p);
    resp = { tag, html: root.outerHTML };
  } catch (e) {
    resp = { tag, err: String(e && e.stack || e) };
  }
  process.stdout.write(JSON.stringify(resp) + '\n');
}
