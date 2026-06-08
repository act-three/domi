// domi client — ES module. Exports [run] (the session bootstrap),
// [applyPatch], and [getFields]. Importing the module has no side
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
    case 'SetText': {
      // Write the new text straight to nodeValue: it takes a raw string
      // with no HTML parsing, so the unescaped Value lands verbatim, and
      // the text node keeps its identity — a selection anchored in it
      // survives. Coerce undefined → "": when a node's text goes empty
      // (an interpolated value blanks out) the Go side drops Value via
      // omitempty, and a missing field here means "clear it".
      walk(root, p.Path).nodeValue = p.Value ?? '';
      return root;
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

// getFields reads a list of field paths out of e and returns an object
// with just the resulting values.
// As a special case, if the first element of a path is "currentTarget",
// getFields reads the rest of the path from el instead of e.
// This lets the caller provide appropriate data
// for the relevant element rather than the global delegated handler.
//
// Only values that can be represented in JSON are included,
// others are skipped.
export function getFields(e, el, paths) {
  const out = {};
  for (const path of paths) {
    let node = path[0] === 'currentTarget' ? el : e;
    let i = path[0] === 'currentTarget' ? 1 : 0;
    for (; i < path.length && node != null; i++) node = node[path[i]];
    const t = typeof node;
    if (t !== 'string' && t !== 'number' && t !== 'boolean') continue;
    let o = out;
    for (let j = 0; j < path.length - 1; j++) o = o[path[j]] ??= {};
    o[path[path.length - 1]] = node;
  }
  return out;
}

// ---- session initialization ----

const EVENTS = ['click', 'submit', 'input', 'change', 'keydown', 'keyup'];

function datasetKeyFor(event) {
  // data-msg-click → dataset.msgClick
  return 'msg' + event.charAt(0).toUpperCase() + event.slice(1);
}

function postEnvelope(eventURL, h, e, ver) {
  fetch(eventURL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ Type: 'Dispatch', Handler: h, Event: e, Ver: ver }),
  }).catch((err) => console.error('domi: event POST failed', err));
}

