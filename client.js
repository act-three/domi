// domi client — ES module. Importing it has no side effects; a caller
// boots a session by invoking [run] explicitly, as the framework's HTML
// renderer does from an inline module script.

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
// domi-key attributes on element children.
function childMap(parent) {
  let map = parent.__domiChildren;
  if (!map) {
    map = new Map();
    for (const child of parent.children) {
      const k = child.getAttribute('domi-key');
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

// nodePath finds the path of node in root: each step is a key for a keyed
// child or a childNodes index for a positional one, so a keyed container is
// addressed by key and survives its siblings reordering.
function nodePath(root, node) {
  const path = [];
  for (let n = node; n !== root; n = n.parentNode) {
    const parent = n.parentNode;
    if (!parent) throw new Error('domi: mutate target is not inside domi-root');
    const key = n.getAttribute('domi-key');
    path.unshift(key != null ? key : indexOf(parent.childNodes, n));
  }
  return path;
}

function indexOf(nodes, node) {
  for (let i = 0; i < nodes.length; i++) if (nodes[i] === node) return i;
  return -1;
}

// isKeyed reports whether node is an element carrying a domi-key
// attribute — the keyedness test that anchor resolution and move
// vetting share. An empty-valued attribute counts as keyed here,
// unlike at the sites that read the key's value and treat "" as
// absent; that asymmetry belongs to the reserved-attribute question
// and is preserved as is.
function isKeyed(node) {
  return !!(node.hasAttribute && node.hasAttribute('domi-key'));
}

// insertAfterLastKeyed resolves a keyed op's empty `Before` anchor:
// node lands immediately after the parent's last keyed child, so any
// unkeyed content trailing the keyed run (a footer, say) stays after
// the run instead of being leapfrogged. When node itself is the last
// keyed child it stays put — hoisting it over the unkeyed content
// between it and the next keyed child would be gratuitous churn. With
// no keyed child at all, plain append; the gap patches that follow
// repair any imprecision. Must mirror the differ's simulation exactly.
function insertAfterLastKeyed(parent, node) {
  for (let c = parent.lastElementChild; c; c = c.previousElementSibling) {
    if (isKeyed(c)) {
      if (c !== node) parent.insertBefore(node, c.nextSibling);
      return;
    }
  }
  parent.appendChild(node);
}

// uniqueKey returns a key derived from key that is not present in map. It
// scans until it finds an unused one, so uniqueness is guaranteed by the
// check, not by the derivation; the marker is purely internal and has no
// significance — nothing depends on its format.
function uniqueKey(map, key) {
  for (let i = 1; ; i++) {
    const k = `${key}~domi-dup-${i}`;
    if (!map.has(k)) return k;
  }
}

// applyClientMutations applies an event's proposed mutation set to the
// live tree and returns the resolved ops to report. Each op names DOM
// nodes; domi resolves them to the paths and keys the server replays.
// The app hands over a clean tree — any transient drag visuals already
// undone — so the resolved addresses match the server's vdom.
//
// A proposal is applied fully or not at all: the set is vetted first,
// and if an op is unrecognized or malformed, applyClientMutations
// warns and returns null without touching the DOM, leaving the
// server's next render to reconcile what it proposed.
function applyClientMutations(root, muts) {
  for (const m of muts) {
    if (m.op !== 'move') {
      console.warn('domi: unknown mutation op', m.op);
      return null;
    }
    if (!moveOK(root, m)) {
      console.warn('domi: malformed move, skipping optimistic commit', m);
      return null;
    }
  }
  return muts.map((m) => applyMove(root, m.node, m.before ?? null, m.into ?? null));
}

// moveOK reports whether a move can be applied: its node is a keyed
// element inside root, and it has a destination — an anchor's parent,
// or an explicit container — that is itself an element inside root
// (an app-supplied text or detached destination would otherwise throw
// halfway through a vetted set). An anchor, when given, must itself
// be keyed: the reported op names it by key, so an unkeyed anchor would
// be reported as a plain append and the server's replay would diverge
// from the DOM. Mirrors applyMove's preconditions so a set can be
// vetted before any of it touches the DOM.
function moveOK(root, m) {
  const dst = m.before ? m.before.parentNode : m.into;
  if (!(dst && dst.nodeType === 1 && root.contains(dst))) return false;
  if (m.before && !isKeyed(m.before)) return false;
  return !!(m.node && m.node.parentNode && root.contains(m.node) && isKeyed(m.node));
}

// applyMove relocates the keyed node before `before`, or appends it into
// `into` when there's no anchor, keeping the per-parent child maps domi
// uses for keyed reconciliation in sync. If the destination already holds
// the node's key, domi generates a fresh one so the maps stay consistent;
// the server's next render owns the real key, so the re-keyed node is just
// removed when the render lands. Returns the move as a mutation op whose From
// and To are the node's path before and after the move — each ending in the
// node's key, the steps before it addressing its container.
function applyMove(root, node, before, into) {
  const src = node.parentNode;
  const dst = before ? before.parentNode : into;
  if (!src || !dst) throw new Error('domi: move needs a connected node and a destination');
  const key = node.getAttribute('domi-key');
  if (key == null) throw new Error('domi: move node has no domi-key');
  const from = nodePath(root, node);
  const beforeKey = before ? before.getAttribute('domi-key') || '' : '';

  const dstMap = childMap(dst);
  let newKey = key;
  const clash = dstMap.get(key);
  if (clash && clash !== node) {
    newKey = uniqueKey(dstMap, key);
    console.warn('domi: move key', key, 'already in destination; generated', newKey);
  }

  childMap(src).delete(key);
  node.setAttribute('domi-key', newKey);
  dst.insertBefore(node, before || null);
  dstMap.set(newKey, node);

  return { Op: 'move', From: from, To: nodePath(root, node), Before: beforeKey };
}

// isOpaque reports whether el is opaque: everything inside an element
// carrying domi-opaque is opaque — client-owned, none of domi's
// business, neither committed to the server nor reverted.
function isOpaque(el) {
  return !!(el.closest && el.closest('[domi-opaque]'));
}

// radioGroup returns el's radio group: the same-name radios under
// root, excluding opaque ones — or just el when it is unnamed.
// Checking a radio silently unchecks the rest of its group, so the
// group is the unit of committed checkedness. The group is name-scoped
// to the whole root; domi has no form-owner boundaries to respect,
// since forms are not one of its patterns.
function radioGroup(root, el) {
  const name = el.getAttribute('name');
  if (!name) return [el];
  const group = [];
  for (const r of root.querySelectorAll('input')) {
    if (r.type === 'radio' && r.getAttribute('name') === name && !isOpaque(r)) group.push(r);
  }
  return group;
}

// commitOps builds the mutation ops reporting a form control's committed
// state — the control facts the server records in its vdom before the
// handler runs — and writes the same state into the live attributes
// and text, so the DOM the ops leave behind matches the tree the
// server reconstructs from them. Radios report their whole group (see
// radioGroup); a select reports every option's selectedness, which
// covers multiple-selects and options deselected without an event of
// their own. Controls domi doesn't commit — file inputs, whose value
// is not domi's to set, and opaque controls — yield no ops, as does a
// target that isn't a form control at all; an opaque option inside a
// plain select is likewise passed over.
function commitOps(root, el) {
  if (!el || el.nodeType !== 1 || isOpaque(el)) return [];
  editing.delete(el);
  const checkedOp = (r) => {
    if (r.checked) r.setAttribute('checked', '');
    else r.removeAttribute('checked');
    return { Op: 'setchecked', Path: nodePath(root, r), Checked: r.checked };
  };
  switch (el.tagName) {
    case 'INPUT':
      if (el.type === 'file') return [];
      if (el.type === 'checkbox') return [checkedOp(el)];
      if (el.type === 'radio') return radioGroup(root, el).map(checkedOp);
      el.setAttribute('value', el.value);
      return [{ Op: 'setvalue', Path: nodePath(root, el), Value: el.value }];
    case 'TEXTAREA':
      el.textContent = el.value;
      return [{ Op: 'settext', Path: nodePath(root, el), Value: el.value }];
    case 'SELECT': {
      const ops = [];
      for (const o of el.options) {
        if (isOpaque(o)) continue;
        if (o.selected) o.setAttribute('selected', '');
        else o.removeAttribute('selected');
        ops.push({ Op: 'setselected', Path: nodePath(root, o), Selected: o.selected });
      }
      return ops;
    }
  }
  return [];
}

// revertControl restores a form control to its rendered state — the
// attributes and text the server last sent. This is the degenerate
// convergence for a control nobody listens to: the render is already
// in hand, so there is nothing to report and no round trip to make,
// and the user learns immediately that the control is display-only.
// The exemptions mirror commitOps's.
function revertControl(root, el) {
  if (!el || el.nodeType !== 1 || isOpaque(el)) return;
  editing.delete(el);
  switch (el.tagName) {
    case 'INPUT':
      if (el.type === 'file') return;
      if (el.type === 'checkbox') el.checked = el.hasAttribute('checked');
      else if (el.type === 'radio') {
        for (const r of radioGroup(root, el)) r.checked = r.hasAttribute('checked');
      } else el.value = el.getAttribute('value') ?? '';
      return;
    case 'TEXTAREA':
      el.value = el.textContent;
      return;
    case 'SELECT':
      for (const o of el.options) {
        if (!isOpaque(o)) o.selected = o.hasAttribute('selected');
      }
      return;
  }
}

// hasEditHandler reports whether a handler on el or an ancestor
// listens for el's edits — an input or change handler, the two commit
// events. (Toggles bind change; see event.Check.)
function hasEditHandler(root, el) {
  for (let n = el; n && n !== root.parentNode; n = n.parentNode) {
    if (n.nodeType === 1 && (n.getAttribute('domi-msg-input') || n.getAttribute('domi-msg-change'))) {
      return true;
    }
  }
  return false;
}

// syncProp mirrors a just-patched attribute into the property it
// normally reflects into, on the form controls where user interaction
// severs that reflection: typing sets an input's dirty value flag,
// toggling sets its dirty checkedness flag, and choosing an option sets
// its selectedness's dirtiness. From then on the content attribute only
// tracks the default, so a bare setAttribute/removeAttribute leaves the
// user's stale state on screen; assigning the property makes the patch
// visible. Attributes and elements outside these three pairs reflect
// live and need nothing.
//
// The value write is skipped only for a focused control marked
// editing (see [editing]), so in-flight typing is never overwritten;
// everything else — committed, unfocused, or a detached clone —
// converges to the patch. Checkedness and selectedness sync
// unconditionally: they are discrete, with no half-typed state to
// protect.
function syncProp(el, name) {
  switch (name) {
    case 'value':
      // Never on a file input: its value property is in filename mode,
      // where a non-empty assignment throws (aborting the rest of the
      // patch frame) and an empty one discards the user's selected
      // file — and the value attribute doesn't apply to it, so there
      // is no default to mirror in the first place.
      if (el.tagName === 'INPUT' && el.type !== 'file' && (el !== document.activeElement || !editing.has(el))) {
        el.value = el.getAttribute('value') ?? '';
      }
      break;
    case 'checked':
      if (el.tagName === 'INPUT') el.checked = el.hasAttribute('checked');
      break;
    case 'selected':
      if (el.tagName === 'OPTION') el.selected = el.hasAttribute('selected');
      break;
  }
}

// syncTextareaValue is syncProp's counterpart for a textarea's content:
// the text inside a textarea is only its default value, so once the
// user has typed (dirty value flag again), patching the child text
// alone leaves the display stale. Called with the parent of any patched
// text node and any inserted or removed child — a blanked-out textarea
// arrives as RemoveChild, a filled-in one as InsertChild — it copies
// the content into the value property under syncProp's guard. Parents
// other than textarea need nothing and are left alone.
function syncTextareaValue(parent) {
  if (parent.tagName === 'TEXTAREA' && (parent !== document.activeElement || !editing.has(parent))) {
    parent.value = parent.textContent;
  }
}

// editing holds the controls with an edit that has not reached a
// commit point. Every input and change event marks its control (the
// marking listener in run); reaching a commit (commitOps), a revert
// (revertControl), or losing focus (the focusout listener in run)
// clears it.
const editing = new WeakSet();

// applyPatch applies a single patch to the tree rooted at `root`.
// Patches address the root's children — a path names the root itself
// only as the parent of a child op — so the root is never replaced; it
// survives the whole patch stream.
//
// Every op is a pure DOM mutation of `root` alone — no document-level or
// navigation side-effects — so it is safe to run against a detached
// clone, as preview snapshot construction does. Document-level changes
// travel as effects in an effect frame, run only against the live page.
function applyPatch(root, p) {
  switch (p.Op) {
    case 'Replace': {
      const node = walk(root, p.Path);
      const parent = node.parentNode;
      const fresh = fragmentFromHTML(p.HTML).firstChild;
      parent.replaceChild(fresh, node);
      // Keep the parent's keyed-child map in step: either node may be
      // keyed (a Replace is emitted for a key-matched pair whose tag
      // or opacity changed), and a stale entry would point later keyed
      // ops at the detached node.
      const map = parent.__domiChildren;
      if (map) {
        const oldKey = node.getAttribute && node.getAttribute('domi-key');
        if (oldKey != null) map.delete(oldKey);
        const newKey = fresh.getAttribute && fresh.getAttribute('domi-key');
        if (newKey != null) map.set(newKey, fresh);
      }
      break;
    }
    case 'SetText': {
      // Write the new text straight to nodeValue: it takes a raw string
      // with no HTML parsing, so the unescaped Value lands verbatim, and
      // the text node keeps its identity — a selection anchored in it
      // survives. A text node never goes empty: the Go side drops empty
      // text during canonicalization, so a blanked-out value arrives as
      // RemoveChild, not SetText with a missing Value.
      const text = walk(root, p.Path);
      text.nodeValue = p.Value;
      syncTextareaValue(text.parentNode);
      break;
    }
    case 'SetAttr': {
      // Coerce undefined → "" so name-only / empty-valued attrs land as
      // present-with-empty-string. The encoder omits `Value` when it's
      // the empty string (omitempty on the Go side), so a missing field
      // here means "set this attribute to empty", not "set it to
      // undefined".
      const el = walk(root, p.Path);
      el.setAttribute(p.Name, p.Value ?? '');
      syncProp(el, p.Name);
      break;
    }
    case 'RemoveAttr': {
      const el = walk(root, p.Path);
      el.removeAttribute(p.Name);
      syncProp(el, p.Name);
      break;
    }
    case 'InsertChild': {
      const parent = walk(root, p.Path);
      const newNode = fragmentFromHTML(p.HTML).firstChild;
      if (p.Key != null) {
        const map = childMap(parent);
        if (p.Before) {
          parent.insertBefore(newNode, map.get(p.Before) || null);
        } else {
          insertAfterLastKeyed(parent, newNode);
        }
        map.set(p.Key, newNode);
      } else {
        parent.insertBefore(newNode, parent.childNodes[p.Index] || null);
      }
      syncTextareaValue(parent);
      break;
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
      syncTextareaValue(parent);
      break;
    }
    case 'MoveChild': {
      const parent = walk(root, p.Path);
      if (p.Key != null) {
        const map = childMap(parent);
        const node = map.get(p.Key);
        if (node) {
          if (p.Before) {
            parent.insertBefore(node, map.get(p.Before) || null);
          } else {
            insertAfterLastKeyed(parent, node);
          }
        }
      } else {
        const node = parent.childNodes[p.From];
        parent.removeChild(node);
        parent.insertBefore(node, parent.childNodes[p.To] || null);
      }
      break;
    }
    case 'Reset': {
      // The root (and its delegated listeners) survives the rebuild, so
      // the session keeps working after a server-driven full resync.
      while (root.firstChild) root.removeChild(root.firstChild);
      delete root.__domiChildren;
      const frag = fragmentFromHTML(p.HTML);
      while (frag.firstChild) root.appendChild(frag.firstChild);
      break;
    }
    default:
      console.warn('domi: unknown op', p);
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
function getFields(e, el, paths) {
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

function postEnvelope(eventURL, h, e, ver, mutations) {
  const body = { Type: 'Dispatch', Handler: h, Event: e, Ver: ver };
  if (mutations && mutations.length) body.Mutations = mutations;
  fetch(eventURL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  }).catch((err) => console.error('domi: event POST failed', err));
}

// run wires up the domi session on the <domi-root> mount element just
// inside document.body and starts the SSE patch stream. Reads the URL
// prefix from domi-root[prefix=…] (which the server emits on initial
// render) and removes the attribute on its way out.
export function run() {
  if (typeof document === 'undefined') return; // synthetic test env is ok
  // root is the patch root.
  // the patch address [] is root itself, [0] is its first child,
  // [0,0] is the first child of its first child, etc.
  // root itself is never replaced.
  // delegated event handlers are set on root.
  const root = document.querySelector('body > domi-root');
  if (!root) throw new Error('domi: element domi-root not found');
  const prefix = root.getAttribute('prefix');
  if (!prefix) throw new Error('domi: attribute domi-root[prefix] not found');
  root.removeAttribute('prefix');
  const eventURL = `${prefix}/event`;
  const eventsURL = `${prefix}/events`;

  const pathSets = new Map();
  function addPathSets(obj) {
    for (const k in obj) pathSets.set(k, obj[k]);
  }
  try {
    addPathSets(JSON.parse(root.getAttribute('path-sets') || '{}'));
  } catch (err) {
    console.error('domi: bad path sets', err);
  }
  root.removeAttribute('path-sets');

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
    for (const patch of p.patches ?? []) applyPatch(root, patch);
    document.title = p.title ?? '';
    base = p.base;
    ver = p.ver;
    fetch(eventURL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ Type: 'URLChange', URL: p.dest, SnapshotVer: p.base, ToPreview: true }),
    }).catch((err) => console.error('domi: urlChange POST failed', err));
  }

  // Be sure to run marking before the dispatch listeners (registration order),
  // so a commit or revert in the same event clears the mark it just
  // set. See [editing].
  for (const ev of ['input', 'change']) {
    root.addEventListener(ev, (e) => {
      if (e.target.nodeType === 1) editing.add(e.target);
    });
  }
  root.addEventListener('focusout', (e) => {
    if (e.target.nodeType === 1) editing.delete(e.target);
  });

  for (const ev of EVENTS) {
    root.addEventListener(ev, (e) => {
      // input and change are commit points: the user's edit to a form
      // control is ready to reconcile with the server. During IME
      // composition nothing is ready yet, so nothing commits.
      const commits = (ev === 'input' || ev === 'change') && !e.isComposing;
      let el = e.target;
      while (el && el !== root.parentNode) {
        if (el.nodeType === 1) {
          const raw = el.getAttribute('domi-msg-' + ev);
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
            const fields = getFields(e, el, paths);
            const committed = commits ? commitOps(root, e.target) : [];
            const muts = e.detail && e.detail.domi && e.detail.domi.mutations;
            const proposed = Array.isArray(muts) && muts.length ? (applyClientMutations(root, muts) ?? []) : [];
            const ops = committed.concat(proposed);
            if (ops.length) {
              // Optimistic commit: the mutations are applied and we rebase
              // onto a derived version (so frames built against the old tree
              // drop), echoing the version we acted on. The server replays
              // the ops to reconstruct what we show, then diffs that
              // against its render.
              const acted = ver;
              base = ver = acted + '-mutated';
              postEnvelope(eventURL, keys.join(','), fields, acted, ops);
            } else {
              // No mutations, or a set we declined to apply: a plain
              // dispatch, leaving the server's next render to reconcile.
              postEnvelope(eventURL, keys.join(','), fields, ver);
            }
            return;
          }
        }
        el = el.parentNode;
      }
      // A commit on a control with no app handler is reverted here
      // to the rendered state, to keep client DOM in sync.
      if (commits && e.target.nodeType === 1 && !hasEditHandler(root, e.target)) {
        revertControl(root, e.target);
      }
    });
  }

  // Link interception for SPA navigation. Intercepts clicks on <a>
  // elements with same-origin hrefs and routes them through the
  // server's onURLRequest callback instead of navigating. Skips
  // modified clicks (ctrl/shift/alt/meta), non-left-button clicks,
  // links with target attributes, download links, links carrying the
  // domi-bypass attribute (the app opted out of interception so
  // the browser navigates normally), and links where an ancestor
  // already has a domi-msg-click handler (the app opted into explicit
  // handling).
  root.addEventListener('click', (e) => {
    checkPreviewTTL();
    if (e.button !== 0 || e.ctrlKey || e.shiftKey || e.altKey || e.metaKey) return;
    let a = e.target;
    while (a && a !== root) {
      if (a.tagName === 'A') break;
      a = a.parentNode;
    }
    if (!a || a.tagName !== 'A') return;

    // If a domi-msg-click handler exists between the target and the
    // <a>, the app explicitly handles this click — skip interception.
    let el = e.target;
    while (el && el !== a.parentNode) {
      if (el.nodeType === 1 && el.getAttribute('domi-msg-click')) return;
      el = el.parentNode;
    }

    const href = a.getAttribute('href');
    if (!href) return;
    const target = a.getAttribute('target');
    if (target && target !== '_self') return;
    if (a.hasAttribute('download')) return;
    if (a.hasAttribute('domi-bypass')) return;

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
  root.addEventListener('mouseover', (e) => {
    checkPreviewTTL();
    let a = e.target;
    while (a && a !== root) {
      if (a.tagName === 'A') break;
      a = a.parentNode;
    }
    if (!a || a.tagName !== 'A') return;
    const href = a.getAttribute('href');
    if (!href) return;
    const target = a.getAttribute('target');
    if (target && target !== '_self') return;
    if (a.hasAttribute('download')) return;
    if (a.hasAttribute('domi-bypass')) return;
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
          for (const p of eff.Patches) applyPatch(root, p);
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
