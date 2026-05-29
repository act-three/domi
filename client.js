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
// returns the (possibly new) root. The root only changes when a `Replace`
// patch at path [] swaps the top-level element — callers must thread the
// returned value back in for the next patch.
//
// Every op is a pure DOM mutation of `root` alone — no document-level or
// navigation side-effects — so it is safe to run against a detached
// clone, as preview snapshot construction does. Document-level changes
// travel as effects in an effect frame, run only against the live page.
export function applyPatch(root, p) {
  switch (p.Op) {
    case 'Replace': {
      const node = walk(root, p.Path);
      const newNode = fragmentFromHTML(p.HTML).firstChild;
      if (node.parentNode) node.parentNode.replaceChild(newNode, node);
      return node === root ? newNode : root;
    }
    case 'SetAttr': {
      // Coerce undefined → "" so name-only / empty-valued attrs land as
      // present-with-empty-string. The wire omits `Value` when it's the
      // empty string (omitempty on the Go side), so a missing field here
      // means "set this attribute to empty", not "set it to undefined".
      walk(root, p.Path).setAttribute(p.Name, p.Value ?? '');
      return root;
    }
    case 'RemoveAttr': {
      walk(root, p.Path).removeAttribute(p.Name);
      return root;
    }
    case 'InsertChild': {
      const parent = walk(root, p.Path);
      const newNode = fragmentFromHTML(p.HTML).firstChild;
      if (p.Key != null) {
        const map = childMap(parent);
        const anchor = p.Before ? map.get(p.Before) : null;
        parent.insertBefore(newNode, anchor || null);
        map.set(p.Key, newNode);
      } else {
        parent.insertBefore(newNode, parent.childNodes[p.Index] || null);
      }
      return root;
    }
    case 'RemoveChild': {
      const parent = walk(root, p.Path);
      if (p.Key != null) {
        const map = childMap(parent);
        const node = map.get(p.Key);
        if (node) {
          parent.removeChild(node);
          map.delete(p.Key);
        }
      } else {
        parent.removeChild(parent.childNodes[p.Index]);
      }
      return root;
    }
    case 'MoveChild': {
      const parent = walk(root, p.Path);
      if (p.Key != null) {
        const map = childMap(parent);
        const node = map.get(p.Key);
        const anchor = p.Before ? map.get(p.Before) : null;
        if (node) parent.insertBefore(node, anchor || null);
      } else {
        const node = parent.childNodes[p.From];
        parent.removeChild(node);
        parent.insertBefore(node, parent.childNodes[p.To] || null);
      }
      return root;
    }
    case 'Reset': {
      // The root (and its delegated listeners) survives the rebuild, so
      // the session keeps working after a server-driven full resync.
      while (root.firstChild) root.removeChild(root.firstChild);
      delete root.__domiChildren;
      const frag = fragmentFromHTML(p.HTML);
      while (frag.firstChild) root.appendChild(frag.firstChild);
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
    body: JSON.stringify({ Type: 'Dispatch', Handler: h, Event: e }),
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

  // Snapshot cache for instant back/forward. Maps snapshot ids
  // (server-generated, stored in history.state) to { frag, title }: a
  // DocumentFragment holding detached clones of the page's children,
  // plus its document title. A snapshot is the whole page as data —
  // restoring it sets both the DOM and the title.
  // `base` tracks which snapshot the client's DOM is built on top of
  // ("" initially). The server tags each SSE frame with its base;
  // the client drops frames whose base doesn't match.
  const snapshots = new Map();
  const SNAPSHOT_MAX = 30;
  let base = '';
  function cacheSnapshot(id, source, title) {
    if (!id) return;
    const frag = document.createDocumentFragment();
    for (const child of source.childNodes) frag.appendChild(child.cloneNode(true));
    snapshots.set(id, { frag, title });
    while (snapshots.size > SNAPSHOT_MAX) {
      snapshots.delete(snapshots.keys().next().value);
    }
  }
  function restoreSnapshot(id) {
    const cached = snapshots.get(id);
    if (!cached) return;
    while (root.firstChild) root.removeChild(root.firstChild);
    delete root.__domiChildren;
    // Clone-on-restore keeps the cache intact for future restores.
    const fresh = cached.frag.cloneNode(true);
    while (fresh.firstChild) root.appendChild(fresh.firstChild);
    document.title = cached.title ?? '';
    base = id;
  }

  // Prefetch cache for instant forward navigation. At most one entry:
  // hovering a different link evicts it. The entry holds the preview
  // snapshot id and the timestamp for dedup/expiry.
  //
  // prefetching holds the url of the most recent in-flight prefetch
  // (cleared when its preview SSE event lands). pendingClick holds
  // the url of a click that fired before its preview arrived; the SSE
  // preview handler navigates immediately when a matching preview
  // arrives.
  const PREVIEW_TTL = 5000; // ms
  let preview = null; // { url, previewId, at }
  let prefetching = ''; // url of an in-flight prefetch
  let pendingClick = ''; // url awaiting an in-flight preview

  // navigateToPreview applies a prefetched navigation: swap in the
  // preview snapshot, push history state, and notify the server with
  // a URLChange (not URLRequest — the navigation decision was made
  // in Preview at hover time). The server restores the preview
  // snapshot and dispatches onURLChange to update its state. The
  // outgoing snapshot was cached on both sides at preview-event
  // arrival time.
  function navigateToPreview(url, previewId) {
    restoreSnapshot(previewId);
    history.pushState(null, '', url);
    fetch(`/event/${encodeURIComponent(sessionId)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ Type: 'URLChange', URL: url, SnapshotID: previewId }),
    }).catch((err) => console.error('domi: urlChange POST failed', err));
  }

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
  // links with target attributes, download links, links carrying the
  // data-domi-bypass attribute (the app opted out of interception so
  // the browser navigates normally), and links where an ancestor
  // already has a data-msg-click handler (the app opted into explicit
  // handling).
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
    if (a.hasAttribute('data-domi-bypass')) return;

    let url;
    try { url = new URL(href, location.href); } catch { return; }
    const internal = url.origin === location.origin;
    if (!internal) return; // let external links navigate normally

    e.preventDefault();
    const urlStr = url.pathname + url.search + url.hash;
    // Fresh preview cached: navigate instantly.
    if (preview && preview.url === urlStr && Date.now() - preview.at < PREVIEW_TTL) {
      const { previewId } = preview;
      preview = null;
      navigateToPreview(urlStr, previewId);
      return;
    }
    // Preview in flight for this URL: wait. The SSE preview handler
    // navigates as soon as it arrives.
    if (prefetching === urlStr) {
      pendingClick = urlStr;
      return;
    }
    // No preview cached or in flight: normal navigation.
    fetch(`/event/${encodeURIComponent(sessionId)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ Type: 'URLRequest', URL: urlStr, Internal: true }),
    }).catch((err) => console.error('domi: urlRequest POST failed', err));
  });

  // Hover handler: send a prefetch when the cursor enters an internal
  // <a>. Dedup against the active preview; same URL within TTL is
  // reused. Hovering a different link replaces the preview (the client
  // holds at most one preloaded page).
  container.addEventListener('mouseover', (e) => {
    let a = e.target;
    while (a && a !== container) {
      if (a.tagName === 'A') break;
      a = a.parentNode;
    }
    if (!a || a.tagName !== 'A') return;
    const href = a.getAttribute('href');
    if (!href) return;
    const target = a.getAttribute('target');
    if (target && target !== '_self') return;
    if (a.hasAttribute('download')) return;
    if (a.hasAttribute('data-domi-bypass')) return;
    let url;
    try { url = new URL(href, location.href); } catch { return; }
    if (url.origin !== location.origin) return;
    const urlStr = url.pathname + url.search + url.hash;
    if (preview && preview.url === urlStr && Date.now() - preview.at < PREVIEW_TTL) return;
    if (prefetching === urlStr) return; // already in flight
    prefetching = urlStr;
    fetch(`/event/${encodeURIComponent(sessionId)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ Type: 'Prefetch', URL: urlStr }),
    }).catch((err) => console.error('domi: prefetch POST failed', err));
  });

  // Browser back/forward: popstate fires when the user navigates
  // through history. If a cached snapshot exists, restore the DOM
  // instantly and update the base so stale SSE frames are dropped;
  // then notify the server so it can send corrective patches for
  // any staleness.
  window.addEventListener('popstate', (e) => {
    const url = location.pathname + location.search + location.hash;
    const snapshotId = e.state && e.state.domiSnapshot;
    if (snapshotId) restoreSnapshot(snapshotId);
    fetch(`/event/${encodeURIComponent(sessionId)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ Type: 'URLChange', URL: url, SnapshotID: snapshotId || '' }),
    }).catch((err) => console.error('domi: urlChange POST failed', err));
  });

  const sse = new EventSource(`/sse/${encodeURIComponent(sessionId)}`);
  sse.addEventListener('effect', (ev) => {
    let f;
    try {
      f = JSON.parse(ev.data);
    } catch (e) {
      console.error('domi: bad effect JSON', ev.data, e);
      return;
    }
    // An ApplyPatch effect's DOM patches were diffed against a specific base;
    // if the client's tree has since moved to another snapshot the frame
    // is stale and must be dropped whole, since its other effects belong
    // to that same transition. A frame with no patches doesn't depend on
    // the DOM, so it runs regardless of base.
    if (f.Effects.some((e) => e.Type === 'ApplyPatch') && f.Base !== base) return;
    // Run the effects in the order the server chose. PushURL leads any
    // DOM patches so the outgoing page (current DOM + title) is
    // snapshotted before it changes; SetTitle trails them so that
    // snapshot still holds the old title. LoadURL abandons the document,
    // so it returns without touching the remaining effects.
    for (const eff of f.Effects) {
      switch (eff.Type) {
        case 'ApplyPatch':
          for (const p of eff.Patches) root = applyPatch(root, p);
          break;
        case 'SetTitle':
          document.title = eff.Title ?? '';
          break;
        case 'PushURL':
          cacheSnapshot(eff.ID, root, document.title);
          history.replaceState({ domiSnapshot: eff.ID }, '', location.href);
          history.pushState(null, '', eff.URL);
          break;
        case 'ReplaceURL':
          history.replaceState(history.state, '', eff.URL);
          break;
        case 'LoadURL':
          window.location.assign(eff.URL);
          return;
        default:
          console.warn('domi: unknown effect', eff);
      }
    }
  });
  // Preview frames: build two snapshots that match the server's at
  // prefetch time. The outgoing snapshot is root as it stands now
  // (which equals server's s.view at prefetch time — guaranteed by
  // the base check above plus SSE ordering). The preview snapshot is
  // built by cloning root and applying the prefetch's patches. If the
  // user has already clicked this link, navigate now.
  sse.addEventListener('preview', (ev) => {
    let f;
    try {
      f = JSON.parse(ev.data);
    } catch (e) {
      console.error('domi: bad preview JSON', ev.data, e);
      return;
    }
    if (f.Base !== base) return; // stale: live DOM has diverged
    // The outgoing snapshot is the live page as it stands now (its DOM
    // and current title). The target snapshot is built by applying the
    // preview's pure DOM patches to a clone and pairing it with the
    // title the server sent as data — nothing touches the live document.
    cacheSnapshot(f.Outgoing, root, document.title);
    const clone = root.cloneNode(true);
    let cur = clone;
    for (const p of f.Patches ?? []) cur = applyPatch(cur, p);
    cacheSnapshot(f.Target, cur, f.Title);
    preview = { url: f.URL, previewId: f.Target, at: Date.now() };
    if (prefetching === f.URL) prefetching = '';
    if (pendingClick === f.URL) {
      pendingClick = '';
      preview = null;
      navigateToPreview(f.URL, f.Target);
    }
  });
  // Deny: the app's Preview returned ok=false for this URL. Clear
  // the in-flight prefetch state and, if the user already clicked,
  // fall back to normal navigation so the app's onURLRequest gets
  // a chance to deny or redirect.
  sse.addEventListener('deny', (ev) => {
    let f;
    try {
      f = JSON.parse(ev.data);
    } catch (e) {
      console.error('domi: bad deny JSON', ev.data, e);
      return;
    }
    if (prefetching === f.URL) prefetching = '';
    if (pendingClick === f.URL) {
      pendingClick = '';
      fetch(`/event/${encodeURIComponent(sessionId)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ Type: 'URLRequest', URL: f.URL, Internal: true }),
      }).catch((err) => console.error('domi: urlRequest POST failed', err));
    }
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
