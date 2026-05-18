// domi client — ES module. Exports the patch applier and event-payload
// builder so test harnesses can drive them in jsdom/headless. Auto-runs
// initSession() at module load when a #domi-root container is present,
// which is the only mode the production server uses.

// fragmentFromHTML parses an HTML string into a DocumentFragment using a
// <template> element. Template parsing context is permissive — <tr>, <td>,
// <option>, etc. parse without their natural ancestors.
export function fragmentFromHTML(html) {
  const tmpl = document.createElement('template');
  tmpl.innerHTML = html;
  return tmpl.content;
}

// childMap(parent) returns a Map<key, Element> for the parent's keyed
// children, lazily building it on first access by scanning for
// data-domi-key attributes on element children.
export function childMap(parent) {
  let map = parent.__domiChildren;
  if (!map) {
    map = new Map();
    for (const child of parent.children) {
      const k = child.dataset && child.dataset.domiKey;
      if (k != null) map.set(k, child);
    }
    parent.__domiChildren = map;
  }
  return map;
}

function walk(root, path) {
  let node = root;
  for (const i of path) node = node.childNodes[i];
  return node;
}

// applyPatch applies a single patch to the tree rooted at `root` and
// returns the (possibly new) root. The root only changes when a `replace`
// patch at path [] swaps the top-level element — callers must thread the
// returned value back in for the next patch.
export function applyPatch(root, p) {
  switch (p.op) {
    case 'replace': {
      const node = walk(root, p.path);
      const newNode = fragmentFromHTML(p.html).firstChild;
      if (node.parentNode) node.parentNode.replaceChild(newNode, node);
      return node === root ? newNode : root;
    }
    case 'set_text': {
      walk(root, p.path).nodeValue = p.value;
      return root;
    }
    case 'set_attr': {
      walk(root, p.path).setAttribute(p.name, p.value);
      return root;
    }
    case 'remove_attr': {
      walk(root, p.path).removeAttribute(p.name);
      return root;
    }
    case 'insert_child': {
      const parent = walk(root, p.path);
      const newNode = fragmentFromHTML(p.html).firstChild;
      if (p.key != null) {
        const map = childMap(parent);
        const anchor = p.before ? map.get(p.before) : null;
        parent.insertBefore(newNode, anchor || null);
        map.set(p.key, newNode);
      } else {
        parent.insertBefore(newNode, parent.childNodes[p.idx] || null);
      }
      return root;
    }
    case 'remove_child': {
      const parent = walk(root, p.path);
      if (p.key != null) {
        const map = childMap(parent);
        const node = map.get(p.key);
        if (node) {
          parent.removeChild(node);
          map.delete(p.key);
        }
      } else {
        parent.removeChild(parent.childNodes[p.idx]);
      }
      return root;
    }
    case 'move_child': {
      const parent = walk(root, p.path);
      if (p.key != null) {
        const map = childMap(parent);
        const node = map.get(p.key);
        const anchor = p.before ? map.get(p.before) : null;
        if (node) parent.insertBefore(node, anchor || null);
      } else {
        const node = parent.childNodes[p.from];
        parent.removeChild(node);
        parent.insertBefore(node, parent.childNodes[p.to] || null);
      }
      return root;
    }
    default:
      console.warn('domi: unknown op', p);
      return root;
  }
}

// eventPayload builds the kitchen-sink record sent with every dispatch.
// The server splices it into any Msg field tagged `domi:"event"`; Msgs
// without that tag ignore it. Modifier/coordinate fields are omitted
// when zero so the wire stays small for ordinary clicks.
export function eventPayload(e, el) {
  const t = e.target;
  const target = { tag: (t.tagName || '').toLowerCase() };
  if (t.id) target.id = t.id;
  if (t.name) target.name = t.name;
  if (t.value !== undefined && t.value !== '') target.value = t.value;
  if (t.checked) target.checked = true;
  if (t.dataset) {
    const data = {};
    let any = false;
    for (const k in t.dataset) { data[k] = t.dataset[k]; any = true; }
    if (any) target.data = data;
  }
  const out = { type: e.type, target };
  if (e.key) out.key = e.key;
  if (e.code) out.code = e.code;
  if (e.button) out.button = e.button;
  if (e.clientX) out.clientX = e.clientX;
  if (e.clientY) out.clientY = e.clientY;
  if (e.ctrlKey) out.ctrl = true;
  if (e.shiftKey) out.shift = true;
  if (e.altKey) out.alt = true;
  if (e.metaKey) out.meta = true;
  // If the firing element is inside a <form>, attach the form's fields.
  // Last-value-wins matches the server's map[string]string shape.
  const form = el.closest && el.closest('form');
  if (form) {
    const fd = new FormData(form);
    const f = {};
    let any = false;
    for (const [k, v] of fd.entries()) {
      if (typeof v === 'string') { f[k] = v; any = true; }
    }
    if (any) out.form = f;
  }
  return out;
}

// ---- session initialization ----
//
// Runs at module load when a #domi-root container is present. Tests
// import the module to use applyPatch / eventPayload directly and don't
// set up that container, so this no-ops in test environments.

const EVENTS = ['click', 'submit', 'input', 'change', 'keydown', 'keyup'];

function datasetKeyFor(event) {
  // data-msg-click → dataset.msgClick
  return 'msg' + event.charAt(0).toUpperCase() + event.slice(1);
}

function postEnvelope(sessionId, h, e) {
  fetch(`/event/${encodeURIComponent(sessionId)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ h, e }),
  }).catch((err) => console.error('domi: event POST failed', err));
}

function initSession(container) {
  const sessionId = container.dataset.domiSession;
  let root = container.firstChild;

  // Delegated listeners on the container (not root): the user's top-level
  // node — which `root` points at — can be replaced by a `replace` patch
  // at path [], and we don't want event delegation to die with it.
  for (const ev of EVENTS) {
    container.addEventListener(ev, (e) => {
      let el = e.target;
      while (el && el !== container.parentNode) {
        if (el.nodeType === 1) {
          const key = datasetKeyFor(ev);
          const raw = el.dataset && el.dataset[key];
          if (raw) {
            if (ev === 'submit') e.preventDefault();
            postEnvelope(sessionId, raw, eventPayload(e, el));
            return;
          }
        }
        el = el.parentNode;
      }
    });
  }

  const sse = new EventSource(`/sse/${encodeURIComponent(sessionId)}`);
  sse.addEventListener('patch', (ev) => {
    let patches;
    try {
      patches = JSON.parse(ev.data);
    } catch (e) {
      console.error('domi: bad patch JSON', ev.data, e);
      return;
    }
    for (const p of patches) root = applyPatch(root, p);
  });
  sse.onerror = (e) => console.warn('domi: SSE error', e);
}

if (typeof document !== 'undefined') {
  const container = document.getElementById('domi-root');
  if (container) initSession(container);
}
