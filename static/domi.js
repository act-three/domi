// domi client — applies server-emitted DOM patches and forwards events.
(() => {
  // The session-bearing container holds the user's top-level view as its
  // sole child. Server-side paths address that top-level view (path [] =
  // the View() return value), so `root` for walking must be the container's
  // first child, not the container itself.
  const container = document.getElementById('domi-root');
  if (!container) return;
  const sessionId = container.dataset.domiSession;
  let root = container.firstChild;

  // ---- event delegation ----
  // For each event we care about, install a single delegated listener on
  // the container (not root): the user's top-level node — which `root`
  // points at — can be replaced by a `replace` patch at path [], and we
  // don't want event delegation to die with it.
  const EVENTS = ['click', 'submit', 'input', 'change', 'keydown', 'keyup'];
  for (const ev of EVENTS) {
    container.addEventListener(ev, (e) => {
      let el = e.target;
      while (el && el !== container.parentNode) {
        if (el.nodeType === 1) {
          const key = datasetKeyFor(ev);
          const raw = el.dataset && el.dataset[key];
          if (raw) {
            if (ev === 'submit') e.preventDefault();
            // Attribute value is a comma-separated list of handler hashes;
            // the server splits and resolves each. We bundle the hashes
            // and a kitchen-sink event payload into one JSON envelope.
            postEnvelope(raw, eventPayload(e, el));
            return;
          }
        }
        el = el.parentNode;
      }
    });
  }

  function datasetKeyFor(event) {
    // data-msg-click → dataset.msgClick
    return 'msg' + event.charAt(0).toUpperCase() + event.slice(1);
  }

  // eventPayload builds the kitchen-sink record sent with every dispatch.
  // The server splices it into any Msg field tagged `domi:"event"`; Msgs
  // without that tag ignore it. Modifier/coordinate fields are omitted
  // when zero so the wire stays small for ordinary clicks.
  function eventPayload(e, el) {
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

  function postEnvelope(h, e) {
    fetch(`/event/${encodeURIComponent(sessionId)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ h, e }),
    }).catch((err) => console.error('domi: event POST failed', err));
  }

  // ---- SSE: receive patches ----
  const sse = new EventSource(`/sse/${encodeURIComponent(sessionId)}`);
  sse.addEventListener('patch', (ev) => {
    let patches;
    try {
      patches = JSON.parse(ev.data);
    } catch (e) {
      console.error('domi: bad patch JSON', ev.data, e);
      return;
    }
    for (const p of patches) applyPatch(p);
  });
  sse.onerror = (e) => console.warn('domi: SSE error', e);

  // ---- patch application ----
  function walk(path) {
    let node = root;
    for (const i of path) node = node.childNodes[i];
    return node;
  }

  // fragmentFromHTML parses an HTML string into a DocumentFragment using a
  // <template> element. Template parsing context is permissive — <tr>, <td>,
  // <option>, etc. parse without their natural ancestors.
  function fragmentFromHTML(html) {
    const tmpl = document.createElement('template');
    tmpl.innerHTML = html;
    return tmpl.content;
  }

  // childMap(parent) returns a Map<key, Element> for the parent's keyed
  // children, lazily building it on first access by scanning for
  // data-domi-key attributes on element children.
  function childMap(parent) {
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

  function applyPatch(p) {
    switch (p.op) {
      case 'replace': {
        const node = walk(p.path);
        const frag = fragmentFromHTML(p.html);
        const newNode = frag.firstChild;
        node.parentNode.replaceChild(newNode, node);
        // If we just replaced the user's top-level view, update root so
        // subsequent walks address the new tree.
        if (node === root) root = newNode;
        return;
      }
      case 'set_text': {
        walk(p.path).nodeValue = p.value;
        return;
      }
      case 'set_attr': {
        walk(p.path).setAttribute(p.name, p.value);
        return;
      }
      case 'remove_attr': {
        walk(p.path).removeAttribute(p.name);
        return;
      }
      case 'insert_child': {
        const parent = walk(p.path);
        const newNode = fragmentFromHTML(p.html).firstChild;
        if (p.key != null) {
          const map = childMap(parent);
          const anchor = p.before ? map.get(p.before) : null;
          parent.insertBefore(newNode, anchor || null);
          map.set(p.key, newNode);
        } else {
          parent.insertBefore(newNode, parent.childNodes[p.idx] || null);
        }
        return;
      }
      case 'remove_child': {
        const parent = walk(p.path);
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
        return;
      }
      case 'move_child': {
        const parent = walk(p.path);
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
        return;
      }
      default:
        console.warn('domi: unknown op', p);
    }
  }
})();
