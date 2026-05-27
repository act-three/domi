// domi client — ES module. Exports [run] (the session bootstrap),
// [applyPatch], and [eventPayload]. Importing the module has no side
// effects; callers invoke [run] explicitly. The framework's HTML
// renderer emits an inline module script that does exactly that.

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
    case 'set_title': {
      document.title = p.value;
      return root;
    }
    case 'set_attr': {
      // Coerce undefined → "" so name-only / empty-valued attrs land as
      // present-with-empty-string. The wire omits `value` when it's the
      // empty string (omitempty on the Go side), so a missing field here
      // means "set this attribute to empty", not "set it to undefined".
      walk(root, p.path).setAttribute(p.name, p.value ?? '');
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
    case 'reset': {
      // The root (and its delegated listeners) survives the rebuild, so
      // the session keeps working after a server-driven full resync.
      while (root.firstChild) root.removeChild(root.firstChild);
      delete root.__domiChildren;
      const frag = fragmentFromHTML(p.html);
      while (frag.firstChild) root.appendChild(frag.firstChild);
      return root;
    }
    case 'push_url': {
      history.pushState(null, '', p.value);
      return root;
    }
    case 'replace_url': {
      history.replaceState(null, '', p.value);
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

// run wires up the domi session on document.body and starts the SSE
// patch stream. Reads the session ID from body[data-domi-session=…]
// (which the server emits on initial render) and removes the
// attribute on its way out — so calling run() twice is a safe no-op,
// and calling it in a non-domi context (no body, no attribute) does
// nothing.
export function run() {
  if (typeof document === 'undefined' || !document.body) return;
  const container = document.body;
  const sessionId = container.dataset.domiSession;
  if (!sessionId) return;
  delete container.dataset.domiSession;
  // The container is document.body. The framework treats it as the
  // patch root. App content becomes its children, addressed by patches
  // at [0], [1], …
  let root = container;

  // Delegated listeners on the container: body stays put for the
  // session, so listeners don't have to migrate when patches mutate
  // its subtree.
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

  // Link interception for SPA navigation. Intercepts clicks on <a>
  // elements with same-origin hrefs and routes them through the
  // server's onURLRequest callback instead of navigating. Skips
  // modified clicks (ctrl/shift/alt/meta), non-left-button clicks,
  // links with target attributes, download links, and links where an
  // ancestor already has a data-msg-click handler (the app opted into
  // explicit handling).
  container.addEventListener('click', (e) => {
    if (e.button !== 0 || e.ctrlKey || e.shiftKey || e.altKey || e.metaKey) return;
    let a = e.target;
    while (a && a !== container) {
      if (a.tagName === 'A') break;
      a = a.parentNode;
    }
    if (!a || a.tagName !== 'A') return;

    // If a data-msg-click handler exists between the target and the
    // <a>, the app explicitly handles this click — skip interception.
    let el = e.target;
    while (el && el !== a.parentNode) {
      if (el.nodeType === 1 && el.dataset && el.dataset.msgClick) return;
      el = el.parentNode;
    }

    const href = a.getAttribute('href');
    if (!href) return;
    const target = a.getAttribute('target');
    if (target && target !== '_self') return;
    if (a.hasAttribute('download')) return;

    let url;
    try { url = new URL(href, location.href); } catch { return; }
    const internal = url.origin === location.origin;
    if (!internal) return; // let external links navigate normally

    e.preventDefault();
    const urlStr = url.pathname + url.search + url.hash;
    fetch(`/event/${encodeURIComponent(sessionId)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ type: 'urlRequest', url: urlStr, internal: true }),
    }).catch((err) => console.error('domi: urlRequest POST failed', err));
  });

  // Browser back/forward: popstate fires when the user navigates
  // through history. Send the new URL to the server so it can
  // dispatch onURLChange and update the view.
  window.addEventListener('popstate', () => {
    const url = location.pathname + location.search + location.hash;
    fetch(`/event/${encodeURIComponent(sessionId)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ type: 'urlChange', url }),
    }).catch((err) => console.error('domi: urlChange POST failed', err));
  });

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
  // A non-2xx response — the server's signal that the session is
  // permanently gone — moves the EventSource to CLOSED and fires
  // onerror. Transient network drops leave readyState at CONNECTING
  // and EventSource auto-reconnects, so checking for CLOSED is what
  // distinguishes "give up and reload" from "wait it out".
  sse.onerror = () => {
    if (sse.readyState === EventSource.CLOSED) location.reload();
  };
}

