// processmap.js: a process drawn in BPMN, the designer's canvas
// (processedit.js). A step is a user task, a sub-process a collapsed one, a
// gateway a diamond with X (first route that holds) or + (every route that
// holds, at once), an end a heavy circle; a start event points at the first
// step, and a route that just stops ends in a small end event of its own.
//
//   const map = ProcessMap.mount(host, process, { onSelect, onChange, onConnect, onDelete, label, flagged });
//
// Positions live on the nodes (x, y: their centres); a map without them is
// laid out in layers from the first step. Nodes are dragged to move, and a
// route is drawn from the handle under the chosen node to another node -- or
// to empty canvas, for a new step there. Every node is a focusable button:
// Enter chooses it, the arrow keys move it, Delete removes it.
//
// Text goes in as textContent only.
(function () {
  'use strict';
  const NS = 'http://www.w3.org/2000/svg';
  const SIZE = { step: [150, 60], subprocess: [150, 60], gateway: [50, 50], end: [36, 36] };
  const GX = 200, GY = 120, PAD = 70, SNAP = 10;
  const fa = s => String(s).replace(/\d/g, c => '۰۱۲۳۴۵۶۷۸۹'[c]);

  function s(tag, attrs, kids) {
    const n = document.createElementNS(NS, tag);
    for (const [k, v] of Object.entries(attrs || {})) if (v != null && v !== false) n.setAttribute(k, v);
    for (const c of [].concat(kids == null ? [] : kids)) if (c != null && c !== false) n.append(c);
    return n;
  }
  const size = n => SIZE[n.kind] || SIZE.step;
  const placed = n => !!(n.x || n.y);

  // A gateway's BPMN kind: what it was drawn as, or what its routes say.
  function gateOf(p, n) {
    if (n.gate === 'xor' || n.gate === 'par') return n.gate;
    return p.edges.some(e => (e.from === n.id && e.kind === 'par') || (e.to === n.id && e.kind === 'join')) ? 'par' : 'xor';
  }

  // Layers from the first step: each node one row below the lowest node with
  // a route into it (routes back up the map left out), each row ordered to
  // sit under its parents. all=false places only the nodes that have no place.
  function layout(p, all) {
    const fresh = p.nodes.filter(n => all || !placed(n));
    if (!fresh.length) return;
    if (!all && fresh.length < p.nodes.length) {
      const y = Math.max(...p.nodes.filter(placed).map(n => n.y)) + GY;
      fresh.forEach((n, i) => { n.x = 120 + i * GX; n.y = y; });
      return;
    }
    const byId = new Map(p.nodes.map(n => [n.id, n]));
    const out = new Map(p.nodes.map(n => [n.id, []]));
    for (const e of p.edges) if (e.to && byId.has(e.from) && byId.has(e.to)) out.get(e.from).push(e.to);
    const seen = new Map(), post = [], back = new Set();
    const visit = id => {
      seen.set(id, 1);
      for (const t of out.get(id)) {
        if (seen.get(t) === 1) back.add(id + '\n' + t);
        else if (!seen.has(t)) visit(t);
      }
      seen.set(id, 2);
      post.push(id);
    };
    if (byId.has(p.start)) visit(p.start);
    for (const n of p.nodes) if (!seen.has(n.id)) visit(n.id);
    const topo = post.reverse();
    const fwd = id => out.get(id).filter(t => !back.has(id + '\n' + t));
    const layer = new Map(topo.map(id => [id, 0]));
    const preds = new Map(topo.map(id => [id, []]));
    for (const id of topo) for (const t of fwd(id)) { layer.set(t, Math.max(layer.get(t), layer.get(id) + 1)); preds.get(t).push(id); }
    const rows = [];
    for (const id of topo) (rows[layer.get(id)] = rows[layer.get(id)] || []).push(id);
    const pos = new Map();
    const spread = row => row.forEach((id, i) => pos.set(id, i - (row.length - 1) / 2));
    rows.forEach(r => r && spread(r));
    const bary = (row, near) => {
      if (!row) return;
      const key = new Map(row.map(id => {
        const ns = near(id).filter(x => pos.has(x));
        return [id, ns.length ? ns.reduce((a, x) => a + pos.get(x), 0) / ns.length : pos.get(id)];
      }));
      row.sort((a, b) => key.get(a) - key.get(b));
      spread(row);
    };
    for (let i = 0; i < 4; i++) {
      for (let l = 1; l < rows.length; l++) bary(rows[l], id => preds.get(id));
      for (let l = rows.length - 2; l >= 0; l--) bary(rows[l], fwd);
    }
    rows.forEach((r, l) => r && r.forEach(id => { const n = byId.get(id); n.x = Math.round(pos.get(id) * GX); n.y = 90 + l * GY; }));
    const minX = Math.min(...p.nodes.map(n => n.x));
    p.nodes.forEach(n => { n.x = n.x - minX + 120; });
  }

  // A name in at most two lines of about 18 characters.
  function lines(text, max) {
    const words = String(text || '').split(/\s+/).filter(Boolean);
    const out = [''];
    for (const w of words) {
      const cur = out[out.length - 1];
      if (!cur || (cur + ' ' + w).length <= max) out[out.length - 1] = cur ? cur + ' ' + w : w;
      else if (out.length < 2) out.push(w);
      else { out[1] = (out[1] + ' ' + w).slice(0, max - 1) + '…'; break; }
    }
    return out.map(l => (l.length > max ? l.slice(0, max - 1) + '…' : l));
  }
  function label(textLines, y, cls) {
    const t = s('text', { class: cls || 'pm-text', y: y - (textLines.length - 1) * 8, 'text-anchor': 'middle', direction: 'rtl' });
    textLines.forEach((l, i) => t.append(s('tspan', { x: 0, dy: i ? 16 : 0 }, l)));
    return t;
  }

  function mount(host, p, opts) {
    opts = opts || {};
    let zoom = 1, sel = null, box = { x: 0, y: 0, w: 800, h: 600 };
    let drag = null;
    const svg = s('svg', { class: 'pm-svg', role: 'group', 'aria-label': 'نقشهٔ فرآیند' });
    host.append(svg);
    const node = id => p.nodes.find(n => n.id === id);

    // Where a route that just stops ends: a small end event under its step.
    const endOf = (e, i) => { const a = node(e.from); return { x: a.x + 46, y: a.y + size(a)[1] / 2 + 56 + (i % 2) * 6, w: 24, h: 24 }; };
    const rect = n => ({ x: n.x, y: n.y, w: size(n)[0], h: size(n)[1] });

    function measure() {
      const pts = [];
      p.nodes.forEach(n => { const r = rect(n); pts.push([r.x - r.w / 2, r.y - r.h / 2], [r.x + r.w / 2, r.y + r.h / 2 + 30]); });
      p.edges.forEach((e, i) => { if (!e.to && node(e.from)) { const r = endOf(e, i); pts.push([r.x + 20, r.y + 20]); } });
      const st = node(p.start);
      if (st) pts.push([st.x, st.y - size(st)[1] / 2 - 60]);
      if (!pts.length) pts.push([0, 0], [600, 400]);
      const xs = pts.map(q => q[0]), ys = pts.map(q => q[1]);
      const x = Math.min(...xs) - PAD, y = Math.min(...ys) - PAD;
      box = { x, y, w: Math.max(...xs) - x + PAD + 60, h: Math.max(...ys) - y + PAD };
    }

    // A route's points: down out of the bottom and into the top; one that
    // would run through another node on its way down, or that goes back up the
    // map (or across a row), goes round the side.
    let side = 0;
    const inWay = (x, y1, y2, skip) => p.nodes.filter(n => !skip.includes(n.id) &&
      Math.abs(n.x - x) < size(n)[0] / 2 + 6 && n.y + size(n)[1] / 2 > y1 && n.y - size(n)[1] / 2 < y2);
    function route(e, i) {
      const a = rect(node(e.from));
      const b = e.to ? rect(node(e.to)) : endOf(e, i);
      const sx = a.x, sy = a.y + a.h / 2, tx = b.x, ty = b.y - b.h / 2;
      if (ty >= sy + 24) {
        const my = sy + 16;
        const block = inWay(tx, my, ty - 2, [e.from, e.to]);
        if (!block.length) return { pts: [[sx, sy], [sx, my], [tx, my], [tx, ty]], at: [tx, Math.min(my + 15, ty - 4)] };
        const x = Math.max(...block.map(n => n.x + size(n)[0] / 2), a.x + a.w / 2) + 26 + (side++ % 5) * 9;
        const ny = ty - 18;
        return { pts: [[sx, sy], [sx, my], [x, my], [x, ny], [tx, ny], [tx, ty]], at: [x + 6, (my + ny) / 2], side: true };
      }
      const x = Math.max(a.x + a.w / 2, b.x + b.w / 2) + 26 + (side++ % 5) * 9;
      return { pts: [[a.x + a.w / 2, a.y], [x, a.y], [x, b.y], [b.x + b.w / 2, b.y]], at: [x + 6, (a.y + b.y) / 2], side: true };
    }

    function shape(n) {
      const [w, h] = size(n);
      const kids = [];
      if (n.kind === 'gateway') {
        kids.push(s('polygon', { class: 'pm-shape', points: `0,${-h / 2} ${w / 2},0 0,${h / 2} ${-w / 2},0` }));
        const m = gateOf(p, n) === 'par'
          ? [[0, -12, 0, 12], [-12, 0, 12, 0]]
          : [[-8, -8, 8, 8], [8, -8, -8, 8]];
        m.forEach(([x1, y1, x2, y2]) => kids.push(s('line', { class: 'pm-glyph', x1, y1, x2, y2 })));
        if (n.name) kids.push(label(lines(n.name, 22), h / 2 + 18, 'pm-text pm-under'));
      } else if (n.kind === 'end') {
        kids.push(s('circle', { class: 'pm-shape pm-end', r: w / 2 }));
        if (n.name) kids.push(label(lines(n.name, 22), h / 2 + 18, 'pm-text pm-under'));
      } else {
        kids.push(s('rect', { class: 'pm-shape', x: -w / 2, y: -h / 2, width: w, height: h, rx: 10 }));
        kids.push(s('use', { href: '#i-user', class: 'pm-mark', x: w / 2 - 20, y: -h / 2 + 6, width: 14, height: 14 }));
        if (n.kind === 'subprocess') kids.push(s('use', { href: '#i-square-plus', class: 'pm-mark', x: -7, y: h / 2 - 17, width: 14, height: 14 }));
        kids.push(label(lines(n.name, 19), 5));
      }
      const kind = { step: 'گام', subprocess: 'زیرفرآیند', gateway: gateOf(p, n) === 'par' ? 'دروازهٔ موازی' : 'دروازهٔ انحصاری', end: 'پایان' }[n.kind] || 'گام';
      const cls = ['pm-node', 'pm-' + n.kind];
      if (sel && sel.node === n.id) cls.push('is-sel');
      if (opts.flagged && opts.flagged(n)) cls.push('is-flag');
      if (opts.dim && opts.dim(n)) cls.push('is-dim');
      return s('g', { class: cls.join(' '), transform: `translate(${n.x},${n.y})`, tabindex: 0, role: 'button', 'data-node': n.id,
        'aria-label': kind + ' ' + (n.name || ''), 'aria-pressed': String(!!(sel && sel.node === n.id)) }, kids);
    }

    function draw() {
      const focused = document.activeElement && svg.contains(document.activeElement);
      measure();
      side = 0;
      svg.textContent = '';
      svg.setAttribute('viewBox', `${box.x} ${box.y} ${box.w} ${box.h}`);
      svg.setAttribute('width', Math.round(box.w * zoom));
      svg.setAttribute('height', Math.round(box.h * zoom));
      const arrow = (id, cls) => s('marker', { id, viewBox: '0 0 10 10', refX: 9, refY: 5, markerWidth: 7, markerHeight: 7, orient: 'auto-start-reverse' },
        s('path', { d: 'M0,0 L10,5 L0,10 z', class: cls }));
      svg.append(s('defs', {}, [arrow('pm-arrow', 'pm-head'), arrow('pm-arrow-sel', 'pm-head is-sel')]));
      const edges = s('g', { class: 'pm-edges' }), nodes = s('g', { class: 'pm-nodes' }), over = s('g');
      const st = node(p.start);
      if (st) {
        const top = st.y - size(st)[1] / 2;
        edges.append(s('line', { class: 'pm-edge', x1: st.x, y1: top - 42, x2: st.x, y2: top, 'marker-end': 'url(#pm-arrow)' }));
        nodes.append(s('g', { class: 'pm-node pm-start', transform: `translate(${st.x},${top - 60})`, 'aria-hidden': 'true' },
          [s('circle', { class: 'pm-shape', r: 18 }), label(['آغاز'], 4, 'pm-text pm-small')]));
      }
      p.edges.forEach((e, i) => {
        if (!node(e.from) || (e.to && !node(e.to))) return;
        const r = route(e, i);
        const on = sel && sel.edge === i;
        const d = 'M' + r.pts.map(q => q.join(',')).join(' L');
        const g = s('g', { class: 'pm-route' + (on ? ' is-sel' : ''), 'data-edge': i }, [
          s('path', { class: 'pm-hit', d }),
          s('path', { class: 'pm-edge' + (e.kind === 'join' ? ' pm-join' : ''), d, 'marker-end': on ? 'url(#pm-arrow-sel)' : 'url(#pm-arrow)' })]);
        const text = opts.label ? opts.label(e, i) : '';
        if (text) {
          const ltr = !/[\u0621-\u064A\u067E-\u06D3]/.test(text);
          g.append(s('text', { class: 'pm-text pm-cond', x: r.at[0], y: r.at[1], direction: ltr ? 'ltr' : 'rtl', 'text-anchor': r.side ? (ltr ? 'start' : 'end') : 'middle' }, text));
        }
        edges.append(g);
        if (!e.to) {
          const b = endOf(e, i);
          nodes.append(s('g', { class: 'pm-node pm-end pm-tail', transform: `translate(${b.x},${b.y})`, 'aria-hidden': 'true' }, s('circle', { class: 'pm-shape pm-end', r: 12 })));
        }
      });
      p.nodes.forEach(n => nodes.append(shape(n)));
      const cur = sel && sel.node && node(sel.node);
      if (cur && cur.kind !== 'end' && !opts.readOnly) {
        const y = cur.y + size(cur)[1] / 2 + 14;
        over.append(s('g', { class: 'pm-handle', transform: `translate(${cur.x},${y})`, 'data-handle': cur.id }, [
          s('title', {}, 'برای کشیدن مسیر بکشید'), s('circle', { r: 10 }), s('line', { x1: 0, y1: -5, x2: 0, y2: 5 }), s('line', { x1: -5, y1: 0, x2: 5, y2: 0 })]));
      }
      if (drag && drag.line) over.append(s('line', { class: 'pm-edge is-sel', x1: drag.line[0], y1: drag.line[1], x2: drag.line[2], y2: drag.line[3], 'marker-end': 'url(#pm-arrow-sel)' }));
      svg.append(edges, nodes, over);
      if (focused && cur) { const g = svg.querySelector(`[data-node="${CSS.escape(cur.id)}"]`); if (g) g.focus({ preventScroll: true }); }
    }

    // The point under the pointer, in map coordinates.
    const at = ev => { const r = svg.getBoundingClientRect(); return [box.x + (ev.clientX - r.left) / zoom, box.y + (ev.clientY - r.top) / zoom]; };
    const snap = v => Math.round(v / SNAP) * SNAP;
    const choose = next => { sel = next; draw(); if (opts.onSelect) opts.onSelect(sel); };

    svg.addEventListener('pointerdown', ev => {
      if (ev.button !== 0) return;
      const handle = ev.target.closest('[data-handle]'), g = ev.target.closest('[data-node]'), eg = ev.target.closest('[data-edge]');
      const pt = at(ev);
      if (handle) {
        const n = node(handle.dataset.handle);
        drag = { connect: n.id, from: [n.x, n.y + size(n)[1] / 2], moved: false };
      } else if (g) {
        const n = node(g.dataset.node);
        if (!sel || sel.node !== n.id) choose({ node: n.id });
        if (!opts.readOnly) drag = { node: n.id, start: [n.x, n.y], p0: pt, moved: false };
      } else if (eg) {
        choose({ edge: Number(eg.dataset.edge) });
      } else {
        choose(null);
      }
      if (drag) {
        try { svg.setPointerCapture(ev.pointerId); } catch (_) { /* a pointer the page did not see begin */ }
        ev.preventDefault();
      }
    });
    svg.addEventListener('pointermove', ev => {
      if (!drag) return;
      const pt = at(ev);
      if (drag.node) {
        const n = node(drag.node);
        const nx = snap(drag.start[0] + pt[0] - drag.p0[0]), ny = snap(drag.start[1] + pt[1] - drag.p0[1]);
        if (!drag.moved && Math.abs(nx - drag.start[0]) < SNAP && Math.abs(ny - drag.start[1]) < SNAP) return;
        drag.moved = true;
        n.x = nx; n.y = ny;
      } else {
        drag.moved = true;
        drag.line = [drag.from[0], drag.from[1], pt[0], pt[1]];
      }
      draw();
    });
    const finish = ev => {
      if (!drag) return;
      const d = drag;
      drag = null;
      if (d.node && d.moved && opts.onChange) opts.onChange('move');
      if (d.connect && d.moved) {
        const hit = document.elementFromPoint(ev.clientX, ev.clientY);
        const g = hit && hit.closest && hit.closest('[data-node]');
        const pt = at(ev);
        if (opts.onConnect) opts.onConnect(d.connect, g && svg.contains(g) ? g.dataset.node : null, [snap(pt[0]), snap(pt[1])]);
      }
      draw();
    };
    svg.addEventListener('pointerup', finish);
    svg.addEventListener('pointercancel', () => { drag = null; draw(); });
    svg.addEventListener('keydown', ev => {
      const g = ev.target.closest && ev.target.closest('[data-node]');
      if (!g) return;
      const n = node(g.dataset.node);
      const step = ev.shiftKey ? 50 : SNAP;
      const move = { ArrowUp: [0, -step], ArrowDown: [0, step], ArrowLeft: [-step, 0], ArrowRight: [step, 0] }[ev.key];
      if (ev.key === 'Enter' || ev.key === ' ') { choose({ node: n.id }); ev.preventDefault(); }
      else if (move && !opts.readOnly) { n.x += move[0]; n.y += move[1]; sel = { node: n.id }; draw(); if (opts.onChange) opts.onChange('move'); ev.preventDefault(); }
      else if ((ev.key === 'Delete' || ev.key === 'Backspace') && opts.onDelete && !opts.readOnly) { opts.onDelete(n.id); ev.preventDefault(); }
    });
    svg.addEventListener('focusin', ev => {
      const g = ev.target.closest && ev.target.closest('[data-node]');
      if (g && (!sel || sel.node !== g.dataset.node)) { sel = { node: g.dataset.node }; if (opts.onSelect) opts.onSelect(sel); requestAnimationFrame(draw); }
    });
    host.addEventListener('wheel', ev => {
      if (!ev.ctrlKey) return;
      ev.preventDefault();
      api.zoomBy(ev.deltaY < 0 ? 1.1 : 1 / 1.1);
    }, { passive: false });

    const api = {
      draw,
      layout(all) { layout(p, all); draw(); },
      select(next) { sel = next; draw(); },
      get selection() { return sel; },
      zoomBy(f) { zoom = Math.min(2, Math.max(0.25, zoom * f)); draw(); },
      fit() {
        measure();
        zoom = Math.min(1.2, Math.max(0.25, Math.min((host.clientWidth - 8) / box.w, (host.clientHeight - 8) / box.h)));
        draw();
      },
      // The first look: the whole map when it can be read that way, else
      // readable size at the first step.
      open() {
        measure();
        const whole = Math.min((host.clientWidth - 8) / box.w, (host.clientHeight - 8) / box.h);
        if (whole >= 0.7) { api.fit(); return; }
        zoom = 0.9;
        draw();
        const st = node(p.start);
        if (st) host.scrollTo({ left: (st.x - box.x) * zoom - host.clientWidth / 2, top: Math.max(0, (st.y - box.y) * zoom - 120) });
      },
      // Scroll so a node is in view.
      show(id) {
        const n = node(id);
        if (!n) return;
        host.scrollTo({ left: (n.x - box.x) * zoom - host.clientWidth / 2, top: (n.y - box.y) * zoom - host.clientHeight / 2, behavior: 'smooth' });
      },
      // The middle of what is in view, in map coordinates, for a new node.
      middle() { return [snap(box.x + (host.scrollLeft + host.clientWidth / 2) / zoom), snap(box.y + (host.scrollTop + host.clientHeight / 2) / zoom)]; },
    };
    layout(p, false);
    draw();
    return api;
  }

  // A small drawing of a kind of node, for the palette's buttons.
  function glyph(kind, gate) {
    const svg = s('svg', { class: 'pm-glyph-ic', viewBox: '-14 -14 28 28', 'aria-hidden': 'true' });
    if (kind === 'gateway') {
      svg.append(s('polygon', { class: 'pm-shape', points: '0,-12 12,0 0,12 -12,0' }));
      (gate === 'par' ? [[0, -6, 0, 6], [-6, 0, 6, 0]] : [[-4, -4, 4, 4], [4, -4, -4, 4]])
        .forEach(([x1, y1, x2, y2]) => svg.append(s('line', { class: 'pm-glyph', x1, y1, x2, y2 })));
    } else if (kind === 'end') svg.append(s('circle', { class: 'pm-shape pm-end', r: 9 }));
    else svg.append(s('rect', { class: 'pm-shape', x: -12, y: -8, width: 24, height: 16, rx: 3 }));
    return svg;
  }

  window.ProcessMap = { mount, layout, gateOf, glyph, fa };
})();
