// navigation_runner exercises client.js's anchor handling policy and URL
// normalization under jsdom. It imports the production helper by copying the
// source to a temporary module that re-exports it, and exits non-zero on the
// first failure. Run by TestClientAnchorNavigation, which skips without bun.

import { JSDOM } from 'jsdom';
import { readFile, unlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'https://app.example/current/page?old=1',
});
globalThis.window = dom.window;
globalThis.document = dom.window.document;
globalThis.location = dom.window.location;
globalThis.Node = dom.window.Node;

const src = await readFile(new URL('../client.js', import.meta.url), 'utf8');
const tmp = join(tmpdir(), `domi-navigation-${process.pid}.mjs`);
await writeFile(tmp, src + '\nexport { handledAnchorURL };');
const { handledAnchorURL } = await import(pathToFileURL(tmp).href);
await unlink(tmp);

let failures = 0;
function check(got, want, name) {
  if (got !== want) {
    console.error('FAIL:', `${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    failures++;
  }
}

function anchor(href, policy = null, attrs = {}) {
  const a = document.createElement('a');
  a.setAttribute('href', href);
  if (policy != null) a.setAttribute('domi-handle', policy);
  for (const [name, value] of Object.entries(attrs)) a.setAttribute(name, value);
  return a;
}

check(handledAnchorURL(anchor('/next?q=1#part')), '/next?q=1#part', 'default relative');
check(handledAnchorURL(anchor('child')), '/current/child', 'default path-relative');
check(
  handledAnchorURL(anchor('https://app.example/absolute?q=1#part')),
  '/absolute?q=1#part',
  'default same-origin absolute',
);
check(
  handledAnchorURL(anchor('https://app.example//admin?q=1#part')),
  '/admin?q=1#part',
  'same-origin leading slashes',
);
check(handledAnchorURL(anchor('https://elsewhere.example/x')), null, 'default cross-origin');
check(
  handledAnchorURL(anchor('https://elsewhere.example/x?q=1#part', 'yes')),
  'https://elsewhere.example/x?q=1#part',
  'yes cross-origin',
);
check(
  handledAnchorURL(anchor('https://elsewhere.example//admin', 'yes')),
  'https://elsewhere.example//admin',
  'cross-origin leading slashes',
);
check(handledAnchorURL(anchor('mailto:person@example.com', 'yes')), 'mailto:person@example.com', 'yes non-http');
check(handledAnchorURL(anchor('/next', 'no')), null, 'no same-origin');
check(handledAnchorURL(anchor('/next', 'invalid')), null, 'invalid policy');
check(handledAnchorURL(anchor('/next', 'yes', { target: '_blank' })), null, 'targeted');
check(handledAnchorURL(anchor('/next', 'yes', { download: '' })), null, 'download');
check(handledAnchorURL(anchor('http://[', 'yes')), null, 'malformed URL');

if (failures) process.exit(1);
console.log('ok');
