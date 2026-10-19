// The process designer: a process board's map in BPMN (processmap.js)
// with, beside it, the chosen node -- its name, who a step goes to, what it
// asks (in the form maker), and the routes out of it with their conditions.
// Steps, decisions (X), parallel gateways (+) and ends are added from the
// palette and connected by drawing; a BPMN 2.0 file from any tool can be
// brought in and the map taken out as one (internal/tasks/bpmn.go).
//
// Saving makes a new version of the map; a case already running keeps the
// version it started on (internal/tasks/flow.go), and the server checks the
// whole map again (process.go). A route's kind is the engine's (seq, xor,
// par, join): it is worked out again only for the nodes changed here, so an
// imported map keeps what it came with.
//
// tasks.js opens it: window.ProcessEdit.open(boardId, ctx) for a board's map,
// window.ProcessEdit.create(workspaceId, ctx) for a new process board.
(function () {
  'use strict';
  function h(tag, props, kids) {
    const n = document.createElement(tag);
    for (const [k, v] of Object.entries(props || {})) {
      if (v == null || v === false) continue;
      if (k === 'class') n.className = v;
      else if (k === 'text') n.textContent = v;
      else if (k.startsWith('on')) n.addEventListener(k.slice(2), v);
      else if (k in n && typeof n[k] !== 'function' && k !== 'list') n[k] = v; // list is read-only: an attribute
      else n.setAttribute(k, v);
    }
    for (const c of [].concat(kids == null ? [] : kids)) if (c != null && c !== false) n.append(c);
    return n;
  }
  function icon(name) {
    const NS = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(NS, 'svg');
    svg.setAttribute('class', 'ic');
    svg.setAttribute('aria-hidden', 'true');
    const u = document.createElementNS(NS, 'use');
    u.setAttribute('href', '#i-' + name);
    svg.append(u);
    return svg;
  }
  const iconBtn = (name, label, onclick, extra) => h('button', Object.assign({ type: 'button', class: 'btn btn-sm btn-ghost pd-ic', title: label, 'aria-label': label, onclick }, extra || {}), icon(name));
  async function api(path, opts) {
    const r = await fetch(path, Object.assign({ credentials: 'same-origin' }, opts || {}));
    const d = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(d.error || 'انجام نشد.');
    return d;
  }
  const json = (method, body) => ({ method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  const fa = s => String(s).replace(/\d/g, c => '۰۱۲۳۴۵۶۷۸۹'[c]);

  const MODES = [['users', 'این افراد'], ['requester', 'آغازکنندهٔ پرونده'], ['previous', 'کسی که گام پیش را انجام داد'],
    ['manager', 'مدیر آغازکننده'], ['department', 'اعضای یک واحد'], ['field', 'همکاری که در پرونده انتخاب شده']];
  const select = (pairs, value, onchange, label) => h('select', { class: 'input', style: 'width:auto', 'aria-label': label, onchange: e => onchange(e.target.value) },
    pairs.map(([v, n]) => h('option', { value: v, text: n, selected: String(v) === String(value) })));

  // Who a step goes to.
  function assignEditor(node, ctx) {
    const box = h('div', { class: 'tm-link' });
    const paint = () => {
      box.textContent = '';
      const a = node.assign = node.assign || { mode: 'requester' };
      box.append(h('span', { class: 'hint', text: 'سپرده به:' }), select(MODES, a.mode || 'users', v => { node.assign = { mode: v }; ctx.changed(); paint(); }, 'سپرده به'));
      if ((a.mode || 'users') === 'users') {
        const pick = h('select', { class: 'input', style: 'width:auto', 'aria-label': 'افزودن', onchange: e => {
          if (e.target.value) { a.users = (a.users || []).concat(e.target.value); ctx.changed(); paint(); }
        } }, [h('option', { value: '', text: '+ افزودن' })].concat(ctx.people.filter(p => !(a.users || []).includes(p.user)).map(p => h('option', { value: p.user, text: p.name || p.user }))));
        const chips = (a.users || []).map((u, i) => h('span', { class: 'tm-chip' }, [ctx.nameOf(u),
          h('button', { type: 'button', class: 'tm-chip-x', text: '×', 'aria-label': 'برداشتن ' + ctx.nameOf(u), onclick: () => { a.users.splice(i, 1); ctx.changed(); paint(); } })]));
        box.append(...chips, pick);
        if ((a.users || []).length > 1) box.append(h('span', { class: 'hint', text: 'یکی از اینها برمی‌دارد' }));
        if (a.group) box.append(h('span', { class: 'hint', text: 'گروه در نقشهٔ اصلی: ' + a.group }));
      }
      if (a.mode === 'department') {
        if (!ctx.departments.length) box.append(h('span', { class: 'hint', text: 'واحدی تعریف نشده است.' }));
        else {
          if (!ctx.departments.some(d => d.id === a.department)) a.department = ctx.departments[0].id;
          box.append(select(ctx.departments.map(d => [d.id, d.name]), a.department, v => { a.department = Number(v); ctx.changed(); }, 'کدام واحد'), h('span', { class: 'hint', text: 'یکی از اعضا برمی‌دارد' }));
        }
      }
      if (a.mode === 'field') {
        const people = ctx.personFields();
        if (!people.length) box.append(h('span', { class: 'hint', text: 'پرسشی از نوع «یکی از همکاران» در فرآیند نیست.' }));
        else {
          if (!people.some(f => f.key === a.field)) a.field = people[0].key;
          box.append(select(people.map(f => [f.key, f.label]), a.field, v => { a.field = v; ctx.changed(); }, 'کدام پرسش'));
        }
      }
      if (a.mode === 'manager') box.append(h('span', { class: 'hint', text: 'سرپرست واحد اصلی آغازکننده (صفحهٔ واحدها)' }));
    };
    paint();
    return box;
  }

  // ------------------------------------------------------------ the map
  const nodeOf = (p, id) => p.nodes.find(n => n.id === id);
  const outs = (p, id) => p.edges.filter(e => e.from === id).sort((a, b) => (a.order || 0) - (b.order || 0));
  const ins = (p, id) => p.edges.filter(e => e.to === id);
  const hasCond = e => (e.rules && e.rules.length) || (e.when && e.when.trim() && e.when.trim() !== '1');
  const isTask = n => n.kind === 'step' || n.kind === 'subprocess';
  // A node as lists name it. A gateway without a name is called after the
  // step that leads into it, so a long map's many gateways can be told apart.
  let current = null;
  const nameOf = n => {
    if (!n) return 'پایان فرآیند';
    if (n.name) return n.name;
    if (n.kind === 'end') return 'پایان';
    if (n.kind !== 'gateway') return n.id;
    const kind = n.gate === 'par' ? 'موازی' : 'تصمیم';
    let at = n, hops = 0;
    while (current && at && at.kind === 'gateway' && !at.name && hops < 8) {
      const before = current.edges.find(e => e.to === at.id);
      at = before && current.nodes.find(m => m.id === before.from);
      hops++;
    }
    if (!at || at === n) return 'دروازهٔ ' + kind;
    return kind + ' پس از «' + (at.name || at.id) + '»' + (hops > 1 ? ' (' + fa(hops) + ')' : '');
  };
  const KIND = { step: 'گام', subprocess: 'زیرفرآیند', end: 'رویداد پایان' };

  // Every question the process asks: what a route's condition may test.
  function questions(p) {
    const seen = new Map();
    for (const n of p.nodes) for (const f of n.fields || []) if (f.key && f.kind !== 'heading' && f.kind !== 'grid' && !seen.has(f.key)) seen.set(f.key, f);
    return [...seen.values()];
  }

  // The engine's kind for each route of the nodes changed here: a lone route
  // into a + gateway several reach waits there (join); a + gateway's several
  // routes all run (par); several routes, or one with a condition, take the
  // first that holds (xor); else the one route on (seq).
  function settle(p, touched) {
    const merging = id => { const n = nodeOf(p, id); return n && n.kind === 'gateway' && n.gate === 'par' && ins(p, id).length >= 2; };
    for (const id of [...touched]) { const n = nodeOf(p, id); if (n && n.kind === 'gateway') ins(p, id).forEach(e => touched.add(e.from)); }
    for (const id of touched) {
      const n = nodeOf(p, id);
      if (!n) continue;
      const es = outs(p, id);
      es.forEach((e, i) => {
        e.order = i + 1;
        if (es.length === 1 && e.to && merging(e.to)) e.kind = 'join';
        else if (n.kind === 'gateway' && n.gate === 'par' && es.length > 1) e.kind = 'par';
        else if (es.length > 1 || hasCond(e)) e.kind = 'xor';
        else e.kind = 'seq';
      });
    }
    touched.clear();
  }

  // What would stop the map being saved (errors), and what is worth a look.
  function check(p) {
    const errs = [], warns = [];
    const start = nodeOf(p, p.start);
    if (!start || start.kind !== 'step') errs.push({ text: 'فرآیند باید با یک گام آغاز شود: گامی را برگزینید و «گام نخست» کنید.' });
    for (const n of p.nodes) {
      if (isTask(n) && !String(n.name || '').trim()) errs.push({ id: n.id, text: 'گامی بی‌نام است.' });
      if (n.kind === 'gateway' && !outs(p, n.id).length) warns.push({ id: n.id, text: '«' + nameOf(n) + '» مسیری به جایی ندارد؛ پرونده‌ای که به آن برسد همان‌جا پایان می‌یابد.' });
      for (const f of n.fields || []) if (f.key && !/^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(f.key)) errs.push({ id: n.id, text: '«' + nameOf(n) + '»: شناسهٔ پرسش «' + f.key + '» پذیرفته نیست.' });
    }
    const keys = new Set(questions(p).map(f => f.key));
    p.edges.forEach(e => {
      if (e.kind === 'join' && outs(p, e.from).length > 1) errs.push({ id: e.from, text: '«' + nameOf(nodeOf(p, e.from)) + '»: مسیری که منتظر شاخه‌های دیگر می‌ماند باید تنها مسیر آن باشد.' });
      for (const r of e.rules || []) {
        if (!keys.has(r.field)) warns.push({ id: e.from, text: '«' + nameOf(nodeOf(p, e.from)) + '»: شرط مسیری به پرسشی بسته است که در فرآیند نیست.' });
        else if (r.op !== 'set' && r.op !== 'unset' && !(r.value || []).filter(v => String(v).trim()).length) errs.push({ id: e.from, text: '«' + nameOf(nodeOf(p, e.from)) + '»: شرط مسیری مقدار ندارد.' });
      }
    });
    for (const n of p.nodes) for (const when of ['before', 'after']) for (const a of n[when] || []) {
      const where = '«' + nameOf(n) + '»، اقدام: ';
      if (a.kind === 'set' && !/^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(a.field || '')) errs.push({ id: n.id, text: where + 'کدام پرسش را پر کند؟' });
      if (a.kind === 'set' && String(a.formula || '').trim().startsWith('=') && window.DocfindExpr) {
        const e = window.DocfindExpr.parse(window.DocfindExpr.formula(a.formula));
        if (e.error) errs.push({ id: n.id, text: where + 'فرمول خوانده نشد: ' + e.error });
      }
      if (a.kind === 'notify' && !String(a.text || '').trim()) errs.push({ id: n.id, text: where + 'پیامی ندارد.' });
      if (a.kind === 'webhook' && !/^https?:\/\/[^\s/]+/.test(a.url || '')) errs.push({ id: n.id, text: where + 'نشانی وب درستی نیست.' });
    }
    const reach = new Set(), stack = [p.start];
    while (stack.length) {
      const id = stack.pop();
      if (reach.has(id) || !nodeOf(p, id)) continue;
      reach.add(id);
      outs(p, id).forEach(e => e.to && stack.push(e.to));
    }
    for (const n of p.nodes) if (!reach.has(n.id)) warns.push({ id: n.id, text: '«' + nameOf(n) + '» از آغاز فرآیند در دسترس نیست.' });
    return { errs, warns };
  }

  function starter(name) {
    return { name, version: 0, start: 's1', nodes: [
      { id: 's1', kind: 'step', name: 'ثبت درخواست', assign: { mode: 'requester' }, fields: [{ key: 'q1', label: 'شرح درخواست', kind: 'textarea', required: true }] },
      { id: 's2', kind: 'step', name: 'بررسی و تأیید', assign: { mode: 'manager' } },
      { id: 'e1', kind: 'end', name: 'پایان' }],
    edges: [{ from: 's1', to: 's2', kind: 'seq', order: 1 }, { from: 's2', to: 'e1', kind: 'seq', order: 1 }] };
  }

  let dlg = null;
  function open(boardId, ctx) { return run({ boardId }, ctx); }
  function create(workspaceId, ctx) { return run({ workspaceId }, ctx); }

  async function run(where, ctx) {
    let p;
    if (where.boardId) {
      try { p = (await api('/api/tasks/boards/' + encodeURIComponent(where.boardId) + '/process')).process; } catch (e) { ctx.toast(e.message, true); return; }
      if (!p) { ctx.toast('این بورد فرآیند ندارد.', true); return; }
    } else p = starter('فرآیند تازه');
    p.nodes = p.nodes || []; p.edges = p.edges || [];
    current = p;
    for (const n of p.nodes) {
      for (const f of n.fields || []) f._old = !!where.boardId;
      if (n.kind === 'gateway') n.gate = window.ProcessMap.gateOf(p, n);
    }
    if (!dlg) { dlg = h('dialog', { class: 'tm-dialog pd-dialog', 'aria-label': 'طراحی فرآیند' }); document.body.append(dlg); }
    dlg.textContent = '';

    const touched = new Set(), history = [];
    let dirty = false, sel = null, warnings = [], closing = false;
    const snap = () => { history.push(JSON.stringify(p)); if (history.length > 40) history.shift(); undoBtn.disabled = false; };
    const changed = () => { dirty = true; };
    ctx = Object.assign({}, ctx, { changed: () => { changed(); map.draw(); } });
    ctx.personFields = () => questions(p).filter(f => f.kind === 'person');

    const side = h('div', { class: 'pd-side' });
    const host = h('div', { class: 'pd-canvas', tabindex: -1 });
    const msg = h('p', { class: 'err', role: 'alert', hidden: true });
    const say = text => { msg.textContent = text || ''; msg.hidden = !text; };
    const pool = () => questions(p);
    const label = e => {
      const parts = [];
      const many = outs(p, e.from).length > 1;
      if (e.kind === 'xor' && many) parts.push(fa(e.order || 0) + '.');
      if (e.rules && e.rules.length) parts.push(window.FormMaker.describe(e.rules, pool(), e.any));
      else if (e.when && e.when.trim() && e.when.trim() !== '1') parts.push(e.when.replace(/@@/g, ''));
      else if (e.kind === 'xor' && many) parts.push('در غیر این صورت');
      if (e.kind === 'join') parts.push('منتظر بقیه');
      const t = parts.join(' ');
      return t.length > 34 ? t.slice(0, 33) + '…' : t;
    };
    const flaggedIds = () => new Set(check(p).errs.map(x => x.id).filter(Boolean));
    let flags = new Set(), filter = '';
    const map = window.ProcessMap.mount(host, p, {
      label,
      flagged: n => flags.has(n.id),
      dim: n => !!filter && !nameOf(n).includes(filter),
      onSelect: next => { sel = next; panel(); },
      onChange: () => { changed(); },
      onConnect: (from, to, pt) => {
        snap();
        if (!to) {
          const n = { id: freshId('s'), kind: 'step', name: 'گام تازه', assign: { mode: 'requester' }, x: pt[0], y: pt[1] };
          p.nodes.push(n);
          to = n.id;
        }
        if (to === from || p.edges.some(e => e.from === from && e.to === to)) { history.pop(); return; }
        p.edges.push({ from, to, kind: 'seq', order: 999 });
        touched.add(from); touched.add(to);
        settle(p, touched); changed();
        sel = { node: from }; map.select(sel); redraw();
      },
      onDelete: id => remove(id),
    });
    ctx.changed = () => { changed(); draw(); };

    function freshId(prefix) { for (let i = p.nodes.length + 1; ; i++) if (!nodeOf(p, prefix + i)) return prefix + i; }
    function draw() { flags = flaggedIds(); map.draw(); }
    const redraw = () => { draw(); panel(); };

    function add(kind, gate) {
      snap();
      const from = sel && sel.node ? nodeOf(p, sel.node) : null;
      const n = { id: freshId({ step: 's', gateway: 'g', end: 'e' }[kind]), kind, name: kind === 'step' ? 'گام تازه' : kind === 'end' ? 'پایان' : '' };
      if (kind === 'step') n.assign = { mode: 'requester' };
      if (gate) n.gate = gate;
      const below = from ? outs(p, from.id).length : 0;
      [n.x, n.y] = from ? [from.x + below * 190, from.y + 130] : map.middle();
      p.nodes.push(n);
      if (from && from.kind !== 'end') { p.edges.push({ from: from.id, to: n.id, kind: 'seq', order: 999 }); touched.add(from.id); touched.add(n.id); settle(p, touched); }
      changed();
      sel = { node: n.id };
      map.select(sel); map.show(n.id); panel();
      const first = side.querySelector('input');
      if (first && kind === 'step') { first.focus(); first.select(); }
    }

    function remove(id) {
      if (id === p.start) { say('گام نخست را نمی‌توان برداشت؛ نخست گام دیگری را «گام نخست» کنید.'); return; }
      snap();
      p.edges.filter(e => e.to === id).forEach(e => touched.add(e.from));
      p.nodes = p.nodes.filter(n => n.id !== id);
      p.edges = p.edges.filter(e => e.from !== id && e.to !== id);
      settle(p, touched); changed();
      sel = null; map.select(null); redraw();
    }

    // ----------------------------------------------------- the side panel
    function panel() {
      side.textContent = '';
      const n = sel && sel.node ? nodeOf(p, sel.node) : sel && sel.edge != null && p.edges[sel.edge] ? nodeOf(p, p.edges[sel.edge].from) : null;
      if (!n) return overview();
      const kindName = n.kind === 'gateway' ? (n.gate === 'par' ? 'دروازهٔ موازی (+)' : 'دروازهٔ انحصاری (X)') : KIND[n.kind] || 'گام';
      side.append(h('div', { class: 'pd-side-head' }, [h('span', { class: 'chip chip-sm', text: kindName }),
        n.id === p.start ? h('span', { class: 'chip chip-sm chip-ok', text: 'گام نخست' }) : null,
        h('span', { class: 'spacer' }),
        iconBtn('x', 'بستن این بخش', () => { sel = null; map.select(null); panel(); })]));
      side.append(h('label', { class: 'tm-field' }, [h('span', { class: 'label', text: n.kind === 'gateway' || n.kind === 'end' ? 'نام (دلخواه)' : 'نام گام' }),
        h('input', { class: 'input', value: n.name || '', oninput: e => { n.name = e.target.value; changed(); draw(); } })]));
      if (n.kind === 'gateway') {
        side.append(h('div', { class: 'pd-seg', role: 'radiogroup', 'aria-label': 'نوع دروازه' }, [['xor', 'تصمیم: نخستین مسیری که شرطش درست است'], ['par', 'موازی: همهٔ مسیرهای درست، هم‌زمان']].map(([v, t]) =>
          h('label', { class: 'pd-seg-opt' }, [h('input', { type: 'radio', name: 'pd-gate', value: v, checked: n.gate === v, onchange: () => {
            snap(); n.gate = v; touched.add(n.id); settle(p, touched); changed(); redraw();
          } }), window.ProcessMap.glyph('gateway', v), ' ' + t]))));
      }
      if (isTask(n)) {
        if (n.id !== p.start) side.append(h('button', { type: 'button', class: 'btn btn-sm pd-start', text: 'گام نخست کن', onclick: () => { snap(); p.start = n.id; changed(); redraw(); } }));
        side.append(h('h3', { text: 'چه کسی انجام می‌دهد' }), assignEditor(n, ctx));
        side.append(h('h3', { text: 'پرسش‌ها' }), h('div', { class: 'tm-link' }, [h('span', { class: 'hint', text: fa((n.fields || []).length) + ' پرسش' }),
          h('button', { type: 'button', class: 'btn btn-sm', text: 'ویرایش پرسش‌ها', onclick: async () => {
            n.fields = n.fields || [];
            const known = p.nodes.filter(m => m !== n).flatMap(m => m.fields || []);
            const before = JSON.stringify(p);
            if (await window.FormMaker.open(n.fields, { title: 'پرسش‌های «' + nameOf(n) + '»', people: ctx.people, modes: true, locked: f => !!f._old, known })) {
              history.push(before); undoBtn.disabled = false; changed(); redraw();
            }
          } })]));
      }
      if (isTask(n)) side.append(actions(n, 'before', 'پیش از گام (وقتی باز می‌شود)'), actions(n, 'after', 'پس از گام (وقتی انجام شد)'));
      if (n.kind !== 'end') side.append(routes(n));
      if (n.id !== p.start) {
        const del = h('button', { type: 'button', class: 'btn btn-sm btn-ghost pd-danger', onclick: () => {
          if (!del.dataset.sure) { del.dataset.sure = '1'; del.lastChild.textContent = ' برای برداشتن دوباره بزنید'; return; }
          remove(n.id);
        } }, [icon('trash-2'), h('span', { text: ' برداشتن از نقشه' })]);
        side.append(del);
      }
    }

    // What a step does by itself (internal/tasks/actions.go): set a question,
    // tell people, or post the case to a web address -- each when its rules hold.
    const ACTS = [['set', 'پر کردن یک پرسش'], ['notify', 'خبر دادن'], ['webhook', 'فرستادن به نشانی وب']];
    function actions(n, when, title) {
      const list = n[when] = n[when] || [];
      const box = h('section', { class: 'pd-routes' }, [h('h3', { text: title })]);
      const keys = h('datalist', { id: 'pd-keys-' + when }, questions(p).map(f => h('option', { value: f.key, label: f.label })));
      box.append(keys);
      const redo = () => { if (!list.length) delete n[when]; changed(); redraw(); };
      list.forEach((a, i) => {
        const row = h('div', { class: 'pd-route' }, [h('div', { class: 'tm-link' }, [
          select(ACTS, a.kind, v => { snap(); list[i] = { kind: v }; if (v === 'notify') list[i].to = { mode: 'requester' }; redo(); }, 'نوع اقدام'),
          h('span', { class: 'spacer' }),
          iconBtn('trash-2', 'برداشتن اقدام', () => { snap(); list.splice(i, 1); redo(); })])]);
        if (a.kind === 'set') {
          row.append(h('div', { class: 'tm-link' }, [h('span', { class: 'hint', text: 'پرسش:' }),
            h('input', { class: 'input', dir: 'ltr', list: keys.id, value: a.field || '', placeholder: 'شناسهٔ پرسش', style: 'width:10em', 'aria-label': 'کدام پرسش',
              oninput: e => { a.field = e.target.value.trim(); changed(); } }),
            h('span', { class: 'hint', text: 'مقدار:' }),
            h('input', { class: 'input pd-code', dir: 'auto', value: a.formula || '', placeholder: 'متن، یا = فرمول', 'aria-label': 'مقدار',
              oninput: e => { a.formula = e.target.value; changed(); } })]));
        } else if (a.kind === 'notify') {
          const holder = { assign: a.to || { mode: 'requester' } };
          const sync = Object.assign({}, ctx, { changed: () => { a.to = holder.assign; changed(); } });
          a.to = holder.assign;
          row.append(assignEditor(holder, sync),
            h('textarea', { class: 'input', rows: 2, value: a.text || '', placeholder: 'پیام؛ {amount} جای پاسخ پرسش amount را می‌گیرد', 'aria-label': 'پیام',
              oninput: e => { a.text = e.target.value; changed(); } }));
        } else {
          row.append(h('input', { class: 'input pd-code', dir: 'ltr', value: a.url || '', placeholder: 'https://…', 'aria-label': 'نشانی وب',
            oninput: e => { a.url = e.target.value.trim(); changed(); } }),
          h('span', { class: 'hint', text: 'پرونده به این نشانی فرستاده می‌شود؛ همان نشانی‌هایی پذیرفته است که وب‌هوک بوردها.' }));
        }
        const paint = () => { if (!(a.if || []).length) delete a.if; changed(); redraw(); };
        paint.soon = () => changed();
        row.append(window.FormMaker.rules(a, 'if', 'فقط وقتی:', pool(), paint, 'any'));
        box.append(row);
      });
      box.append(h('button', { type: 'button', class: 'btn btn-sm', text: '+ اقدام', onclick: () => { snap(); list.push({ kind: 'set' }); redo(); } }));
      return box;
    }

    function routes(n) {
      const box = h('section', { class: 'pd-routes' });
      const es = outs(p, n.id);
      const kind = es[0] && es[0].kind;
      const how = !es.length ? (isTask(n) ? 'مسیری نیست: پرونده پس از این گام پایان می‌یابد.' : 'مسیری نیست.')
        : kind === 'par' ? 'همهٔ مسیرهایی که شرطشان درست است، هم‌زمان پیش می‌روند.'
          : kind === 'join' ? 'اینجا منتظر می‌ماند تا شاخه‌های موازی دیگر هم برسند، سپس پیش می‌رود.'
            : es.length > 1 ? 'به ترتیب: نخستین مسیری که شرطش درست باشد. اگر هیچ‌کدام درست نباشد، گام انجام نمی‌شود و علتش گفته می‌شود.'
              : 'همیشه به این مسیر می‌رود.';
      box.append(h('h3', { text: n.kind === 'gateway' ? 'مسیرهای این دروازه' : 'پس از این گام' }), h('p', { class: 'hint', text: how }));
      const targets = [['', 'پایان فرآیند']].concat(p.nodes.filter(m => m.id !== n.id).map(m => [m.id, nameOf(m) + (m.kind === 'gateway' ? ' (دروازه)' : '')]));
      es.forEach((e, i) => {
        const at = p.edges.indexOf(e);
        const row = h('div', { class: 'pd-route' + (sel && sel.edge === at ? ' is-sel' : '') });
        const top = h('div', { class: 'tm-link' }, [
          h('span', { class: 'pd-order', text: es.length > 1 && kind !== 'par' ? fa(i + 1) : '' }),
          select(targets, e.to || '', v => { snap(); e.to = v; touched.add(n.id); touched.add(v); settle(p, touched); changed(); redraw(); }, 'مقصد مسیر'),
          es.length > 1 && kind !== 'par' ? iconBtn('arrow-up', 'بالاتر', () => { if (!i) return; snap(); [es[i - 1].order, e.order] = [e.order, es[i - 1].order]; touched.add(n.id); settle(p, touched); changed(); redraw(); }, { disabled: !i }) : null,
          es.length > 1 && kind !== 'par' ? iconBtn('arrow-down', 'پایین‌تر', () => { if (i === es.length - 1) return; snap(); [es[i + 1].order, e.order] = [e.order, es[i + 1].order]; touched.add(n.id); settle(p, touched); changed(); redraw(); }, { disabled: i === es.length - 1 }) : null,
          iconBtn('trash-2', 'برداشتن مسیر', () => { snap(); p.edges.splice(at, 1); touched.add(n.id); if (e.to) touched.add(e.to); settle(p, touched); changed(); sel = { node: n.id }; map.select(sel); redraw(); })]);
        row.append(top);
        if (es.length > 1 || n.kind === 'gateway' || hasCond(e)) {
          if (e.when && e.when.trim() && e.when.trim() !== '1' && !(e.rules && e.rules.length)) {
            row.append(h('div', { class: 'tm-link' }, [h('span', { class: 'hint', text: 'شرط پیشرفته:' }),
              h('input', { class: 'input pd-code', dir: 'ltr', value: e.when, 'aria-label': 'شرط پیشرفته', oninput: ev => { e.when = ev.target.value; touched.add(n.id); changed(); draw(); } }),
              h('button', { type: 'button', class: 'btn btn-sm btn-ghost', text: 'ساده کن', title: 'شرط را با پرسش‌ها بسازید', onclick: () => { snap(); e.when = ''; e.rules = []; touched.add(n.id); settle(p, touched); changed(); redraw(); } })]));
          } else {
            e.rules = e.rules || [];
            const paint = () => { if (!(e.rules || []).length) delete e.rules; touched.add(n.id); settle(p, touched); changed(); redraw(); };
            paint.soon = () => { changed(); draw(); };
            row.append(window.FormMaker.rules(e, 'rules', kind === 'par' ? 'این شاخه وقتی:' : 'این مسیر وقتی:', pool(), paint, 'any'));
            if (!(e.rules || []).length) row.append(h('span', { class: 'hint', text: kind === 'par' ? 'بی‌شرط: همیشه' : 'بی‌شرط: در غیر این صورت' }));
          }
        }
        if (e.kind === 'join') row.append(h('span', { class: 'hint', text: 'منتظر شاخه‌های دیگر می‌ماند.' }));
        box.append(row);
      });
      box.append(h('button', { type: 'button', class: 'btn btn-sm', text: '+ مسیر', onclick: () => {
        snap();
        const to = p.nodes.find(m => m.id !== n.id && !es.some(e => e.to === m.id));
        p.edges.push({ from: n.id, to: to ? to.id : '', kind: 'seq', order: 999 });
        touched.add(n.id); if (to) touched.add(to.id); settle(p, touched); changed(); redraw();
      } }));
      return box;
    }

    function overview() {
      const { errs, warns } = check(p);
      const steps = p.nodes.filter(isTask).length;
      side.append(h('h3', { text: 'نقشهٔ فرآیند' }),
        h('p', { class: 'hint', text: (p.version ? 'نسخهٔ ' + fa(p.version) + ' · ' : '') + fa(steps) + ' گام، ' + fa(p.nodes.length - steps) + ' دروازه و پایان، ' + fa(p.edges.length) + ' مسیر.' }),
        h('p', { class: 'hint', text: 'گره‌ای را برگزینید تا نام، انجام‌دهنده، پرسش‌ها و مسیرهایش اینجا بیاید. از نوار بالا گام و دروازه بیفزایید؛ گره تازه زیر گره برگزیده می‌نشیند و به آن وصل می‌شود. برای کشیدن مسیر، دایرهٔ زیر گره برگزیده را تا گره دیگر بکشید؛ رها کردن در جای خالی گام تازه‌ای می‌سازد.' }));
      const list = (items, cls, title) => {
        if (!items.length) return;
        side.append(h('h3', { text: title }), h('ul', { class: 'pd-issues ' + cls }, items.map(x => h('li', {}, x.id
          ? h('button', { type: 'button', class: 'pd-link', text: x.text, onclick: () => { sel = { node: x.id }; map.select(sel); map.show(x.id); panel(); } })
          : x.text))));
      };
      list(errs, 'is-bad', 'پیش از ذخیره درست کنید');
      list(warns, 'is-warn', 'بهتر است نگاهی بیندازید');
      list(warnings.map(t => ({ text: t })), 'is-warn', 'از فایل BPMN');
      if (!errs.length && !warns.length && !warnings.length) side.append(h('p', { class: 'pd-ok', text: 'نقشه آمادهٔ ذخیره است.' }));
    }

    // ----------------------------------------------------------- the bars
    const nameIn = h('input', { class: 'input pd-name', value: p.name || '', 'aria-label': 'نام فرآیند', oninput: e => { p.name = e.target.value; changed(); } });
    const fromHint = h('span', { class: 'hint', text: p.source ? 'از ' + p.source.split(':')[0] : '' });
    const undoBtn = iconBtn('undo-2', 'برگرداندن آخرین تغییر (Ctrl+Z)', () => {
      if (!history.length) return;
      const back = JSON.parse(history.pop());
      Object.keys(p).forEach(k => delete p[k]);
      Object.assign(p, back);
      undoBtn.disabled = !history.length;
      nameIn.value = p.name || '';
      changed(); sel = null; map.select(null); redraw();
    }, { disabled: true });
    const file = h('input', { type: 'file', accept: '.bpmn,.xml,application/xml,text/xml', hidden: true, onchange: async () => {
      const f = file.files[0];
      file.value = '';
      if (!f) return;
      try {
        const d = await api('/api/tasks/process/bpmn/import', { method: 'POST', headers: { 'Content-Type': 'application/xml' }, body: await f.text() });
        snap();
        const next = d.process;
        next.name = next.name || p.name;
        next.version = p.version;
        Object.keys(p).forEach(k => delete p[k]);
        Object.assign(p, next);
        p.nodes = p.nodes || []; p.edges = p.edges || [];
        p.nodes.forEach(n => { if (n.kind === 'gateway') n.gate = window.ProcessMap.gateOf(p, n); });
        nameIn.value = p.name || '';
        fromHint.textContent = 'از فایل ' + f.name;
        warnings = (d.warnings || []).concat(d.problem ? [d.problem] : []);
        if (!p.nodes.some(n => n.x || n.y)) map.layout(true);
        changed(); sel = null; map.select(null); redraw(); map.fit();
        ctx.toast('نقشه از «' + f.name + '» آمد؛ تا ذخیره نکنید جایگزین نمی‌شود.');
      } catch (e) { say(e.message); }
    } });
    const exportBPMN = async () => {
      try {
        const r = await fetch('/api/tasks/process/bpmn/export', Object.assign({ credentials: 'same-origin' }, json('POST', clean())));
        if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || 'انجام نشد.');
        const url = URL.createObjectURL(await r.blob());
        const a = h('a', { href: url, download: (p.name || 'process').replace(/[\\/:*?"<>|]/g, '-') + '.bpmn' });
        document.body.append(a); a.click(); a.remove();
        setTimeout(() => URL.revokeObjectURL(url), 5000);
      } catch (e) { say(e.message); }
    };
    const search = h('input', { class: 'input pd-search', type: 'search', placeholder: 'جست‌وجو در نقشه', 'aria-label': 'جست‌وجو در نقشه', oninput: e => {
      filter = e.target.value.trim(); draw();
    }, onkeydown: e => {
      if (e.key !== 'Enter' || !filter) return;
      const hit = p.nodes.find(n => nameOf(n).includes(filter));
      if (hit) { sel = { node: hit.id }; map.select(sel); map.show(hit.id); panel(); }
    } });
    const tool = (kind, gate, text) => h('button', { type: 'button', class: 'btn btn-sm pd-tool', onclick: () => add(kind, gate) }, [window.ProcessMap.glyph(kind, gate), h('span', { text })]);

    const clean = () => JSON.parse(JSON.stringify(p, (k, v) => (k === '_old' ? undefined : v)));
    let boardId = where.boardId;
    const save = async ev => {
      const { errs } = check(p);
      if (errs.length) { sel = null; map.select(null); panel(); say('پیش از ذخیره: ' + errs[0].text); return; }
      if (!String(p.name || '').trim()) { say('فرآیند نامی می‌خواهد.'); nameIn.focus(); return; }
      ev.target.disabled = true; say('');
      try {
        if (!boardId) boardId = (await api('/api/tasks/boards', json('POST', { workspace_id: where.workspaceId, name: p.name, kind: 'kanban' }))).id;
        const r = await api('/api/tasks/boards/' + encodeURIComponent(boardId) + '/process', json('PUT', clean()));
        dirty = false; closing = true;
        dlg.close();
        ctx.toast((where.boardId ? 'نسخهٔ ' : 'فرآیند ساخته شد؛ نسخهٔ ') + fa(r.version) + ' ذخیره شد.');
        if (ctx.onSaved) ctx.onSaved(boardId);
      } catch (e) { ev.target.disabled = false; say(e.message); }
    };
    const closeBtn = h('button', { type: 'button', class: 'btn btn-ghost', text: 'بستن', onclick: () => tryClose() });
    function tryClose() {
      if (dirty && !closing) { closing = true; closeBtn.textContent = 'تغییرات ذخیره نشده؛ برای بستن دوباره بزنید'; return; }
      dlg.close();
    }

    dlg.append(
      h('div', { class: 'pd-head' }, [h('h2', { text: where.boardId ? 'طراحی فرآیند' : 'فرآیند تازه' }), nameIn, fromHint,
        h('span', { class: 'spacer' }), undoBtn,
        h('button', { type: 'button', class: 'btn btn-sm btn-ghost', onclick: () => file.click() }, [icon('upload'), h('span', { text: ' ورود BPMN' })]),
        h('button', { type: 'button', class: 'btn btn-sm btn-ghost', onclick: exportBPMN }, [icon('download'), h('span', { text: ' خروجی BPMN' })]), file]),
      h('div', { class: 'pd-tools', role: 'toolbar', 'aria-label': 'افزودن به نقشه' }, [
        tool('step', '', 'گام'), tool('gateway', 'xor', 'تصمیم'), tool('gateway', 'par', 'موازی'), tool('end', '', 'پایان'),
        h('span', { class: 'spacer' }), search,
        iconBtn('minus', 'کوچک‌تر', () => map.zoomBy(1 / 1.2)), iconBtn('plus', 'بزرگ‌تر', () => map.zoomBy(1.2)),
        iconBtn('maximize', 'همهٔ نقشه در دید', () => map.fit()),
        h('button', { type: 'button', class: 'btn btn-sm btn-ghost', text: 'چیدن دوباره', title: 'جای همهٔ گره‌ها از نو', onclick: () => { snap(); map.layout(true); changed(); map.fit(); } })]),
      h('div', { class: 'pd-body' }, [side, host]),
      h('div', { class: 'tm-dialog-foot' }, [h('button', { type: 'button', class: 'btn btn-primary', text: where.boardId ? 'ذخیره، نسخهٔ تازه' : 'ساختن فرآیند', onclick: save }),
        msg, h('span', { class: 'spacer' }), closeBtn]));
    dlg.oncancel = e => { e.preventDefault(); tryClose(); };
    dlg.onkeydown = e => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'z' && !/^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName)) { e.preventDefault(); undoBtn.click(); }
    };
    flags = flaggedIds();
    panel();
    dlg.showModal();
    requestAnimationFrame(() => map.open());
  }

  window.ProcessEdit = { open, create };
})();
