// applier_runner — bun harness for the diff/apply property test.
//
// Reads newline-delimited JSON requests on stdin:
//   {"tag": "...", "initial": "<div>...</div>...", "patches": [...]}
// Stages each initial child list inside a fresh <domi-root> wrapper —
// the same shape the instance patches in production — applies the
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
import { readFile, unlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import * as readline from 'node:readline';
import { pathToFileURL } from 'node:url';

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

// client.js keeps applyPatch internal — its only export is run. Copy the
// source to a temp module that re-exports applyPatch, and import that, so
// the property test drives the production applier without widening the
// module's public surface. The copy differs from what ships only by the
// appended export line, so the code under test is exactly the production
// applier. (Importing the source as a data: URL would avoid the temp file,
// but bun rejects a data: specifier this long.)
const clientSrc = await readFile(new URL('../../../client.js', import.meta.url), 'utf8');
const underTest = join(tmpdir(), `domi-applier-${process.pid}.mjs`);
await writeFile(underTest, clientSrc + '\nexport { applyPatch };');
const { applyPatch } = await import(pathToFileURL(underTest).href);
await unlink(underTest);

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
    // The wrapper plays the instance's patch root: the initial child
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
