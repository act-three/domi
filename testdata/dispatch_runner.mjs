// Browser coverage for clone stamps, scoped dispatch, and event versions.
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import * as Domi from '../domi.js';

const { window } = new JSDOM(
  '<body><domi-root prefix="/i">' +
    '<div id="outbox"><button id="source" domi-msg-click="old:ps">note</button></div>' +
    '<input id="edit" value="before" domi-msg-input="edit:ps" domi-msg-click="edit:ps">' +
    '<div id="display" domi-opaque></div><a id="link" href="/next">next</a>' +
  '</domi-root></body>', { url: 'https://app.example/' });
const { document } = window;
Object.assign(globalThis, { window, document, location: window.location, history: window.history });
const root = document.querySelector('domi-root');
root.setAttribute('path-sets', JSON.stringify({ ps: [['type']] }));
const find = id => root.querySelector('#' + id);
const stamp = el => Domi.clone(el).getAttribute('domi-event-tree-ver');
const posted = [];
globalThis.fetch = async (_url, init) => { posted.push(JSON.parse(init.body)); };
let stream;
globalThis.EventSource = class {
  constructor() { stream = this; }
  addEventListener(_type, fn) { this.update = fn; }
};
const push = (...Steps) => stream.update({ data: JSON.stringify({ Steps }) });
const patch = (Ver, Patches = []) => ({ Type: 'ApplyPatch', Ver, Patches });

function dispatch(el, handler, ver, handlerVer) {
  posted.length = 0;
  el.click();
  const expected = { Type: 'Dispatch', Handler: handler, Ver: ver, Event: { type: 'click' } };
  if (handlerVer !== undefined) expected.HandlerVer = handlerVer;
  assert.deepEqual(posted, [expected]);
}

Domi.run();
const initial = '11111111111111111111111111';
const source = find('source');
const copy = Domi.clone(source);
copy.id = 'copy';
assert.equal(stamp(source), initial);
assert.equal(source.hasAttribute('domi-event-tree-ver'), false);
assert.equal(Domi.clone(copy).outerHTML, copy.outerHTML, 'detached copies use cloneNode');
assert.equal(stamp(document.createElement('button')), null, 'external content is not stamped');
find('display').appendChild(copy);
dispatch(source, 'old', initial);

// Lifecycle callbacks cannot clone from the tree while it is being updated.
const blocked = [];
window.customElements.define('x-probe', class extends window.HTMLElement {
  connectedCallback() {
    try {
      Domi.clone(this);
      blocked.push(false);
    } catch {
      blocked.push(true);
    }
  }
});
push(patch('V1', [
  { Op: 'RemoveChild', Path: [0], Index: 0 },
  { Op: 'InsertChild', Path: [0], Index: 0, HTML: '<x-probe></x-probe>' },
]));
assert.equal(source.isConnected, false);
assert.deepEqual(blocked, [true]);
assert.equal(stamp(find('edit')), 'V1');
dispatch(copy, 'old', 'V1', initial);
stream.update({ data: JSON.stringify({ Base: 'stale', Steps: [patch('ignored')] }) });
assert.equal(stamp(find('edit')), 'V1');

// Scope selection follows the matched handler, including nested scopes.
find('display').insertAdjacentHTML('beforeend', `
  <div id="scope" domi-event-tree-ver="outer" domi-msg-click="one:ps,two:ps">
    <button id="child" domi-msg-click="child:ps"></button>
    <div domi-event-tree-ver="inner"><button id="nested" domi-msg-click="nested:ps"></button></div>
  </div>
  <div domi-msg-click="ancestor:ps"><span id="target" domi-event-tree-ver="unused"></span></div>`);
dispatch(find('scope'), 'one,two', 'V1', 'outer');
dispatch(find('child'), 'child', 'V1', 'outer');
dispatch(find('nested'), 'nested', 'V1', 'inner');
dispatch(find('target'), 'ancestor', 'V1');
assert.equal(stamp(find('child')), 'outer');
const scopedCopy = Domi.clone(find('scope'));
assert.equal(scopedCopy.querySelector('[domi-event-tree-ver]').getAttribute('domi-event-tree-ver'), 'inner');
document.body.setAttribute('domi-event-tree-ver', 'above-root');
dispatch(find('target'), 'ancestor', 'V1', 'above-root');
document.body.removeAttribute('domi-event-tree-ver');

// A clone made before the mutation request reaches the server must still
// name the server-provided table, while Ver advances for reconciliation.
const edit = find('edit');
posted.length = 0;
edit.value = 'after';
edit.dispatchEvent(new window.Event('input', { bubbles: true }));
assert.deepEqual(posted, [{
  Type: 'Dispatch', Handler: 'edit', Ver: 'V1', Event: { type: 'input' },
  Mutations: [{ Op: 'setvalue', Path: [1], Value: 'after' }],
}]);
assert.equal(stamp(edit), 'V1');
const afterMutation = Domi.clone(edit);
find('display').appendChild(afterMutation);
dispatch(afterMutation, 'edit', 'V1-mutated', 'V1');
dispatch(copy, 'old', 'V1-mutated', initial);
afterMutation.remove();

// Both navigation paths save the handler version separately from the
// mutation-derived snapshot version. Retained stamps survive restoration.
for (const [navigation, version] of [['server', 'V2'], ['preview', 'V3']]) {
  if (navigation === 'server') {
    push({ Type: 'PushURL', URL: '/two' }, patch(version));
  } else {
    find('link').dispatchEvent(new window.MouseEvent('mouseover', { bubbles: true }));
    push({ Type: 'SetPreview', URL: '/next', Dest: '/next', Ver: version, Patches: [] });
    assert.equal(stamp(find('edit')), 'V1', 'holding a preview does not change the version');
    find('link').click();
  }
  assert.equal(stamp(find('edit')), version, navigation);
  dispatch(find('copy'), 'old', version, initial);
  window.dispatchEvent(new window.PopStateEvent('popstate', { state: { domiSnapshot: 'V1-mutated' } }));
  assert.equal(stamp(find('edit')), 'V1', navigation + ' restoration');
  dispatch(find('copy'), 'old', 'V1-mutated', initial);
}
assert.ok(blocked.length > 1 && blocked.every(Boolean), 'cloning is blocked during restoration');

push(patch('V4', [{ Op: 'Reset', HTML: '<button id="fresh"></button>' }]));
assert.equal(stamp(find('fresh')), 'V4');
console.log('ok');
