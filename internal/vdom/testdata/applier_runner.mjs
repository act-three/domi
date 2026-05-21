// applier_runner — bun harness for the diff/apply property test.
//
// Reads newline-delimited JSON requests on stdin:
//   {"tag": "...", "initial": "<html>...</html>", "patches": [...]}
// Spins each through the production applier inside jsdom and writes
// back the resulting tree as serialized HTML, echoing the tag so the
// Go side can detect stdin/stdout desync:
//   {"tag": "...", "html": "<...>"}   — success
//   {"tag": "...", "err":  "..."}     — failure (caught error)
//
// The Go side parses both this output and its own render(next) through
// the same HTML parser to compare — no canonicalization here.
//
// Reuses one JSDOM across requests; per-request state lives only in the
// fresh wrapper we recreate each iteration.

import { JSDOM } from 'jsdom';
import * as readline from 'node:readline';

// Set up jsdom globals BEFORE importing the applier, since client.js
// touches `document` at module load to decide whether to initSession.
// We deliberately don't include a #domi-root in the initial HTML so the
// auto-init no-ops; tests drive applyPatch directly.
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
    // Stage the initial tree inside a fresh wrapper so applyPatch's
    // root-replace branch (which walks node.parentNode) has somewhere
    // to land.
    const wrap = document.createElement('div');
    wrap.innerHTML = req.initial;
    let root = wrap.firstChild;
    for (const p of req.patches) root = applyPatch(root, p);
    resp = { tag, html: root.outerHTML };
  } catch (e) {
    resp = { tag, err: String(e && e.stack || e) };
  }
  process.stdout.write(JSON.stringify(resp) + '\n');
}