// run wires up the domi session on document.body and starts the SSE
// patch stream. Reads the URL prefix from body[data-domi-prefix=…]
// (which the server emits on initial render) and removes the
// attribute on its way out — so calling run() twice is a safe no-op,
// and calling it in a non-domi context (no body, no attribute) does
// nothing.
export function run() {
  if (typeof document === 'undefined' || !document.body) return;
  const container = document.body;
  const prefix = container.dataset.domiPrefix;
  if (!prefix) return;
  delete container.dataset.domiPrefix;
  const eventURL = `${prefix}/event`;
  const eventsURL = `${prefix}/events`;
  // The container is document.body. The framework treats it as the
  // patch root. App content becomes its children, addressed by patches
  // at [0], [1], …
  let root = container;

  const pathSets = new Map();
  function addPathSets(obj) {
    for (const k in obj) pathSets.set(k, obj[k]);
  }
  try {
    addPathSets(JSON.parse(container.dataset.domiPathSets || '{}'));
  } catch (err) {
    console.error('domi: bad path sets', err);
  }
  delete container.dataset.domiPathSets;

  // Snapshot cache for instant back/forward. Maps snapshot vers (the
  // tree versions of cached pages, stored in history.state) to
  // { frag, title }: a DocumentFragment holding detached clones of the
  // page's children, plus its document title. A snapshot is the whole
  // page as data — restoring it sets both the DOM and the title.
  // `ver` is the server-minted name of the tree the DOM displays; the
  // client remembers the last name it was told and echoes it with
  // events. `base` names the tree this patch lineage is rooted in; we
  // drop frames with a stale base. Both start at the initial tree.
  const snapshots = new Map();
  const SNAPSHOT_MAX = 30;
  let base = '11111111111111111111111111';
  let ver = '11111111111111111111111111';

  // The snapshot parameter is snapVer, not ver: these helpers assign
  // the outer ver, which a same-named parameter would shadow.
  function cacheSnapshot(snapVer, source, title) {
    if (!snapVer) return;
    const frag = document.createDocumentFragment();
    for (const child of source.childNodes) frag.appendChild(child.cloneNode(true));
    snapshots.set(snapVer, { frag, title });
    while (snapshots.size > SNAPSHOT_MAX) {
      snapshots.delete(snapshots.keys().next().value);
    }
  }

  function restoreSnapshot(snapVer) {
    const cached = snapshots.get(snapVer);
    if (!cached) return;
    while (root.firstChild) root.removeChild(root.firstChild);
    delete root.__domiChildren;
    // Clone-on-restore keeps the cache intact for future restores.
    const fresh = cached.frag.cloneNode(true);
    while (fresh.firstChild) root.appendChild(fresh.firstChild);
    document.title = cached.title ?? '';
    base = snapVer;
    ver = snapVer;
  }

  // The single in-flight link preview, or null. The client tracks exactly
  // one at a time: hovering a different link supersedes any previous, so it
  // can never claim a preview it has moved on from — and that hover is also
  // how the server learns to discard the old one. isReady flips false
  // (Prefetch sent) → true (SetPreview received, a patchset to apply on
  // click); isClicked records a click that beat the SetPreview, so it
  // navigates the moment one arrives.
  let pv = null; // { url, isReady, isClicked, patches?, title?, dest?, base?, ver?, at? }

  function checkPreviewTTL() {
    const ttl = 5000; // ms
    if (pv && pv.at && Date.now() - pv.at > ttl) pv = null;
  }

  // navigateToPreview applies the held preview by simulating
  // a normal navigation effect list: PushURL, ApplyPatch, SetTitle.
  // It navigates to p.dest, which is the requested url unless the app
  // redirected the preview, so the URL bar and the URLChange the server
  // routes on both reflect the page actually rendered.
  function navigateToPreview() {
    const p = pv;
    pv = null;
    cacheSnapshot(p.base, root, document.title);
    history.replaceState({ domiSnapshot: p.base }, '', location.href);
    history.pushState(null, '', p.dest);
    for (const patch of p.patches ?? []) root = applyPatch(root, patch);
    document.title = p.title ?? '';
    base = p.base;
    ver = p.ver;
    fetch(eventURL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ Type: 'URLChange', URL: p.dest, SnapshotVer: p.base, ToPreview: true }),
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
            const keys = [];
            const paths = [];
            for (const tok of raw.split(',')) {
              const ci = tok.indexOf(':');
              keys.push(tok.slice(0, ci));
              const p = pathSets.get(tok.slice(ci + 1));
              if (p) paths.push(...p);
            }
            postEnvelope(eventURL, keys.join(','), getFields(e, el, paths), ver);
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
    checkPreviewTTL();
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
    if (pv && pv.url === urlStr) {
      if (pv.isReady) {
        navigateToPreview();
      } else {
        pv.isClicked = true; // requested but not here yet; navigate when it arrives
      }
      return;
    }
    pv = null; // Clicking a different link abandons any preview.
    fetch(eventURL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ Type: 'URLRequest', URL: urlStr, Internal: true }),
    }).catch((err) => console.error('domi: urlRequest POST failed', err));
  });

  // Hover handler: prefetch the link under the cursor, superseding any
  // tracked preview (see pv).
  container.addEventListener('mouseover', (e) => {
    checkPreviewTTL();
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
    if (pv && pv.isClicked) return; // a click is committed; don't supersede it
    if (pv && pv.url === urlStr) return; // already requested or holding it
    pv = { url: urlStr, isReady: false, isClicked: false };
    fetch(eventURL, {
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
    const snapVer = e.state && e.state.domiSnapshot;
    pv = null; // it's based on the page we're leaving; drop it
    if (snapVer) restoreSnapshot(snapVer);
    fetch(eventURL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ Type: 'URLChange', URL: url, SnapshotVer: snapVer || '' }),
    }).catch((err) => console.error('domi: urlChange POST failed', err));
  });

  const sse = new EventSource(eventsURL);
  sse.addEventListener('effect', (ev) => {
    checkPreviewTTL();
    let f;
    try {
      f = JSON.parse(ev.data);
    } catch (e) {
      console.error('domi: bad effect JSON', ev.data, e);
      return;
    }
    if (f.Base && f.Base !== base) return;
    // Run the effects in the order the server chose. PushURL leads any
    // DOM patches so the outgoing page (current DOM + title) is
    // snapshotted before it changes; SetTitle trails them so that
    // snapshot still holds the old title. LoadURL abandons the document,
    // so it returns without touching the remaining effects.
    for (const eff of f.Effects) {
      switch (eff.Type) {
        case 'ApplyPatch':
          for (const p of eff.Patches) root = applyPatch(root, p);
          ver = eff.Ver;
          break;
        case 'SetTitle':
          document.title = eff.Title ?? '';
          break;
        case 'AddPathSets':
          addPathSets(eff.PathSets);
          break;
        case 'PushURL':
          cacheSnapshot(ver, root, document.title);
          history.replaceState({ domiSnapshot: ver }, '', location.href);
          history.pushState(null, '', eff.URL);
          break;
        case 'ReplaceURL':
          history.replaceState(history.state, '', eff.URL);
          break;
        case 'LoadURL':
          window.location.assign(eff.URL);
          return;
        case 'SetPreview':
          // Ignore any preview we're not intentionally waiting for. The
          // match is on the requested url; Dest is where committing the
          // preview navigates (it differs when the app redirected).
          if (!pv || pv.url !== eff.URL) break;
          pv.isReady = true;
          pv.patches = eff.Patches;
          pv.title = eff.Title;
          pv.dest = eff.Dest;
          pv.base = ver;
          pv.ver = eff.Ver;
          pv.at ||= Date.now();
          if (pv.isClicked) navigateToPreview();
          break;
        case 'DeletePreview':
          // Drop the preview for the given url. (Empty means any preview.)
          // A waiting click means the server denied the preview request
          // (or perhaps even a resync), so fall back to a normal request.
          if (pv && (!eff.URL || pv.url === eff.URL)) {
            const { url, isClicked } = pv;
            pv = null;
            if (isClicked) {
              fetch(eventURL, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ Type: 'URLRequest', URL: url, Internal: true }),
              }).catch((err) => console.error('domi: urlRequest POST failed', err));
            }
          }
          break;
        default:
          console.warn('domi: unknown effect', eff);
      }
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
