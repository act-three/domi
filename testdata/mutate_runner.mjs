// mutate_runner — exercises client.js's optimistic move applier under
// jsdom. It imports the production applyMove (kept internal to client.js)
// by copying the source to a temp module that re-exports it, drives a few
// DOM scenarios, and exits non-zero with a message on the first failure.
// Run by TestClientApplyMove, which skips when bun is absent.

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
const tmp = join(tmpdir(), `domi-mutate-${process.pid}.mjs`);
await writeFile(tmp, src + '\nexport { applyMove, childMap, applyClientMutations };');
const { applyMove, childMap, applyClientMutations } = await import(pathToFileURL(tmp).href);
await unlink(tmp);

let failures = 0;
function check(cond, msg) {
  if (!cond) {
    console.error('FAIL:', msg);
    failures++;
  }
}

// keys returns the domi-key of each element child of parent.
const keys = (parent) => [...parent.children].map((c) => c.getAttribute('domi-key'));
// mapKeys returns the keys domi tracks for parent, sorted for comparison.
const mapKeys = (parent) => [...childMap(parent).keys()].sort();

function freshList(root, parentKeys) {
  while (root.firstChild) root.removeChild(root.firstChild);
  delete root.__domiChildren;
  const ul = document.createElement('ul');
  for (const k of parentKeys) {
    const li = document.createElement('li');
    li.setAttribute('domi-key', k);
    li.textContent = k;
    ul.appendChild(li);
  }
  root.appendChild(ul);
  return ul;
}

const root = document.createElement('domi-root');
document.body.appendChild(root);

// Reorder within one container, before an anchor.
{
  const ul = freshList(root, ['a', 'b', 'c']);
  const c = ul.children[2];
  const a = ul.children[0];
  const op = applyMove(root, c, a, null);
  check(keys(ul).join() === 'c,a,b', `reorder DOM = ${keys(ul)}, want c,a,b`);
  check(JSON.stringify(op) === JSON.stringify({ Op: 'move', From: [0, 'c'], To: [0, 'c'], Before: 'a' }), `reorder op = ${JSON.stringify(op)}`);
  check(mapKeys(ul).join() === 'a,b,c', `reorder childMap = ${mapKeys(ul)}`);
}

// Append within a container (no anchor → end).
{
  const ul = freshList(root, ['a', 'b', 'c']);
  const a = ul.children[0];
  const op = applyMove(root, a, null, ul);
  check(keys(ul).join() === 'b,c,a', `append DOM = ${keys(ul)}, want b,c,a`);
  check(op.Before === '' && op.To.join() === '0,a', `append op = ${JSON.stringify(op)}`);
}

// Move across containers, addressing each by its own path.
{
  while (root.firstChild) root.removeChild(root.firstChild);
  delete root.__domiChildren;
  const div = document.createElement('div');
  const mk = (parentKeys) => {
    const ul = document.createElement('ul');
    for (const k of parentKeys) {
      const li = document.createElement('li');
      li.setAttribute('domi-key', k);
      ul.appendChild(li);
    }
    return ul;
  };
  const ul1 = mk(['a', 'b', 'c']);
  const ul2 = mk(['x', 'y']);
  div.append(ul1, ul2);
  root.appendChild(div);

  const b = ul1.children[1];
  const y = ul2.children[1];
  const op = applyMove(root, b, y, null);
  check(keys(ul1).join() === 'a,c', `cross src = ${keys(ul1)}, want a,c`);
  check(keys(ul2).join() === 'x,b,y', `cross dst = ${keys(ul2)}, want x,b,y`);
  check(op.From.join() === '0,0,b' && op.To.join() === '0,1,b' && op.Before === 'y', `cross op = ${JSON.stringify(op)}`);
  check(!childMap(ul1).has('b'), 'cross: source map still holds b');
  check(childMap(ul2).get('b') === b, 'cross: destination map missing b');
}

// A key already present at the destination is re-keyed, carried in To's
// last step, and tracked under the new key so the maps stay consistent.
{
  while (root.firstChild) root.removeChild(root.firstChild);
  delete root.__domiChildren;
  const div = document.createElement('div');
  const mk = (parentKeys) => {
    const ul = document.createElement('ul');
    for (const k of parentKeys) {
      const li = document.createElement('li');
      li.setAttribute('domi-key', k);
      ul.appendChild(li);
    }
    return ul;
  };
  const ul1 = mk(['a', 'b']);
  const ul2 = mk(['a', 'c']);
  div.append(ul1, ul2);
  root.appendChild(div);

  const a = ul1.children[0];
  const c = ul2.children[1];
  const op = applyMove(root, a, c, null);
  const nk = op.To[op.To.length - 1];
  check(op.From[op.From.length - 1] === 'a' && nk !== '' && nk !== 'a', `collision op = ${JSON.stringify(op)}`);
  check(a.getAttribute('domi-key') === nk, 'collision: moved node not re-keyed in DOM');
  check(childMap(ul2).get(nk) === a, 'collision: destination map missing the re-keyed node');
  check(childMap(ul2).get('a') !== a, 'collision: re-keyed node clobbered the existing key');
}

// A container nested under a keyed ancestor is addressed by that
// ancestor's key, not its index — the episode-list of a keyed season.
{
  while (root.firstChild) root.removeChild(root.firstChild);
  delete root.__domiChildren;
  const seasons = document.createElement('ul'); // keyed seasons
  const mkSeason = (skey, items) => {
    const li = document.createElement('li');
    li.setAttribute('domi-key', skey);
    const eps = document.createElement('ul'); // positional child of the season
    for (const k of items) {
      const e = document.createElement('li');
      e.setAttribute('domi-key', k);
      eps.appendChild(e);
    }
    li.appendChild(eps);
    seasons.appendChild(li);
    return eps;
  };
  const s1 = mkSeason('s1', ['a', 'b']);
  const s2 = mkSeason('s2', ['x', 'y']);
  root.appendChild(seasons);

  const op = applyMove(root, s1.children[0], s2.children[0], null); // a → before x
  check(JSON.stringify(op.From) === JSON.stringify([0, 's1', 0, 'a']), `keyed-ancestor From = ${JSON.stringify(op.From)}, want [0,"s1",0,"a"]`);
  check(JSON.stringify(op.To) === JSON.stringify([0, 's2', 0, 'a']), `keyed-ancestor To = ${JSON.stringify(op.To)}, want [0,"s2",0,"a"]`);
  check(keys(s1).join() === 'b', `keyed-ancestor src = ${keys(s1)}, want b`);
  check(keys(s2).join() === 'a,x,y', `keyed-ancestor dst = ${keys(s2)}, want a,x,y`);
}

// A set with a malformed op is declined whole: applyClientMutations
// returns null and leaves the DOM untouched, even though an earlier op
// in the set was applicable — the caller falls back to a plain dispatch
// instead of committing a half-applied change.
{
  const ul = freshList(root, ['a', 'b', 'c']);
  const a = ul.children[0];
  const detached = document.createElement('li'); // never inserted: no parent
  detached.setAttribute('domi-key', 'z');
  const out = applyClientMutations(root, [
    { op: 'move', node: a, before: ul.children[2] }, // applicable on its own
    { op: 'move', node: detached, into: ul }, // malformed: node not connected
  ]);
  check(out === null, `malformed set should return null, got ${JSON.stringify(out)}`);
  check(keys(ul).join() === 'a,b,c', `declined set must not mutate the DOM, got ${keys(ul)}`);
}

if (failures) {
  console.error(`${failures} check(s) failed`);
  process.exit(1);
}
console.log('ok');
