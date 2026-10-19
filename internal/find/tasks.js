// The task manager's page (/tasks; internal/tasks is the model
// and its rules, internal/find/tasksroutes.go the API). Workspaces and their
// boards down the side; a board as a Kanban, a table, a list, a Jalali
// calendar, a timeline, the team's workload or its activity; "my work" across
// every board; and one item open in a panel with its checklist, subtasks,
// links, dependencies and updates.
//
// Dates are stored as days (2026-09-30) and shown and typed in the Jalali
// calendar (datepick.js). Everything that came from a person is set as text,
// never as markup.
(function () {
  'use strict';
  const $ = id => document.getElementById(id);
  function h(tag, props, kids) {
    const n = document.createElement(tag);
    for (const [k, v] of Object.entries(props || {})) {
      if (v == null || v === false) continue;
      if (k === 'class') n.className = v;
      else if (k === 'text') n.textContent = v;
      else if (k.startsWith('on')) n.addEventListener(k.slice(2), v);
      else if (k === 'style') n.style.cssText = v;
      else if (k in n && typeof n[k] !== 'function') n[k] = v;
      else n.setAttribute(k, v);
    }
    for (const c of [].concat(kids == null ? [] : kids)) if (c != null && c !== false) n.append(c);
    return n;
  }
  // Element.append writes a null or false argument as the text "null": the
  // optional parts of a panel go through put() instead.
  const put = (el, ...kids) => el.append(...kids.filter(k => k != null && k !== false));
  const fa = new Intl.NumberFormat('fa-IR');
  const faN = n => fa.format(n || 0);
  const DP = () => window.docfindDatePicker;
  const PRI = { low: 'کم', normal: 'عادی', high: 'زیاد', critical: 'بحرانی' };
  const PRI_COLOR = { low: '#6B7C8F', normal: '#3B82F6', high: '#E5641D', critical: '#DC2626' };
  const LINK_KINDS = { customer: 'مشتری', order: 'سفارش', email: 'ایمیل', document: 'سند', tender: 'مناقصه',
    supplier: 'تأمین‌کننده', contract: 'قرارداد', page: 'صفحه' };
  const DEP_KINDS = { fs: 'پس از پایانِ', ss: 'هم‌زمان با شروعِ', ff: 'هم‌زمان با پایانِ', sf: 'پایان پس از شروعِ',
    blocks: 'مسدود می‌کند', related: 'مرتبط با', duplicate: 'تکراریِ' };

  // ----------------------------------------------------------------- dates
  const pad = n => String(n).padStart(2, '0');
  function toJalali(iso) {
    if (!iso || !DP()) return iso || '';
    const [y, m, d] = iso.split('-').map(Number);
    const [jy, jm, jd] = DP().gregorianToJalali(y, m, d);
    return jy + '/' + pad(jm) + '/' + pad(jd);
  }
  function fromJalali(s) {
    if (!s || !DP()) return '';
    const p = DP().parseDate(s);
    if (!p) return null;
    const [y, m, d] = p[0] < 1700 ? DP().jalaliToGregorian(p[0], p[1], p[2]) : p;
    return y + '-' + pad(m) + '-' + pad(d);
  }
  const faDate = iso => iso ? toJalali(iso).replace(/\d/g, c => '۰۱۲۳۴۵۶۷۸۹'[c]) : '';
  const todayISO = () => { const n = new Date(); return n.getFullYear() + '-' + pad(n.getMonth() + 1) + '-' + pad(n.getDate()); };
  const addDays = (iso, k) => { const d = new Date(iso + 'T12:00:00'); d.setDate(d.getDate() + k); return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()); };
  const daysBetween = (a, b) => Math.round((new Date(b + 'T12:00:00') - new Date(a + 'T12:00:00')) / 86400000);

  // ------------------------------------------------------------------- api
  async function api(path, opts) {
    const r = await fetch(path, Object.assign({ credentials: 'same-origin' }, opts || {}));
    if (r.status === 401) { location.replace('/login?next=' + encodeURIComponent(location.pathname + location.search)); throw new Error('signed out'); }
    const d = await r.json().catch(() => ({}));
    if (!r.ok) { const e = new Error(d.error || 'انجام نشد.'); e.status = r.status; e.data = d; throw e; }
    return d;
  }
  const send = (method, path, body) => api(path, { method, headers: { 'Content-Type': 'application/json' }, body: body == null ? undefined : JSON.stringify(body) });

  let toastTimer = 0;
  function toast(text, bad) {
    const t = $('tmToast');
    t.textContent = text;
    t.className = 'tm-toast' + (bad ? ' bad' : '');
    t.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { t.hidden = true; }, 3200);
  }

  // ----------------------------------------------------------------- state
  const S = { me: '', admin: false, workspaces: [], people: [], current: null, view: 'kanban',
    board: null, workspace: null, role: '', items: [], filter: { q: '', who: '', label: '', pri: '' }, sort: null, calMonth: null };
  const nameOf = u => { const p = S.people.find(x => x.user === u); return p ? p.name || u : u; };
  const initials = u => { const n = nameOf(u).trim().split(/\s+/); return (n[0] || '?').slice(0, 1) + (n[1] ? n[1].slice(0, 1) : ''); };
  const canEdit = () => S.admin || S.role === 'owner' || S.role === 'member';
  const canOwn = () => S.admin || S.role === 'owner';
  const statusOf = key => (S.board.statuses || []).find(s => s.key === key) || { name: key };

  // The views that are not one board: everything of mine, the steps of the
  // processes that wait for me (taskflow.js), the figures, what waits on my
  // approval, the intake forms, and the timesheet.
  const SPECIAL = [['my', 'کارهای من'], ['flow', 'صندوق کار'], ['dash', 'داشبورد'], ['approvals', 'تأییدهای من'], ['forms', 'فرم‌ها'], ['time', 'زمان‌کرد']];
  const isSpecial = k => SPECIAL.some(([x]) => x === k);
  function readURL() {
    const q = new URLSearchParams(location.search);
    return { board: q.get('board'), item: q.get('item'), view: q.get('view') };
  }
  function writeURL(push) {
    const q = new URLSearchParams();
    if (isSpecial(S.current)) q.set('board', S.current);
    else if (S.board) q.set('board', S.board.id);
    if (S.view !== 'kanban' && !isSpecial(S.current)) q.set('view', S.view);
    const url = '/tasks' + (q.toString() ? '?' + q : '');
    (push ? history.pushState : history.replaceState).call(history, {}, '', url);
  }

  // ------------------------------------------------------------ the side
  async function loadAll() {
    const d = await api('/api/tasks');
    S.me = d.me; S.admin = !!d.admin; S.workspaces = d.workspaces || []; S.people = d.people || []; S.departments = d.departments || [];
    drawSide();
  }
  function drawSide() {
    const side = $('tmSide');
    side.textContent = '';
    for (const [k, name] of SPECIAL) {
      if (S.flowOnly && k !== 'flow') continue;
      side.append(h('button', { type: 'button', class: 'tm-side-item' + (S.current === k ? ' on' : ''), text: name,
        onclick: () => openSpecial(k, true) }));
    }
    for (const w of S.workspaces) {
      const box = h('div', { class: 'tm-ws' });
      box.append(h('div', { class: 'tm-ws-head' }, [
        h('b', { text: w.name }),
        w.department ? h('span', { class: 'hint', text: w.department }) : null,
        h('span', { class: 'spacer' }),
        (S.admin || w.role === 'owner') ? h('button', { type: 'button', class: 'btn btn-sm btn-ghost', text: 'تنظیم', title: 'تنظیم فضای کار',
          onclick: () => editWorkspace(w) }) : null,
      ]));
      for (const b of w.boards || []) {
        box.append(h('button', { type: 'button', class: 'tm-side-item' + (S.board && S.board.id === b.id && S.current !== 'my' ? ' on' : ''),
          text: (b.kind === 'project' ? '◆ ' : '▦ ') + b.name, onclick: () => openBoard(b.id, true) }));
      }
      if (S.admin || w.role === 'owner' || w.role === 'member') {
        box.append(h('button', { type: 'button', class: 'btn btn-sm btn-ghost tm-add', text: '+ بورد تازه', onclick: () => editBoard({ workspace_id: w.id }) }));
      }
      // A process of its own, drawn in the designer: for the workspace's owner.
      if (S.admin || w.role === 'owner') {
        box.append(h('button', { type: 'button', class: 'btn btn-sm btn-ghost tm-add', text: '+ فرآیند تازه', onclick: () => window.ProcessEdit.create(w.id,
          { people: S.people, departments: S.departments || [], nameOf, toast, onSaved: async id => { await loadAll(); openBoard(id, true); } }) }));
      }
      side.append(box);
    }
    if (S.flowOnly) return;
    side.append(h('button', { type: 'button', class: 'btn btn-sm tm-add', text: '+ فضای کار تازه', onclick: () => editWorkspace({ members: [] }) }));
    if (!S.workspaces.length) side.append(h('p', { class: 'hint', text: 'هنوز فضای کاری ندارید. یکی بسازید: مثلاً «فروش» یا «فنی».' }));
  }

  // --------------------------------------------------------- one board
  async function openBoard(id, push) {
    S.current = id;
    try {
      const d = await api('/api/tasks/boards/' + encodeURIComponent(id));
      S.board = d.board; S.workspace = d.workspace; S.role = d.role; S.items = d.items || []; S.buttons = d.buttons || [];
    } catch (e) { toast(e.message, true); return; }
    writeURL(push);
    drawSide();
    drawHead();
    drawView();
  }
  function drawHead() {
    const head = $('tmHead');
    head.textContent = '';
    if (isSpecial(S.current)) {
      const hint = { my: 'هرچه به شما سپرده شده، در همهٔ بوردها', flow: 'گام‌هایی از فرآیندها که منتظر شماست، و آغاز فرآیند تازه', dash: 'کارهای همهٔ بوردهایی که می‌بینید', approvals: 'آنچه منتظر تصمیم شماست',
        forms: 'درخواست به یک تیم، با فرم آن', time: 'زمانی که روی کارها گذاشته شده است' }[S.current];
      head.append(h('div', { class: 'tm-title' }, [h('h2', { text: SPECIAL.find(([k]) => k === S.current)[1] }), h('span', { class: 'hint', text: hint })]));
      return;
    }
    const views = [['kanban', 'کانبان'], ['table', 'جدول'], ['list', 'فهرست'], ['calendar', 'تقویم'], ['timeline', 'زمان‌بندی'],
      ['workload', 'بار کاری'], ['activity', 'فعالیت']];
    head.append(
      h('div', { class: 'tm-title' }, [
        h('h2', { text: S.board.name }),
        h('span', { class: 'hint', text: S.workspace.name + (S.role === 'viewer' ? ' · فقط خواندن' : '') }),
        h('span', { class: 'spacer' }),
        S.board.process && (S.role === 'owner' || S.admin) ? h('button', { type: 'button', class: 'btn', text: 'طراحی فرآیند',
          onclick: () => window.ProcessEdit.open(S.board.id, { people: S.people, departments: S.departments || [], nameOf, toast, onSaved: id => openBoard(id, false) }) }) : null,
        S.board.process ? h('button', { type: 'button', class: 'btn btn-primary', text: '+ آغاز پرونده', onclick: () => window.TaskFlow.startCase(S.board.id) })
          : canEdit() ? h('button', { type: 'button', class: 'btn btn-primary', text: '+ کار تازه', onclick: () => openItem({ board_id: S.board.id }) }) : null,
        canEdit() ? h('button', { type: 'button', class: 'btn', text: 'تنظیم بورد', onclick: () => editBoard(S.board) }) : null,
      ]),
      h('div', { class: 'tm-tabs', role: 'tablist' }, views.map(([k, n]) => h('button', { type: 'button', role: 'tab',
        'aria-selected': String(S.view === k), text: n, onclick: () => { S.view = k; writeURL(true); drawHead(); drawView(); } }))),
      filterBar(),
    );
  }
  function filterBar() {
    const labels = (S.board.labels || []).map(l => l.name);
    const who = [...new Set(S.items.flatMap(i => i.assignees || []))];
    const sel = (id, first, opts, key) => {
      const s = h('select', { class: 'input', id, onchange: () => { S.filter[key] = s.value; drawView(); } },
        [h('option', { value: '', text: first })].concat(opts.map(([v, t]) => h('option', { value: v, text: t }))));
      s.value = S.filter[key] || '';
      return s;
    };
    const q = h('input', { class: 'input', id: 'tmQ', placeholder: 'جست‌وجو در کارها', value: S.filter.q,
      oninput: () => { S.filter.q = q.value; drawView(); } });
    return h('div', { class: 'tm-filter' }, [q,
      sel('tmWho', 'همهٔ افراد', who.map(u => [u, nameOf(u)]), 'who'),
      sel('tmLabel', 'همهٔ برچسب‌ها', labels.map(l => [l, l]), 'label'),
      sel('tmPri', 'همهٔ اولویت‌ها', Object.entries(PRI), 'pri')]);
  }
  function visible(items) {
    const f = S.filter, q = f.q.trim().toLowerCase();
    return items.filter(i => (!q || (i.title + ' ' + (i.description || '')).toLowerCase().includes(q)) &&
      (!f.who || (i.assignees || []).includes(f.who)) && (!f.label || (i.labels || []).includes(f.label)) &&
      (!f.pri || i.priority === f.pri));
  }
  const topItems = () => visible(S.items.filter(i => !i.parent_id));
  const subsOf = id => S.items.filter(i => i.parent_id === id);
  function drawView() {
    const v = $('tmView');
    v.textContent = '';
    if (isSpecial(S.current)) return;
    ({ kanban, table, list, calendar, timeline, workload, activity }[S.view] || kanban)(v);
  }

  // Saving an item: the version the page read goes with it, and a newer one
  // on the server is shown, not overwritten.
  async function saveItem(it, quiet) {
    try {
      const saved = await send('POST', '/api/tasks/items', it);
      const i = S.items.findIndex(x => x.id === saved.id);
      if (i >= 0) S.items[i] = saved; else S.items.push(saved);
      if (!quiet) toast('ذخیره شد.');
      return saved;
    } catch (e) {
      if (e.status === 409 && e.data && e.data.current) {
        const i = S.items.findIndex(x => x.id === e.data.current.id);
        if (i >= 0) S.items[i] = e.data.current;
        toast('کسی دیگر همین کار را تغییر داده بود؛ نسخهٔ تازه نشان داده شد.', true);
        drawView();
      } else toast(e.message, true);
      return null;
    }
  }

  // --------------------------------------------------------------- cards
  function card(it) {
    const subs = subsOf(it.id);
    const checks = it.checklist || [];
    const overdue = it.due && it.due < todayISO() && !statusOf(it.status).done;
    const c = h('article', { class: 'tm-card', draggable: canEdit(), tabIndex: 0,
      style: 'border-inline-start-color:' + (it.color || PRI_COLOR[it.priority] || 'transparent'),
      onclick: () => openItem(it), onkeydown: e => { if (e.key === 'Enter') openItem(it); },
      ondragstart: e => { e.dataTransfer.setData('text/plain', it.id); e.dataTransfer.effectAllowed = 'move'; c.classList.add('drag'); },
      ondragend: () => c.classList.remove('drag') }, [
      (it.labels || []).length ? h('div', { class: 'tm-labels' }, it.labels.map(l => {
        const def = (S.board.labels || []).find(x => x.name === l);
        return h('span', { class: 'tm-label', text: l, style: def && def.color ? 'background:' + def.color : '' });
      })) : null,
      h('b', { class: 'tm-card-title', text: it.title }),
      h('div', { class: 'tm-badges' }, [
        it.priority !== 'normal' ? h('span', { class: 'tm-badge', style: 'color:' + PRI_COLOR[it.priority], text: PRI[it.priority] }) : null,
        it.due ? h('span', { class: 'tm-badge' + (overdue ? ' late' : ''), text: '⏰ ' + faDate(it.due) }) : null,
        checks.length ? h('span', { class: 'tm-badge', text: '☑ ' + faN(checks.filter(x => x.done).length) + '/' + faN(checks.length) }) : null,
        subs.length ? h('span', { class: 'tm-badge', text: '⤷ ' + faN(subs.filter(x => statusOf(x.status).done).length) + '/' + faN(subs.length) }) : null,
        (it.links || []).length ? h('span', { class: 'tm-badge', text: '🔗 ' + faN(it.links.length) }) : null,
        h('span', { class: 'spacer' }),
        ...(it.assignees || []).slice(0, 3).map(u => h('span', { class: 'tm-face', title: nameOf(u), text: initials(u) })),
      ]),
    ]);
    return c;
  }
  // Where a dropped card lands: between the cards either side of the drop.
  function positionAt(list, index) {
    const before = list[index - 1], after = list[index];
    if (!before && !after) return 1024;
    if (!before) return after.position - 1024;
    if (!after) return before.position + 1024;
    return (before.position + after.position) / 2;
  }
  function kanban(v) {
    const top = topItems();
    const board = h('div', { class: 'tm-kanban' });
    for (const st of S.board.statuses) {
      const cards = top.filter(i => i.status === st.key).sort((a, b) => a.position - b.position);
      const full = st.wip > 0 && cards.length >= st.wip;
      const body = h('div', { class: 'tm-col-body' }, cards.map(card));
      const col = h('section', { class: 'tm-col' + (full ? ' full' : ''), 'aria-label': st.name,
        ondragover: e => { if (canEdit()) { e.preventDefault(); col.classList.add('over'); } },
        ondragleave: () => col.classList.remove('over'),
        ondrop: async e => {
          e.preventDefault();
          col.classList.remove('over');
          const id = e.dataTransfer.getData('text/plain');
          const it = S.items.find(x => x.id === id);
          if (!it) return;
          const others = cards.filter(x => x.id !== id);
          const rects = [...body.children].filter(n => n.classList.contains('tm-card'));
          let index = others.length;
          for (let k = 0; k < rects.length; k++) {
            const r = rects[k].getBoundingClientRect();
            if (e.clientY < r.top + r.height / 2) { index = k; break; }
          }
          const next = Object.assign({}, it, { status: st.key, position: positionAt(others, Math.min(index, others.length)) });
          if (await saveItem(next, true)) drawView();
        } }, [
        h('header', { class: 'tm-col-head', style: 'border-top-color:' + (st.color || 'var(--line)') }, [
          h('b', { text: st.name }), h('span', { class: 'hint', text: faN(cards.length) + (st.wip ? ' / ' + faN(st.wip) : '') })]),
        body,
        canEdit() && !S.board.process ? quickAdd(st.key) : null,
      ]);
      board.append(col);
    }
    v.append(board);
  }
  function quickAdd(status) {
    const input = h('input', { class: 'input', placeholder: '+ کار تازه', onkeydown: async e => {
      if (e.key !== 'Enter' || !input.value.trim()) return;
      const col = topItems().filter(i => i.status === status);
      const pos = col.length ? Math.max(...col.map(i => i.position)) + 1024 : 1024;
      if (await saveItem({ board_id: S.board.id, title: input.value, status, position: pos }, true)) drawView();
    } });
    return h('div', { class: 'tm-quick' }, input);
  }

  // --------------------------------------------------------------- table
  function table(v) {
    const fields = S.board.fields || [];
    const cols = [
      ['title', 'کار'], ['status', 'ستون'], ['priority', 'اولویت'], ['assignees', 'مسئول'], ['start', 'شروع'], ['due', 'سررسید'],
      ['labels', 'برچسب'], ...fields.map(f => ['f:' + f.key, f.name])];
    let rows = topItems();
    if (S.sort) {
      const [k, dir] = S.sort;
      const val = i => k.startsWith('f:') ? String((i.fields || {})[k.slice(2)] ?? '') : k === 'assignees' ? (i.assignees || []).join() :
        k === 'labels' ? (i.labels || []).join() : k === 'priority' ? String(Object.keys(PRI).indexOf(i.priority)) : String(i[k] || '');
      rows = rows.slice().sort((a, b) => val(a).localeCompare(val(b), 'fa') * dir);
    }
    const edit = canEdit();
    const t = h('table', { class: 'data tm-table' }, [
      h('thead', {}, h('tr', {}, cols.map(([k, n]) => h('th', {}, h('button', { type: 'button', class: 'sortbtn', text: n + (S.sort && S.sort[0] === k ? (S.sort[1] > 0 ? ' ▲' : ' ▼') : ''),
        onclick: () => { S.sort = S.sort && S.sort[0] === k ? [k, -S.sort[1]] : [k, 1]; drawView(); } }))))),
      h('tbody', {}, rows.map(it => h('tr', {}, cols.map(([k]) => h('td', {}, tableCell(it, k, edit)))))),
    ]);
    v.append(h('div', { class: 'table-scroll' }, t));
    if (!rows.length) v.append(h('p', { class: 'hint', text: 'کاری نیست.' }));
  }
  function tableCell(it, k, edit) {
    const upd = async patch => { if (await saveItem(Object.assign({}, it, patch), true)) drawView(); };
    if (k === 'title') return h('button', { type: 'button', class: 'linkbtn', text: it.title, onclick: () => openItem(it) });
    if (k === 'status') {
      if (!edit) return statusOf(it.status).name;
      const s = h('select', { class: 'input', onchange: () => upd({ status: s.value }) },
        S.board.statuses.map(st => h('option', { value: st.key, text: st.name })));
      s.value = it.status;
      return s;
    }
    if (k === 'priority') {
      if (!edit) return PRI[it.priority];
      const s = h('select', { class: 'input', onchange: () => upd({ priority: s.value }) },
        Object.entries(PRI).map(([p, n]) => h('option', { value: p, text: n })));
      s.value = it.priority;
      return s;
    }
    if (k === 'assignees') return (it.assignees || []).map(nameOf).join('، ');
    if (k === 'labels') return (it.labels || []).join('، ');
    if (k === 'start' || k === 'due') {
      if (!edit) return faDate(it[k]);
      const inp = h('input', { class: 'input', value: toJalali(it[k]), placeholder: '۱۴۰۵/۰۷/۰۸', style: 'width:8.5em', onchange: () => {
        const iso = fromJalali(inp.value);
        if (iso === null) { toast('تاریخ را به شکل ۱۴۰۵/۰۷/۰۸ بنویسید.', true); return; }
        upd({ [k]: iso });
      } });
      if (DP()) DP().attach(inp);
      return inp;
    }
    if (k.startsWith('f:')) {
      const f = (S.board.fields || []).find(x => x.key === k.slice(2));
      const val = (it.fields || {})[f.key];
      if (!edit) return f.kind === 'check' ? (val ? '✓' : '') : f.kind === 'date' ? faDate(val) : String(val ?? '');
      return fieldInput(f, val, nv => upd({ fields: Object.assign({}, it.fields || {}, { [f.key]: nv }) }));
    }
    return '';
  }
  function fieldInput(f, val, set) {
    if (f.kind === 'choice') {
      const s = h('select', { class: 'input', onchange: () => set(s.value) },
        [h('option', { value: '', text: '—' })].concat((f.options || []).map(o => h('option', { value: o, text: o }))));
      s.value = val || '';
      return s;
    }
    if (f.kind === 'check') {
      const c = h('input', { type: 'checkbox', checked: !!val, onchange: () => set(c.checked) });
      return c;
    }
    if (f.kind === 'number') {
      const n = h('input', { class: 'input', type: 'number', value: val ?? '', style: 'width:7em', onchange: () => set(n.value === '' ? null : Number(n.value)) });
      return n;
    }
    if (f.kind === 'date') {
      const d = h('input', { class: 'input', value: toJalali(val), style: 'width:8.5em', onchange: () => { const iso = fromJalali(d.value); if (iso !== null) set(iso); } });
      if (DP()) DP().attach(d);
      return d;
    }
    if (f.kind === 'person') {
      const s = h('select', { class: 'input', onchange: () => set(s.value) },
        [h('option', { value: '', text: '—' })].concat(S.people.map(p => h('option', { value: p.user, text: p.name || p.user }))));
      s.value = val || '';
      return s;
    }
    const t = h('input', { class: 'input', value: val ?? '', onchange: () => set(t.value) });
    return t;
  }

  // ---------------------------------------------------------------- list
  function list(v) {
    for (const st of S.board.statuses) {
      const items = topItems().filter(i => i.status === st.key);
      if (!items.length) continue;
      v.append(h('h3', { class: 'tm-list-head', text: st.name + ' (' + faN(items.length) + ')' }),
        h('ul', { class: 'tm-list' }, items.map(it => h('li', {}, h('button', { type: 'button', class: 'linkbtn', onclick: () => openItem(it) }, [
          h('b', { text: it.title }),
          h('span', { class: 'hint', text: [PRI[it.priority], (it.assignees || []).map(nameOf).join('، '), faDate(it.due)].filter(Boolean).join(' · ') })])))));
    }
    if (!topItems().length) v.append(h('p', { class: 'hint', text: 'کاری نیست.' }));
  }

  // ------------------------------------------------------------ calendar
  function calendar(v) {
    const dp = DP();
    if (!dp) { v.append(h('p', { class: 'hint', text: 'تقویم بار نشد.' })); return; }
    const now = new Date();
    if (!S.calMonth) { const [jy, jm] = dp.gregorianToJalali(now.getFullYear(), now.getMonth() + 1, now.getDate()); S.calMonth = [jy, jm]; }
    const [jy, jm] = S.calMonth;
    const MONTHS = ['فروردین', 'اردیبهشت', 'خرداد', 'تیر', 'مرداد', 'شهریور', 'مهر', 'آبان', 'آذر', 'دی', 'بهمن', 'اسفند'];
    const first = dp.jalaliToGregorian(jy, jm, 1);
    const firstISO = first[0] + '-' + pad(first[1]) + '-' + pad(first[2]);
    const nextM = jm === 12 ? [jy + 1, 1] : [jy, jm + 1];
    const nf = dp.jalaliToGregorian(nextM[0], nextM[1], 1);
    const len = daysBetween(firstISO, nf[0] + '-' + pad(nf[1]) + '-' + pad(nf[2]));
    const lead = (new Date(firstISO + 'T12:00:00').getDay() + 1) % 7; // Saturday first
    const byDay = {};
    for (const it of topItems()) if (it.due) (byDay[it.due] = byDay[it.due] || []).push(it);
    const grid = h('div', { class: 'tm-cal' }, ['ش', 'ی', 'د', 'س', 'چ', 'پ', 'ج'].map(d => h('b', { class: 'tm-cal-dow', text: d })));
    for (let k = 0; k < lead; k++) grid.append(h('div', { class: 'tm-cal-day empty' }));
    for (let d = 1; d <= len; d++) {
      const iso = addDays(firstISO, d - 1);
      const cell = h('div', { class: 'tm-cal-day' + (iso === todayISO() ? ' today' : '') }, [h('span', { class: 'tm-cal-n', text: faN(d) })]);
      for (const it of byDay[iso] || []) cell.append(h('button', { type: 'button', class: 'tm-cal-item', text: it.title,
        style: 'border-inline-start-color:' + (statusOf(it.status).color || 'var(--line)'), onclick: () => openItem(it) }));
      grid.append(cell);
    }
    v.append(h('div', { class: 'tm-cal-bar' }, [
      h('button', { type: 'button', class: 'btn btn-sm', text: 'ماه قبل', onclick: () => { S.calMonth = jm === 1 ? [jy - 1, 12] : [jy, jm - 1]; drawView(); } }),
      h('b', { text: MONTHS[jm - 1] + ' ' + String(jy).replace(/\d/g, c => '۰۱۲۳۴۵۶۷۸۹'[c]) }),
      h('button', { type: 'button', class: 'btn btn-sm', text: 'ماه بعد', onclick: () => { S.calMonth = nextM; drawView(); } }),
    ]), grid);
  }

  // ------------------------------------------------------------ timeline
  // A Gantt of the items with dates: a bar from start to due over the weeks
  // they span, today marked, and each item's dependencies named under it.
  function timeline(v) {
    const dated = topItems().filter(i => i.start || i.due);
    if (!dated.length) { v.append(h('p', { class: 'hint', text: 'برای زمان‌بندی، به کارها تاریخ شروع یا سررسید بدهید.' })); return; }
    let from = dated.map(i => i.start || i.due).sort()[0], to = dated.map(i => i.due || i.start).sort().at(-1);
    from = addDays(from, -3); to = addDays(to, 3);
    const span = Math.max(daysBetween(from, to), 7);
    const pct = iso => (daysBetween(from, iso) / span) * 100;
    const rows = h('div', { class: 'tm-gantt' });
    const t = todayISO();
    for (const it of dated.sort((a, b) => (a.start || a.due).localeCompare(b.start || b.due))) {
      const s = it.start || it.due, e = it.due || it.start;
      const deps = (it.deps || []).map(d => { const o = S.items.find(x => x.id === d.id); return o ? DEP_KINDS[d.kind] + ' «' + o.title + '»' : ''; }).filter(Boolean);
      rows.append(h('div', { class: 'tm-gantt-row' }, [
        h('button', { type: 'button', class: 'linkbtn tm-gantt-name', text: it.title, onclick: () => openItem(it) }),
        h('div', { class: 'tm-gantt-track' }, [
          t >= from && t <= to ? h('span', { class: 'tm-gantt-today', style: 'inset-inline-start:' + pct(t) + '%' }) : null,
          h('span', { class: 'tm-gantt-bar' + (statusOf(it.status).done ? ' done' : ''), title: faDate(s) + ' تا ' + faDate(e),
            style: 'inset-inline-start:' + pct(s) + '%;width:' + Math.max(pct(addDays(e, 1)) - pct(s), 1.2) + '%;background:' + (statusOf(it.status).color || 'var(--accent)') }),
        ]),
        deps.length ? h('span', { class: 'hint tm-gantt-deps', text: deps.join('؛ ') }) : null,
      ]));
    }
    v.append(h('p', { class: 'hint', text: 'از ' + faDate(from) + ' تا ' + faDate(to) }), rows);
  }

  // ------------------------------------------------------------ workload
  // Who carries what: open items, overdue ones and estimated hours per person,
  // against a capacity of forty hours; past it a person is flagged.
  function workload(v) {
    const by = {};
    for (const it of S.items) {
      if (statusOf(it.status).done) continue;
      for (const u of (it.assignees || []).length ? it.assignees : ['']) {
        const w = by[u] = by[u] || { open: 0, late: 0, min: 0 };
        w.open++;
        if (it.due && it.due < todayISO()) w.late++;
        w.min += it.estimate_min || 0;
      }
    }
    const rows = Object.entries(by).sort((a, b) => b[1].min - a[1].min || b[1].open - a[1].open);
    if (!rows.length) { v.append(h('p', { class: 'hint', text: 'کار بازی نیست.' })); return; }
    v.append(h('div', { class: 'tm-load' }, rows.map(([u, w]) => {
      const load = Math.round((w.min / 60 / 40) * 100);
      return h('div', { class: 'tm-load-row' + (load > 100 ? ' over' : '') }, [
        h('b', { text: u ? nameOf(u) : 'بی‌مسئول' }),
        h('div', { class: 'tm-load-track' }, h('span', { style: 'width:' + Math.min(load, 100) + '%' })),
        h('span', { class: 'hint', text: faN(w.open) + ' کار باز · ' + faN(w.late) + ' دیرکرد · ' + faN(Math.round(w.min / 60)) + ' ساعت (' + faN(load) + '٪)' }),
      ]);
    })));
  }

  // ------------------------------------------------------------ activity
  async function activity(v) {
    v.append(h('p', { class: 'hint', text: 'در حال خواندن…' }));
    try {
      const d = await api('/api/tasks/boards/' + encodeURIComponent(S.board.id) + '/activity');
      v.textContent = '';
      v.append(logList(d.activity || []));
    } catch (e) { v.textContent = ''; v.append(h('div', { class: 'err', text: e.message })); }
  }
  function logList(rows) {
    if (!rows.length) return h('p', { class: 'hint', text: 'هنوز چیزی ثبت نشده است.' });
    return h('ul', { class: 'tm-log' }, rows.map(a => h('li', {}, [
      h('span', { class: 'hint', text: new Date(a.at).toLocaleString('fa-IR', { dateStyle: 'short', timeStyle: 'short' }) }),
      h('b', { text: nameOf(a.actor) }), h('span', { text: a.detail })])));
  }

  // ------------------------------------------------------------- my work
  async function openSpecial(kind, push) {
    S.current = kind; S.board = null;
    writeURL(push);
    drawSide(); drawHead();
    const v = $('tmView');
    v.textContent = '';
    v.append(h('p', { class: 'hint', text: 'در حال خواندن…' }));
    const path = { my: '/api/tasks/my', flow: '/api/tasks/flow', dash: '/api/tasks/dashboard', approvals: '/api/tasks/approvals', forms: '/api/tasks/forms',
      time: '/api/tasks/timesheet?from=' + addDays(todayISO(), -30) }[kind];
    let d;
    try { d = await api(path); } catch (e) { v.textContent = ''; v.append(h('div', { class: 'err', text: e.message })); return; }
    if (S.current !== kind) return;
    v.textContent = '';
    ({ my: drawMy, flow: window.TaskFlow.draw, dash: drawDash, approvals: drawApprovals, forms: drawForms, time: drawTime }[kind])(v, d);
  }
  const openMy = push => openSpecial('my', push);
  function drawMy(v, d) {
    const t = todayISO(), week = addDays(t, 7);
    const groups = [['دیرکرد', i => i.due && i.due < t], ['امروز', i => i.due === t], ['این هفته', i => i.due > t && i.due <= week],
      ['بعدتر', i => i.due > week], ['بی‌سررسید', i => !i.due]];
    const items = d.items || [];
    if (!items.length) v.append(h('p', { class: 'hint', text: 'کار بازی به شما سپرده نشده است.' }));
    for (const [name, test] of groups) {
      const g = items.filter(test);
      if (!g.length) continue;
      v.append(h('h3', { class: 'tm-list-head', text: name + ' (' + faN(g.length) + ')' }),
        h('ul', { class: 'tm-list' }, g.map(it => {
          const b = (d.boards || {})[it.board_id] || {};
          const st = (b.statuses || []).find(s => s.key === it.status) || {};
          return h('li', {}, h('a', { href: '/tasks?item=' + encodeURIComponent(it.id) }, [
            h('b', { text: it.title }),
            h('span', { class: 'hint', text: [b.name, st.name, PRI[it.priority], faDate(it.due)].filter(Boolean).join(' · ') })]));
        })));
    }
  }

  // The figures across every board this person sees.
  function drawDash(v, d) {
    const pct = x => faN(Math.round((x || 0) * 100)) + '٪';
    const tile = (n, label, cls) => h('div', { class: 'tm-kpi' + (cls ? ' ' + cls : '') }, [h('b', { text: n }), h('span', { text: label })]);
    v.append(h('div', { class: 'tm-kpis' }, [
      tile(faN(d.open), 'کار باز'), tile(faN(d.overdue), 'دیرکرد', d.overdue ? 'bad' : ''), tile(faN(d.due_week), 'سررسید تا یک هفته'),
      tile(faN(d.done_week), 'انجام‌شده در هفتهٔ گذشته', 'ok'), tile(pct(d.rate), 'نسبت انجام‌شده'),
      tile(d.avg_days ? faN(Math.round(d.avg_days * 10) / 10) + ' روز' : '-', 'میانگین زمان انجام'),
      tile(faN(d.approvals), 'منتظر تأیید شما', d.approvals ? 'warn' : '')]));
    const table2 = (title, rows, cols) => rows.length ? h('section', { class: 'card' }, [h('h3', { class: 'card-head', text: title }),
      h('div', { class: 'table-scroll' }, h('table', { class: 'data' }, [
        h('thead', {}, h('tr', {}, cols.map(c => h('th', { text: c[0] })))),
        h('tbody', {}, rows.map(r => h('tr', {}, cols.map(c => h('td', { text: c[1](r) })))))]))]) : null;
    v.append(h('div', { class: 'tm-dash-cols' }, [
      table2('به تفکیک بورد', d.by_board || [], [['بورد', r => r.name], ['باز', r => faN(r.open)], ['دیرکرد', r => faN(r.overdue)], ['انجام‌شده', r => faN(r.done)]]),
      table2('به تفکیک فرد', d.by_person || [], [['فرد', r => r.name ? nameOf(r.name) : 'بی‌مسئول'], ['باز', r => faN(r.open)], ['دیرکرد', r => faN(r.overdue)],
        ['برآورد (ساعت)', r => faN(Math.round(r.hours || 0))]]),
      table2('به تفکیک ستون', d.by_status || [], [['ستون', r => r.name], ['باز', r => faN(r.open)], ['انجام‌شده', r => faN(r.done)]]),
    ]));
    if ((d.recent || []).length) v.append(h('section', { class: 'card' }, [h('h3', { class: 'card-head', text: 'آخرین کارها' }),
      h('div', { class: 'card-body' }, logList(d.recent))]));
  }

  // What waits on my decision.
  function drawApprovals(v, d) {
    const items = d.items || [];
    if (!items.length) { v.append(h('p', { class: 'hint', text: 'چیزی منتظر تأیید شما نیست.' })); return; }
    v.append(h('ul', { class: 'tm-list' }, items.map(it => {
      const p = it.approval || {};
      const step = (p.steps || [])[p.at] || {};
      return h('li', {}, h('a', { href: '/tasks?item=' + encodeURIComponent(it.id) }, [h('b', { text: it.title }),
        h('span', { class: 'hint', text: [(d.boards || {})[it.board_id], step.name, p.by ? 'درخواست ' + nameOf(p.by) : '', p.due ? 'تا ' + faDate(p.due) : '']
          .filter(Boolean).join(' · ') })]));
    })));
  }

  // The intake forms this person may send.
  function drawForms(v, d) {
    const forms = d.forms || [];
    if (!forms.length) { v.append(h('p', { class: 'hint', text: 'فرمی نیست. در «تنظیم بورد» فرم بسازید.' })); return; }
    v.append(h('ul', { class: 'tm-list' }, forms.map(f => h('li', { class: 'tm-row' }, [
      h('button', { type: 'button', class: 'linkbtn', style: 'flex:1 1 auto', onclick: () => fillForm(f) }, [
        h('b', { text: f.form.name }), h('span', { class: 'hint', text: f.board_name + (f.form.open ? ' · برای همه' : '') })]),
      f.sees_board ? h('button', { type: 'button', class: 'btn btn-sm', text: 'پاسخ‌ها', onclick: () => formAnswers(f) }) : null]))));
  }
  // A form's answers as a table (formanswers.go), each row its request, and
  // the same as a file for Excel.
  async function formAnswers(ref) {
    const path = '/api/tasks/boards/' + encodeURIComponent(ref.board_id) + '/forms/' + encodeURIComponent(ref.form.id) + '/answers';
    let d;
    try { d = await api(path); } catch (e) { toast(e.message, true); return; }
    const dlg = $('tmForm'), body = $('tmFormBody'), foot = $('tmFormFoot');
    body.textContent = ''; foot.textContent = '';
    body.append(h('h2', { text: 'پاسخ‌های «' + d.form.name + '»' }), h('p', { class: 'hint', text: faN(d.rows.length) + ' درخواست، تازه‌ترین بالا' }));
    if (d.rows.length) {
      body.append(h('div', { class: 'table-scroll' }, h('table', { class: 'data' }, [
        h('thead', {}, h('tr', {}, ['تاریخ', 'فرستنده', 'وضعیت'].concat(d.columns.map(c => c.label)).map(t => h('th', { text: t })))),
        h('tbody', {}, d.rows.map(r => h('tr', { style: 'cursor:pointer', title: r.title, onclick: () => { dlg.close(); openItem({ id: r.item_id }); } },
          [faDate(r.created.slice(0, 10)), nameOf(r.by), r.status].concat(r.cells).map(t => h('td', { text: t })))))])));
    }
    foot.append(h('a', { class: 'btn btn-primary', href: path + '?format=csv', download: '', text: 'دریافت برای Excel' }),
      h('span', { class: 'spacer' }), h('button', { type: 'button', class: 'btn btn-ghost', text: 'بستن', onclick: () => dlg.close() }));
    dlg.showModal();
  }
  function fillForm(ref) {
    const f = ref.form;
    const dlg = $('tmForm'), body = $('tmFormBody'), foot = $('tmFormFoot');
    body.textContent = ''; foot.textContent = '';
    put(body, h('h2', { text: f.name }), f.intro ? h('p', { class: 'hint', text: f.intro }) : null);
    // Every kind of question as the processes draw it (taskflow.js), and the
    // answers typed: a date as a date, an amount as a number.
    const fm = window.TaskFlow.form(f.fields, {}, S.people, { board: ref.board_id, form: f.id });
    body.append(fm.el);
    foot.append(h('button', { type: 'button', class: 'btn btn-primary', text: 'فرستادن', onclick: async () => {
      try {
        const r = await send('POST', '/api/tasks/forms/' + encodeURIComponent(ref.board_id) + '/' + encodeURIComponent(f.id), { answers: fm.read() });
        dlg.close(); toast('فرستاده شد: ' + r.title);
      } catch (e) { toast(e.message, true); }
    } }), h('span', { class: 'spacer' }), h('button', { type: 'button', class: 'btn btn-ghost', text: 'بستن', onclick: () => dlg.close() }));
    dlg.showModal();
  }

  // The timesheet of the last thirty days: per person and day, then each entry.
  function drawTime(v, d) {
    const rows = d.rows || [];
    if (!rows.length) { v.append(h('p', { class: 'hint', text: 'در سی روز گذشته زمانی ثبت نشده است.' })); return; }
    const by = {};
    for (const r of rows) { const k = r.user + '|' + r.day; by[k] = (by[k] || 0) + r.minutes; }
    const hours = m => faN(Math.round(m / 6) / 10);
    v.append(h('section', { class: 'card' }, [h('h3', { class: 'card-head', text: 'به تفکیک فرد و روز (ساعت)' }),
      h('div', { class: 'table-scroll' }, h('table', { class: 'data' }, [
        h('thead', {}, h('tr', {}, ['فرد', 'روز', 'ساعت'].map(t => h('th', { text: t })))),
        h('tbody', {}, Object.entries(by).map(([k, m]) => { const [u, day] = k.split('|'); return h('tr', {}, [h('td', { text: nameOf(u) }), h('td', { text: faDate(day) }), h('td', { text: hours(m) })]); }))]))]),
      h('section', { class: 'card' }, [h('h3', { class: 'card-head', text: 'ریز کارها' }),
        h('ul', { class: 'tm-list' }, rows.map(r => h('li', {}, h('a', { href: '/tasks?item=' + encodeURIComponent(r.item_id) }, [
          h('b', { text: r.title }), h('span', { class: 'hint', text: [nameOf(r.user), faDate(r.day), r.board, hours(r.minutes) + ' ساعت', r.running ? 'در جریان' : ''].filter(Boolean).join(' · ') })]))))]));
  }

  // ------------------------------------------------------ one item, open
  async function openItem(seed) {
    // A case on a process board has steps, not a free edit.
    if (seed.flow) return window.TaskFlow.openCase(seed.id);
    const dlg = $('tmItem');
    let it = seed.id ? seed : Object.assign({ title: '', status: '', priority: 'normal', assignees: [], watchers: [], labels: [],
      checklist: [], links: [], deps: [], fields: {}, version: 0, position: Date.now() / 1000 }, seed);
    let comments = [], log = [];
    if (it.id) {
      try {
        const d = await api('/api/tasks/items/' + encodeURIComponent(it.id));
        it = d.item; comments = d.comments || []; log = d.activity || [];
        if (it.flow) return window.TaskFlow.openCase(it.id);
        S.buttons = d.buttons || S.buttons;
        if (!S.board || S.board.id !== d.board.id) { S.board = d.board; S.workspace = d.workspace; S.role = d.role; S.items = d.items || [it]; }
      } catch (e) { toast(e.message, true); return; }
    }
    const edit = canEdit();
    const draft = JSON.parse(JSON.stringify(it));
    const body = $('tmItemBody');
    body.textContent = '';
    const field = (label, control) => h('label', { class: 'tm-field' }, [h('span', { class: 'label', text: label }), control]);
    const title = h('input', { class: 'input tm-item-title', value: draft.title, placeholder: 'عنوان کار', disabled: !edit, oninput: () => { draft.title = title.value; } });
    const desc = h('textarea', { class: 'input', rows: 4, value: draft.description || '', placeholder: 'شرح', disabled: !edit, oninput: () => { draft.description = desc.value; } });
    const status = h('select', { class: 'input', disabled: !edit, onchange: () => { draft.status = status.value; } },
      S.board.statuses.map(s => h('option', { value: s.key, text: s.name })));
    status.value = draft.status || S.board.statuses[0].key;
    draft.status = status.value;
    const pri = h('select', { class: 'input', disabled: !edit, onchange: () => { draft.priority = pri.value; } },
      Object.entries(PRI).map(([k, n]) => h('option', { value: k, text: n })));
    pri.value = draft.priority;
    const people = (key, label) => {
      const box = h('div', { class: 'tm-people' });
      const paint = () => {
        box.textContent = '';
        for (const u of draft[key]) box.append(h('span', { class: 'tm-chip' }, [nameOf(u),
          edit ? h('button', { type: 'button', class: 'tm-chip-x', text: '×', 'aria-label': 'برداشتن', onclick: () => { draft[key] = draft[key].filter(x => x !== u); paint(); } }) : null]));
        if (edit) {
          const s = h('select', { class: 'input', 'aria-label': label, onchange: () => { if (s.value && !draft[key].includes(s.value)) draft[key].push(s.value); paint(); } },
            [h('option', { value: '', text: '+ افزودن' })].concat(S.people.filter(p => !draft[key].includes(p.user)).map(p => h('option', { value: p.user, text: p.name || p.user }))));
          box.append(s);
        }
      };
      paint();
      return box;
    };
    const dateIn = key => {
      const d = h('input', { class: 'input', value: toJalali(draft[key]), placeholder: '۱۴۰۵/۰۷/۰۸', disabled: !edit, onchange: () => {
        const iso = fromJalali(d.value);
        if (iso === null) { toast('تاریخ را به شکل ۱۴۰۵/۰۷/۰۸ بنویسید.', true); return; }
        draft[key] = iso;
      } });
      if (edit && DP()) DP().attach(d);
      return d;
    };
    const num = (key, label) => { const n = h('input', { class: 'input', type: 'number', min: 0, value: draft[key] ? Math.round(draft[key] / 60 * 10) / 10 : '', disabled: !edit,
      oninput: () => { draft[key] = Math.round(Number(n.value || 0) * 60); } }); return field(label, n); };
    const labels = h('div', { class: 'tm-people' });
    const paintLabels = () => {
      labels.textContent = '';
      for (const l of S.board.labels || []) {
        const on = draft.labels.includes(l.name);
        labels.append(h('button', { type: 'button', class: 'tm-label' + (on ? '' : ' off'), text: l.name, disabled: !edit,
          style: l.color ? 'background:' + l.color : '', onclick: () => { draft.labels = on ? draft.labels.filter(x => x !== l.name) : draft.labels.concat(l.name); paintLabels(); } }));
      }
      if (!(S.board.labels || []).length) labels.append(h('span', { class: 'hint', text: 'این بورد برچسبی ندارد (تنظیم بورد).' }));
    };
    paintLabels();

    // The checklist.
    const checks = h('div', { class: 'tm-checks' });
    const paintChecks = () => {
      checks.textContent = '';
      draft.checklist.forEach((c, i) => checks.append(h('div', { class: 'tm-check' }, [
        h('input', { type: 'checkbox', checked: c.done, disabled: !edit, 'aria-label': c.text, onchange: e => { c.done = e.target.checked; } }),
        h('span', { text: c.text, class: c.done ? 'done' : '' }),
        edit ? h('button', { type: 'button', class: 'tm-chip-x', text: '×', 'aria-label': 'حذف', onclick: () => { draft.checklist.splice(i, 1); paintChecks(); } }) : null])));
      if (edit) {
        const add = h('input', { class: 'input', placeholder: '+ مورد تازه، Enter', onkeydown: e => {
          if (e.key === 'Enter' && add.value.trim()) { e.preventDefault(); draft.checklist.push({ text: add.value.trim(), done: false }); paintChecks(); checks.querySelector('input:last-of-type')?.focus(); }
        } });
        checks.append(add);
      }
    };
    paintChecks();

    // Links: a customer, an order, an email, a document, a page...
    const links = h('div', { class: 'tm-links' });
    const paintLinks = () => {
      links.textContent = '';
      for (const [i, l] of draft.links.entries()) links.append(h('div', { class: 'tm-link' }, [
        h('span', { class: 'hint', text: LINK_KINDS[l.kind] || l.kind }), linkTo(l),
        edit ? h('button', { type: 'button', class: 'tm-chip-x', text: '×', 'aria-label': 'حذف', onclick: () => { draft.links.splice(i, 1); paintLinks(); } }) : null]));
      if (edit) {
        const kind = h('select', { class: 'input', style: 'width:auto' }, Object.entries(LINK_KINDS).map(([k, n]) => h('option', { value: k, text: n })));
        const ref = h('input', { class: 'input', placeholder: 'شماره سفارش، نام مشتری، نشانی صفحه…' });
        links.append(h('div', { class: 'tm-link-add' }, [kind, ref, h('button', { type: 'button', class: 'btn btn-sm', text: 'افزودن', onclick: async () => {
          const typed = ref.value.trim();
          if (!typed) return;
          draft.links.push({ kind: kind.value, ref: typed, title: typed }); ref.value = ''; paintLinks();
        } })]));
      }
    };
    paintLinks();

    // Dependencies on other items of this board.
    const deps = h('div', { class: 'tm-links' });
    const paintDeps = () => {
      deps.textContent = '';
      for (const [i, d] of draft.deps.entries()) {
        const o = S.items.find(x => x.id === d.id);
        deps.append(h('div', { class: 'tm-link' }, [h('span', { class: 'hint', text: DEP_KINDS[d.kind] }),
          h('span', { text: o ? o.title : '(کار حذف‌شده)' }),
          edit ? h('button', { type: 'button', class: 'tm-chip-x', text: '×', 'aria-label': 'حذف', onclick: () => { draft.deps.splice(i, 1); paintDeps(); } }) : null]));
      }
      if (edit) {
        const kind = h('select', { class: 'input', style: 'width:auto' }, Object.entries(DEP_KINDS).map(([k, n]) => h('option', { value: k, text: n })));
        const other = h('select', { class: 'input' }, S.items.filter(x => x.id !== draft.id && !draft.deps.some(d => d.id === x.id))
          .map(x => h('option', { value: x.id, text: x.title })));
        if (other.options.length) deps.append(h('div', { class: 'tm-link-add' }, [kind, other,
          h('button', { type: 'button', class: 'btn btn-sm', text: 'افزودن', onclick: () => { draft.deps.push({ id: other.value, kind: kind.value }); paintDeps(); } })]));
      }
    };
    paintDeps();

    const custom = (S.board.fields || []).map(f => field(f.name, edit ? fieldInput(f, (draft.fields || {})[f.key], v => { draft.fields = Object.assign({}, draft.fields, { [f.key]: v }); })
      : h('span', { text: String((draft.fields || {})[f.key] ?? '') })));

    // Subtasks: their own items, under this one.
    const subs = h('ul', { class: 'tm-list' });
    const paintSubs = () => {
      subs.textContent = '';
      for (const s of subsOf(draft.id || '-')) subs.append(h('li', {}, h('button', { type: 'button', class: 'linkbtn', onclick: () => openItem(s) }, [
        h('b', { text: (statusOf(s.status).done ? '✓ ' : '') + s.title }),
        h('span', { class: 'hint', text: [statusOf(s.status).name, (s.assignees || []).map(nameOf).join('، '), faDate(s.due)].filter(Boolean).join(' · ') })])));
    };
    paintSubs();
    const subAdd = edit && draft.id && !draft.parent_id ? h('input', { class: 'input', placeholder: '+ زیرکار تازه، Enter', onkeydown: async e => {
      if (e.key !== 'Enter' || !subAdd.value.trim()) return;
      e.preventDefault();
      if (await saveItem({ board_id: S.board.id, parent_id: draft.id, title: subAdd.value, position: Date.now() / 1000 }, true)) { subAdd.value = ''; paintSubs(); }
    } }) : null;

    // Updates: comments, notes, decisions; and the item's history.
    const updates = h('div', { class: 'tm-updates' });
    const paintUpdates = () => {
      updates.textContent = '';
      for (const c of comments) updates.append(h('div', { class: 'tm-update ' + c.kind }, [
        h('div', { class: 'hint' }, [h('b', { text: nameOf(c.author) }), ' · ' + new Date(c.created).toLocaleString('fa-IR', { dateStyle: 'short', timeStyle: 'short' }) +
          (c.kind !== 'comment' ? ' · ' + ({ note: 'یادداشت داخلی', decision: 'تصمیم', approval: 'تأیید' }[c.kind] || '') : '')]),
        h('p', { text: c.body })]));
      if (!comments.length) updates.append(h('p', { class: 'hint', text: 'هنوز چیزی نوشته نشده است. با @نام‌کاربری کسی را خبر کنید.' }));
    };
    paintUpdates();
    const say = h('textarea', { class: 'input', rows: 2, placeholder: 'به‌روزرسانی، @نام‌کاربری برای خبر کردن', disabled: !edit || !draft.id });
    const sayKind = h('select', { class: 'input', style: 'width:auto', disabled: !edit || !draft.id },
      [['comment', 'نظر'], ['note', 'یادداشت داخلی'], ['decision', 'تصمیم']].map(([k, n]) => h('option', { value: k, text: n })));
    const sayBtn = h('button', { type: 'button', class: 'btn', text: 'فرستادن', disabled: !edit || !draft.id, onclick: async () => {
      if (!say.value.trim()) return;
      try {
        const c = await send('POST', '/api/tasks/items/' + encodeURIComponent(draft.id) + '/comments', { body: say.value, kind: sayKind.value });
        comments.push(c); say.value = ''; paintUpdates();
      } catch (e) { toast(e.message, true); }
    } });

    // Refresh the open item from what an action returned.
    const took = saved => {
      const i = S.items.findIndex(x => x.id === saved.id);
      if (i >= 0) S.items[i] = saved; else S.items.push(saved);
      openItem(saved);
      drawView();
    };

    // Time: my timer on this item, a stretch entered by hand, and the log.
    const timeBox = h('div', { class: 'tm-links' });
    if (draft.id) {
      const running = (draft.time || []).some(e => e.user === S.me && !e.end);
      const mins = h('input', { class: 'input', type: 'number', min: 1, placeholder: 'دقیقه', style: 'width:6em' });
      const note = h('input', { class: 'input', placeholder: 'شرح (اختیاری)' });
      const timer = async (action, extra) => {
        try { took(await send('POST', '/api/tasks/items/' + encodeURIComponent(draft.id) + '/timer', Object.assign({ action }, extra || {}))); }
        catch (e) { toast(e.message, true); }
      };
      if (edit) timeBox.append(h('div', { class: 'tm-link-add' }, [
        h('button', { type: 'button', class: 'btn btn-sm' + (running ? ' btn-primary' : ''), text: running ? '■ توقف زمان‌سنج' : '▶ شروع زمان‌سنج',
          onclick: () => timer(running ? 'stop' : 'start') }),
        mins, note, h('button', { type: 'button', class: 'btn btn-sm', text: 'ثبت دستی', onclick: () => timer('add', { minutes: Number(mins.value || 0), note: note.value }) })]));
      for (const e of (draft.time || []).slice(-10).reverse()) {
        timeBox.append(h('div', { class: 'tm-link' }, [h('span', { text: nameOf(e.user) }), h('span', { class: 'hint', text: faDate(e.day) }),
          h('span', { text: e.end ? faN(e.minutes) + ' دقیقه' : 'در جریان' }), e.note ? h('span', { class: 'hint', text: e.note }) : null]));
      }
      if (draft.actual_min) timeBox.append(h('p', { class: 'hint', text: 'جمع: ' + faN(Math.round(draft.actual_min / 6) / 10) + ' ساعت' }));
    }

    // The sign-off: its chain and what was decided; my decision when it waits
    // on me; or, with none running, asking for one.
    const approvalBox = h('div', { class: 'tm-links' });
    if (draft.id) {
      const p = draft.approval;
      if (p) {
        const words = { pending: 'در انتظار', approved: 'تأیید شد', rejected: 'رد شد', changes: 'نیاز به اصلاح' };
        approvalBox.append(h('p', { class: 'hint', text: 'وضعیت: ' + (words[p.state] || p.state) + (p.due ? ' · تا ' + faDate(p.due) : '') + ' · درخواست ' + nameOf(p.by) }));
        (p.steps || []).forEach((st, i) => approvalBox.append(h('div', { class: 'tm-link' }, [
          h('b', { text: (i < p.at || p.state === 'approved' ? '✓ ' : i === p.at && p.state === 'pending' ? '… ' : '') + st.name }),
          h('span', { class: 'hint', text: st.approvers.map(nameOf).join('، ') + (st.mode === 'all' ? ' (همه)' : ' (یکی کافی)') })])));
        for (const dcs of p.decisions || []) approvalBox.append(h('div', { class: 'tm-link hint' }, [nameOf(dcs.user) + ': ' +
          ({ approve: 'تأیید', reject: 'رد', changes: 'اصلاح', delegate: 'سپرد به ' + nameOf(dcs.to || '') }[dcs.decision] || dcs.decision) + (dcs.note ? ' «' + dcs.note + '»' : '')]));
        const mine = p.state === 'pending' && ((p.steps[p.at] || {}).approvers || []).includes(S.me) &&
          !(p.decisions || []).some(x => x.step === p.at && x.user === S.me && x.decision === 'approve');
        if (mine) {
          const why = h('input', { class: 'input', placeholder: 'توضیح (اختیاری)' });
          const to = h('select', { class: 'input', style: 'width:auto' }, [h('option', { value: '', text: 'سپردن به…' })]
            .concat(S.people.filter(x => x.user !== S.me).map(x => h('option', { value: x.user, text: x.name || x.user }))));
          const decide = async (decision) => {
            if (decision === 'delegate' && !to.value) { toast('به چه کسی سپرده شود؟', true); return; }
            try { took(await send('POST', '/api/tasks/items/' + encodeURIComponent(draft.id) + '/decide', { decision, note: why.value, to: to.value })); }
            catch (e) { toast(e.message, true); }
          };
          approvalBox.append(why, h('div', { class: 'tm-row' }, [
            h('button', { type: 'button', class: 'btn btn-primary', text: 'تأیید', onclick: () => decide('approve') }),
            h('button', { type: 'button', class: 'btn', text: 'نیاز به اصلاح', onclick: () => decide('changes') }),
            h('button', { type: 'button', class: 'btn', text: 'رد', onclick: () => decide('reject') }), to,
            h('button', { type: 'button', class: 'btn btn-ghost', text: 'سپردن', onclick: () => decide('delegate') })]));
        }
      }
      if (edit && (!p || p.state !== 'pending')) {
        const steps = [{ name: '', approvers: [], mode: 'any' }];
        const box = h('div', { class: 'tm-links' });
        const paintSteps = () => {
          box.textContent = '';
          steps.forEach((st, i) => {
            const who = h('select', { class: 'input', 'aria-label': 'تأییدکننده', onchange: () => { if (who.value && !st.approvers.includes(who.value)) st.approvers.push(who.value); paintSteps(); } },
              [h('option', { value: '', text: '+ تأییدکننده' })].concat(S.people.map(x => h('option', { value: x.user, text: x.name || x.user }))));
            const mode = h('select', { class: 'input', style: 'width:auto', onchange: () => { st.mode = mode.value; } },
              [['any', 'یکی کافی'], ['all', 'همه']].map(([k, n]) => h('option', { value: k, text: n })));
            mode.value = st.mode;
            box.append(h('div', { class: 'tm-link' }, [h('input', { class: 'input', placeholder: 'مرحلهٔ ' + faN(i + 1), value: st.name, style: 'width:10em',
              oninput: e => { st.name = e.target.value; } }), h('span', { text: st.approvers.map(nameOf).join('، ') || '-' }), who, mode]));
          });
          box.append(h('div', { class: 'tm-row' }, [
            h('button', { type: 'button', class: 'btn btn-sm', text: '+ مرحله', onclick: () => { steps.push({ name: '', approvers: [], mode: 'any' }); paintSteps(); } }),
            h('button', { type: 'button', class: 'btn btn-sm btn-primary', text: 'درخواست تأیید', onclick: async () => {
              try { took(await send('POST', '/api/tasks/items/' + encodeURIComponent(draft.id) + '/approval', { steps: steps.filter(s => s.approvers.length) })); }
              catch (e) { toast(e.message, true); }
            } })]));
        };
        paintSteps();
        approvalBox.append(h('details', {}, [h('summary', { text: p ? 'درخواست تأیید دوباره' : 'درخواست تأیید' }), box]));
      }
    }


    put(body,
      title,
      h('div', { class: 'tm-grid' }, [field('ستون', status), field('اولویت', pri), field('شروع', dateIn('start')), field('سررسید', dateIn('due')),
        num('estimate_min', 'برآورد (ساعت)'), num('actual_min', 'صرف‌شده (ساعت)')]),
      field('مسئول', people('assignees', 'مسئول')),
      field('دنبال‌کنندگان', people('watchers', 'دنبال‌کننده')),
      field('برچسب‌ها', labels),
      custom.length ? h('div', { class: 'tm-grid' }, custom) : null,
      field('شرح', desc),
      h('h3', { text: 'فهرست بررسی' }), checks,
      draft.id && !draft.parent_id ? h('h3', { text: 'زیرکارها' }) : null, draft.id && !draft.parent_id ? subs : null, subAdd,
      h('h3', { text: 'پیوند به مشتری، سفارش، ایمیل و سند' }), links,
      h('h3', { text: 'وابستگی‌ها' }), deps,
      draft.id ? h('h3', { text: 'زمان' }) : null, draft.id ? timeBox : null,
      draft.id ? h('h3', { text: 'تأیید' }) : null, draft.id ? approvalBox : null,
      h('h3', { text: 'به‌روزرسانی‌ها' }), updates,
      edit && draft.id ? h('div', { class: 'tm-say' }, [say, h('div', { class: 'tm-row' }, [sayKind, sayBtn])]) : null,
      log.length ? h('details', {}, [h('summary', { text: 'تاریخچه (' + faN(log.length) + ')' }), logList(log)]) : null,
    );
    const foot = $('tmItemFoot');
    foot.textContent = '';
    put(foot,
      edit ? h('button', { type: 'button', class: 'btn btn-primary', text: 'ذخیره', onclick: async () => {
        const saved = await saveItem(draft);
        if (!saved) return;
        dlg.close(); drawView();
      } }) : null,
      // The board's quick actions: Start, Complete, Escalate, or its own.
      ...(edit && draft.id ? (S.buttons || []).map(bt => h('button', { type: 'button', class: 'btn', text: bt.name,
        style: bt.color ? 'border-color:' + bt.color : '', onclick: async () => {
          try { took(await send('POST', '/api/tasks/items/' + encodeURIComponent(draft.id) + '/press', { button: bt.id })); toast('«' + bt.name + '» انجام شد.'); }
          catch (e) { toast(e.message, true); }
        } })) : []),
      h('span', { class: 'spacer' }),
      edit && draft.id ? h('button', { type: 'button', class: 'btn btn-ghost', text: 'حذف', onclick: async () => {
        if (!foot.dataset.sure) { foot.dataset.sure = '1'; toast('برای حذف دوباره «حذف» را بزنید.'); return; }
        try {
          await send('DELETE', '/api/tasks/items/' + encodeURIComponent(draft.id));
          S.items = S.items.filter(x => x.id !== draft.id && x.parent_id !== draft.id);
          dlg.close(); drawView(); toast('حذف شد.');
        } catch (e) { toast(e.message, true); }
      } }) : null,
      h('button', { type: 'button', class: 'btn btn-ghost', text: 'بستن', onclick: () => dlg.close() }),
    );
    delete foot.dataset.sure;
    if (!dlg.open) dlg.showModal();
    if (!draft.id) title.focus();
  }
  // A link to a page of this site opens it; the others are references, shown
  // as they were written.
  function linkTo(l) {
    const href = l.kind === 'page' && l.ref.startsWith('/') && !l.ref.startsWith('//') ? l.ref : '';
    return href ? h('a', { href, text: l.title || l.ref }) : h('span', { text: l.title || l.ref });
  }

  // ------------------------------------------------- workspace and board
  function editWorkspace(w) {
    const dlg = $('tmForm');
    const body = $('tmFormBody');
    body.textContent = '';
    const draft = JSON.parse(JSON.stringify(w));
    draft.members = draft.members || [];
    const name = h('input', { class: 'input', value: draft.name || '', placeholder: 'نام فضای کار' });
    const dept = h('input', { class: 'input', value: draft.department || '', placeholder: 'واحد، مثلاً فروش' });
    const team = h('input', { class: 'input', value: draft.team || '', placeholder: 'تیم' });
    const open = h('input', { type: 'checkbox', checked: !!draft.open });
    const members = h('div', { class: 'tm-links' });
    const paint = () => {
      members.textContent = '';
      draft.members.forEach((m, i) => {
        const role = h('select', { class: 'input', style: 'width:auto', onchange: () => { m.role = role.value; } },
          [['owner', 'مدیر'], ['member', 'عضو'], ['viewer', 'فقط خواندن']].map(([k, n]) => h('option', { value: k, text: n })));
        role.value = m.role || 'member';
        members.append(h('div', { class: 'tm-link' }, [h('span', { text: nameOf(m.user) }), role,
          h('button', { type: 'button', class: 'tm-chip-x', text: '×', 'aria-label': 'برداشتن', onclick: () => { draft.members.splice(i, 1); paint(); } })]));
      });
      const add = h('select', { class: 'input', onchange: () => { if (add.value) { draft.members.push({ user: add.value, role: 'member' }); paint(); } } },
        [h('option', { value: '', text: '+ افزودن عضو' })].concat(S.people.filter(p => !draft.members.some(m => m.user === p.user)).map(p => h('option', { value: p.user, text: p.name || p.user }))));
      members.append(add);
    };
    paint();
    body.append(h('h2', { text: draft.id ? 'تنظیم فضای کار' : 'فضای کار تازه' }), name, h('div', { class: 'tm-grid' }, [dept, team]),
      h('label', { class: 'tm-row' }, [open, ' همه می‌توانند ببینند و کار کنند']), h('h3', { text: 'اعضا' }), members);
    const foot = $('tmFormFoot');
    foot.textContent = '';
    put(foot, h('button', { type: 'button', class: 'btn btn-primary', text: 'ذخیره', onclick: async () => {
      Object.assign(draft, { name: name.value, department: dept.value, team: team.value, open: open.checked });
      try { await send('POST', '/api/tasks/workspaces', draft); dlg.close(); await loadAll(); toast('ذخیره شد.'); } catch (e) { toast(e.message, true); }
    } }), h('span', { class: 'spacer' }),
    draft.id ? h('button', { type: 'button', class: 'btn btn-ghost', text: 'حذف فضای کار', onclick: async () => {
      if (!foot.dataset.sure) { foot.dataset.sure = '1'; toast('همهٔ بوردها و کارهایش هم حذف می‌شوند؛ برای حذف دوباره بزنید.', true); return; }
      try { await send('DELETE', '/api/tasks/workspaces/' + encodeURIComponent(draft.id)); dlg.close(); S.board = null; S.current = null; await loadAll(); drawHead(); drawView(); } catch (e) { toast(e.message, true); }
    } }) : null,
    h('button', { type: 'button', class: 'btn btn-ghost', text: 'بستن', onclick: () => dlg.close() }));
    delete foot.dataset.sure;
    dlg.showModal();
    name.focus();
  }
  function editBoard(b) {
    const dlg = $('tmForm');
    const body = $('tmFormBody');
    body.textContent = '';
    const draft = JSON.parse(JSON.stringify(b));
    draft.statuses = draft.statuses || [];
    draft.fields = draft.fields || [];
    draft.labels = draft.labels || [];
    const name = h('input', { class: 'input', value: draft.name || '', placeholder: 'نام بورد' });
    const kind = h('select', { class: 'input', style: 'width:auto' }, [['board', 'بورد'], ['project', 'پروژه']].map(([k, n]) => h('option', { value: k, text: n })));
    kind.value = draft.kind || 'board';
    const rows = (arr, make, add) => {
      const box = h('div', { class: 'tm-links' });
      const paint = () => {
        box.textContent = '';
        arr.forEach((x, i) => box.append(h('div', { class: 'tm-link' }, make(x).concat([
          h('button', { type: 'button', class: 'btn btn-sm btn-ghost', text: '▲', 'aria-label': 'بالاتر', onclick: () => { if (i) { [arr[i - 1], arr[i]] = [arr[i], arr[i - 1]]; paint(); } } }),
          h('button', { type: 'button', class: 'tm-chip-x', text: '×', 'aria-label': 'حذف', onclick: () => { arr.splice(i, 1); paint(); } })]))));
        box.append(h('button', { type: 'button', class: 'btn btn-sm', text: '+ افزودن', onclick: () => { arr.push(add()); paint(); } }));
      };
      paint();
      return box;
    };
    const text = (x, k, ph, w) => h('input', { class: 'input', value: x[k] ?? '', placeholder: ph, style: w ? 'width:' + w : '', oninput: e => { x[k] = e.target.value; } });
    const statuses = draft.id ? rows(draft.statuses, x => [text(x, 'name', 'نام ستون'), text(x, 'key', 'شناسه', '7em'),
      h('input', { type: 'color', value: x.color || '#3B82F6', 'aria-label': 'رنگ', oninput: e => { x.color = e.target.value; } }),
      h('input', { class: 'input', type: 'number', min: 0, value: x.wip || '', placeholder: 'سقف', style: 'width:5em', oninput: e => { x.wip = Number(e.target.value || 0); } }),
      h('label', { class: 'tm-row' }, [h('input', { type: 'checkbox', checked: !!x.done, onchange: e => { x.done = e.target.checked; } }), ' پایانی'])],
      () => ({ key: 'col' + (draft.statuses.length + 1), name: 'ستون تازه' })) : h('p', { class: 'hint', text: 'ستون‌ها: بک‌لاگ ← برای انجام ← در حال انجام ← در انتظار ← بازبینی ← تأییدشده ← انجام‌شده (پس از ساختن قابل تغییر است).' });
    const fields = rows(draft.fields, x => {
      const k = h('select', { class: 'input', style: 'width:auto', onchange: e => { x.kind = e.target.value; } },
        [['text', 'متن'], ['number', 'عدد'], ['date', 'تاریخ'], ['choice', 'انتخابی'], ['person', 'شخص'], ['check', 'تیک']].map(([a, n]) => h('option', { value: a, text: n })));
      k.value = x.kind || 'text';
      return [text(x, 'name', 'نام ستون'), k, h('input', { class: 'input', value: (x.options || []).join('، '), placeholder: 'گزینه‌ها با ، جدا',
        oninput: e => { x.options = e.target.value.split(/[،,]/).map(s => s.trim()).filter(Boolean); } })];
    }, () => ({ key: 'f' + Date.now().toString(36), name: '', kind: 'text' }));
    const labels = rows(draft.labels, x => [text(x, 'name', 'برچسب'), h('input', { type: 'color', value: x.color || '#6B7C8F', 'aria-label': 'رنگ', oninput: e => { x.color = e.target.value; } })],
      () => ({ name: '', color: '#6B7C8F' }));
    body.append(h('h2', { text: draft.id ? 'تنظیم بورد' : 'بورد تازه' }), h('div', { class: 'tm-row' }, [name, kind]),
      h('h3', { text: 'ستون‌های کانبان' }), statuses, h('h3', { text: 'ستون‌های سفارشی' }), fields, h('h3', { text: 'برچسب‌ها' }), labels);
    // A new board may start from a template: the owner's New Tender, the
    // technical offer, a sales pipeline, or any board marked as one.
    let template = null;
    if (!draft.id) {
      template = h('select', { class: 'input' }, [h('option', { value: '', text: 'بورد خالی' })]);
      api('/api/tasks/templates').then(d => (d.templates || []).forEach(t =>
        template.append(h('option', { value: t.id, text: t.name + (t.description ? ' - ' + t.description : '') })))).catch(() => {});
      body.append(h('label', { class: 'tm-field' }, [h('span', { class: 'label', text: 'از الگو' }), template]));
    } else {
      draft.rules = draft.rules || []; draft.buttons = draft.buttons || []; draft.forms = draft.forms || []; draft.webhooks = draft.webhooks || [];
      body.append(
        h('h3', { text: 'قاعده‌های خودکار (اگر … آنگاه …)' }), rulesEditor(draft),
        h('h3', { text: 'دکمه‌ها' }), h('p', { class: 'hint', text: 'بی دکمهٔ سفارشی، «شروع»، «انجام شد» و «ارجاع فوری» روی هر کار هست.' }),
        rows(draft.buttons, x => [text(x, 'name', 'نام دکمه'), h('input', { type: 'color', value: x.color || '#3B82F6', 'aria-label': 'رنگ', oninput: e => { x.color = e.target.value; } }),
          actionsEditor(x.then = x.then || [], draft)], () => ({ name: '', then: [] })),
        h('h3', { text: 'فرم‌های درخواست' }), formsEditor(draft),
        h('h3', { text: 'وب‌هوک‌ها' }), h('p', { class: 'hint', text: 'رویدادهای بورد به این نشانی‌ها فرستاده می‌شوند؛ فقط شبکهٔ داخلی یا میزبان‌های مجاز. رمز خالی یعنی همان رمز قبلی.' }),
        rows(draft.webhooks, x => [text(x, 'url', 'https://…', '16em'),
          ...['created', 'moved', 'updated', 'deleted', 'commented', 'approved'].map(ev => h('label', { class: 'tm-row' }, [
            h('input', { type: 'checkbox', checked: (x.events || []).includes(ev), onchange: e => { x.events = (x.events || []).filter(y => y !== ev).concat(e.target.checked ? [ev] : []); } }),
            ' ' + ({ created: 'ساخت', moved: 'جابه‌جایی', updated: 'تغییر', deleted: 'حذف', commented: 'یادداشت', approved: 'تأیید' }[ev])])),
          h('input', { class: 'input', type: 'password', placeholder: 'رمز امضا', style: 'width:9em', autocomplete: 'new-password', oninput: e => { x.secret = e.target.value; } })],
          () => ({ url: '', events: ['created', 'moved'] })),
        h('label', { class: 'tm-row' }, [h('input', { type: 'checkbox', checked: !!draft.template, onchange: e => { draft.template = e.target.checked; } }),
          ' این بورد الگو باشد (برای ساختن بوردهای تازه)']));
    }
    const foot = $('tmFormFoot');
    foot.textContent = '';
    put(foot, h('button', { type: 'button', class: 'btn btn-primary', text: 'ذخیره', onclick: async () => {
      draft.name = name.value; draft.kind = kind.value;
      draft.fields = draft.fields.filter(f => f.name.trim());
      draft.labels = draft.labels.filter(l => l.name.trim());
      try {
        const saved = template && template.value
          ? await send('POST', '/api/tasks/boards/from-template', { workspace_id: draft.workspace_id, template: template.value, name: name.value })
          : await send('POST', '/api/tasks/boards', draft);
        dlg.close(); await loadAll(); openBoard(saved.id, true); toast('ذخیره شد.');
      } catch (e) { toast(e.message, true); }
    } }), h('span', { class: 'spacer' }),
    draft.id && canOwn() ? h('button', { type: 'button', class: 'btn btn-ghost', text: 'حذف بورد', onclick: async () => {
      if (!foot.dataset.sure) { foot.dataset.sure = '1'; toast('همهٔ کارهایش هم حذف می‌شوند؛ برای حذف دوباره بزنید.', true); return; }
      try { await send('DELETE', '/api/tasks/boards/' + encodeURIComponent(draft.id)); dlg.close(); S.board = null; S.current = null; await loadAll(); drawHead(); drawView(); } catch (e) { toast(e.message, true); }
    } }) : null,
    h('button', { type: 'button', class: 'btn btn-ghost', text: 'بستن', onclick: () => dlg.close() }));
    delete foot.dataset.sure;
    dlg.showModal();
    name.focus();
  }

  // --------------------------------------------- rules, actions and forms
  const ACTIONS = { set_status: 'بردن به ستون', set_priority: 'اولویت', assign: 'سپردن به', unassign: 'برداشتن مسئولان',
    add_watcher: 'افزودن دنبال‌کننده', add_label: 'افزودن برچسب', remove_label: 'برداشتن برچسب', set_group: 'گروه', set_due: 'سررسید (روز بعد)',
    add_check: 'افزودن مورد بررسی', complete_checklist: 'کامل کردن فهرست بررسی', notify: 'خبر دادن به', create_item: 'ساختن کار',
    comment: 'نوشتن یادداشت', start_timer: 'روشن کردن زمان‌سنج', stop_timer: 'خاموش کردن زمان‌سنج' };
  const TRIGGERS = { created: 'کار ساخته شد', moved: 'کار به ستون رفت', assigned: 'به کسی سپرده شد', commented: 'یادداشت تازه',
    due_soon: 'سررسید نزدیک است', overdue: 'سررسید گذشت' };
  const PEOPLE_HINT = 'نام‌های کاربری با ، جدا؛ یا @creator @assignees @watchers @owners @actor';
  const csv = s => String(s || '').split(/[،,]/).map(x => x.trim()).filter(Boolean);
  function statusSelect(board, value, set, any) {
    const s = h('select', { class: 'input', style: 'width:auto', onchange: () => set(s.value) },
      (any ? [h('option', { value: '', text: 'هر ستون' })] : []).concat((board.statuses || []).map(st => h('option', { value: st.key, text: st.name }))));
    s.value = value || '';
    return s;
  }
  function actionsEditor(list, board) {
    const box = h('div', { class: 'tm-links', style: 'flex:1 1 100%' });
    const paint = () => {
      box.textContent = '';
      list.forEach((a, i) => {
        const kind = h('select', { class: 'input', style: 'width:auto', onchange: () => { a.kind = kind.value; paint(); } },
          Object.entries(ACTIONS).map(([k, n]) => h('option', { value: k, text: n })));
        kind.value = a.kind;
        const val = (ph, key, w) => h('input', { class: 'input', placeholder: ph, value: a[key] ?? '', style: w ? 'width:' + w : '', oninput: e => { a[key] = e.target.value; } });
        const extra = {
          set_status: () => [statusSelect(board, a.value, v => { a.value = v; })],
          set_priority: () => { const s = h('select', { class: 'input', style: 'width:auto', onchange: () => { a.value = s.value; } },
            Object.entries(PRI).map(([k, n]) => h('option', { value: k, text: n }))); s.value = a.value || 'normal'; a.value = s.value; return [s]; },
          assign: () => [h('input', { class: 'input', placeholder: PEOPLE_HINT, value: (a.users || []).join('، '), oninput: e => { a.users = csv(e.target.value); } })],
          add_watcher: () => [h('input', { class: 'input', placeholder: PEOPLE_HINT, value: (a.users || []).join('، '), oninput: e => { a.users = csv(e.target.value); } })],
          notify: () => [h('input', { class: 'input', placeholder: PEOPLE_HINT, value: (a.users || []).join('، '), oninput: e => { a.users = csv(e.target.value); } }),
            val('متن پیام، مثلاً «{title}» رسید', 'title')],
          add_label: () => [val('برچسب', 'value')], remove_label: () => [val('برچسب', 'value')], set_group: () => [val('گروه', 'value')],
          add_check: () => [val('مورد', 'value')], comment: () => [val('متن یادداشت', 'value')],
          set_due: () => [h('input', { class: 'input', type: 'number', value: a.days ?? 2, style: 'width:5em', oninput: e => { a.days = Number(e.target.value || 0); } }), ' روز'],
          create_item: () => [val('عنوان کار، مثلاً بررسی فنی {title}', 'title'),
            h('input', { class: 'input', type: 'number', value: a.days ?? '', placeholder: 'سررسید (روز)', style: 'width:7em', oninput: e => { a.days = Number(e.target.value || 0); } }),
            h('label', { class: 'tm-row' }, [h('input', { type: 'checkbox', checked: !!a.sub, onchange: e => { a.sub = e.target.checked; } }), ' زیرکار'])],
        }[a.kind];
        box.append(h('div', { class: 'tm-link' }, [kind, ...(extra ? extra() : []),
          h('button', { type: 'button', class: 'tm-chip-x', text: '×', 'aria-label': 'حذف', onclick: () => { list.splice(i, 1); paint(); } })]));
      });
      box.append(h('button', { type: 'button', class: 'btn btn-sm', text: '+ کار', onclick: () => { list.push({ kind: 'set_status', value: (board.statuses[0] || {}).key }); paint(); } }));
    };
    paint();
    return box;
  }
  function rulesEditor(board) {
    const box = h('div', { class: 'tm-links' });
    const paint = () => {
      box.textContent = '';
      board.rules.forEach((r, i) => {
        r.when = r.when || { kind: 'created' }; r.if = r.if || []; r.then = r.then || [];
        const trig = h('select', { class: 'input', style: 'width:auto', onchange: () => { r.when.kind = trig.value; paint(); } },
          Object.entries(TRIGGERS).map(([k, n]) => h('option', { value: k, text: n })));
        trig.value = r.when.kind;
        const conds = h('div', { class: 'tm-links' });
        r.if.forEach((c, j) => {
          const f = h('select', { class: 'input', style: 'width:auto', onchange: () => { c.field = f.value; } },
            [['priority', 'اولویت'], ['label', 'برچسب'], ['assignee', 'مسئول'], ['status', 'ستون'], ['group', 'گروه'], ['title', 'عنوان']].map(([k, n]) => h('option', { value: k, text: n })));
          f.value = c.field || 'priority';
          const op = h('select', { class: 'input', style: 'width:auto', onchange: () => { c.op = op.value; } },
            [['is', 'برابر'], ['is_not', 'نابرابر'], ['has', 'دارد'], ['lacks', 'ندارد'], ['contains', 'شامل']].map(([k, n]) => h('option', { value: k, text: n })));
          op.value = c.op || 'is';
          conds.append(h('div', { class: 'tm-link' }, ['اگر', f, op, h('input', { class: 'input', value: c.value || '', placeholder: 'مقدار (مثلاً high یا فوری)', oninput: e => { c.value = e.target.value; } }),
            h('button', { type: 'button', class: 'tm-chip-x', text: '×', 'aria-label': 'حذف', onclick: () => { r.if.splice(j, 1); paint(); } })]));
        });
        conds.append(h('button', { type: 'button', class: 'btn btn-sm', text: '+ شرط', onclick: () => { r.if.push({ field: 'priority', op: 'is', value: '' }); paint(); } }));
        box.append(h('div', { class: 'tm-rule' }, [
          h('div', { class: 'tm-link' }, [h('input', { type: 'checkbox', checked: r.enabled !== false, 'aria-label': 'روشن', onchange: e => { r.enabled = e.target.checked; } }),
            h('input', { class: 'input', value: r.name || '', placeholder: 'نام قاعده', style: 'width:12em', oninput: e => { r.name = e.target.value; } }),
            'وقتی', trig,
            r.when.kind === 'moved' ? statusSelect(board, r.when.status, v => { r.when.status = v; }, true) : null,
            r.when.kind === 'due_soon' ? h('input', { class: 'input', type: 'number', min: 0, value: r.when.days ?? 1, style: 'width:4em', oninput: e => { r.when.days = Number(e.target.value || 0); } }) : null,
            r.when.kind === 'due_soon' ? 'روز مانده' : null,
            h('button', { type: 'button', class: 'tm-chip-x', text: '×', 'aria-label': 'حذف قاعده', onclick: () => { board.rules.splice(i, 1); paint(); } })]),
          conds, h('span', { class: 'hint', text: 'آنگاه:' }), actionsEditor(r.then, board)]));
      });
      box.append(h('button', { type: 'button', class: 'btn btn-sm', text: '+ قاعده', onclick: () => {
        board.rules.push({ name: '', enabled: true, when: { kind: 'created' }, if: [], then: [] }); paint(); } }));
    };
    paint();
    return box;
  }
  // Who a form's requests go to (tasks/forms.go): the people above, the
  // sender's manager, the colleague a question names, or a department's
  // members -- and the people above when the rule names nobody.
  function assignEditor(f) {
    const box = h('div', { class: 'tm-link' });
    const paint = () => {
      box.textContent = '';
      const mode = (f.assign && f.assign.mode) || 'users';
      const pick = h('select', { class: 'input', style: 'width:auto', 'aria-label': 'سپردن درخواست به', onchange: () => {
        f.assign = pick.value === 'users' ? null : { mode: pick.value };
        paint();
      } }, [['users', 'مسئولان بالا'], ['manager', 'مدیر فرستنده'], ['field', 'همکاری که در فرم انتخاب شود'], ['department', 'اعضای یک واحد']]
        .map(([v, n]) => h('option', { value: v, text: n, selected: v === mode })));
      box.append(h('span', { class: 'hint', text: 'سپردن به:' }), pick);
      if (mode === 'field') {
        const people = (f.fields || []).filter(q => q.kind === 'person' && q.key);
        if (!people.length) box.append(h('span', { class: 'hint', text: 'پرسشی از نوع «یکی از همکاران» با شناسه بسازید.' }));
        else {
          if (!people.some(q => q.key === f.assign.field)) f.assign.field = people[0].key;
          box.append(h('select', { class: 'input', style: 'width:auto', 'aria-label': 'کدام پرسش', onchange: e => { f.assign.field = e.target.value; } },
            people.map(q => h('option', { value: q.key, text: q.label || q.key, selected: q.key === f.assign.field }))));
        }
      }
      if (mode === 'department') {
        const deps = S.departments || [];
        if (!deps.length) box.append(h('span', { class: 'hint', text: 'واحدی تعریف نشده است.' }));
        else {
          if (!deps.some(d => d.id === f.assign.department)) f.assign.department = deps[0].id;
          box.append(h('select', { class: 'input', style: 'width:auto', 'aria-label': 'کدام واحد', onchange: e => { f.assign.department = Number(e.target.value); } },
            deps.map(d => h('option', { value: d.id, text: d.name, selected: d.id === f.assign.department }))));
        }
      }
      if (mode === 'manager') box.append(h('span', { class: 'hint', text: 'سرپرست واحد اصلی فرستنده (صفحهٔ واحدها)' }));
    };
    paint();
    return box;
  }
  function formsEditor(board) {
    const box = h('div', { class: 'tm-links' });
    const paint = () => {
      box.textContent = '';
      board.forms.forEach((f, i) => {
        f.fields = f.fields || [];
        // The questions in the form maker (formmaker.js), with a preview.
        const qs = h('div', { class: 'tm-link' }, [
          h('span', { class: 'hint', text: f.fields.length ? f.fields.filter(q => q.kind !== 'heading').map(q => q.label).filter(Boolean).slice(0, 6).join('، ') + (f.fields.length > 6 ? '، …' : '') : 'هنوز پرسشی ندارد.' }),
          h('button', { type: 'button', class: 'btn btn-sm', text: 'ویرایش پرسش‌ها (' + faN(f.fields.length) + ')', onclick: async () => {
            if (await window.FormMaker.open(f.fields, { title: 'پرسش‌های فرم «' + (f.name || 'بی‌نام') + '»', people: S.people })) paint();
          } })]);
        box.append(h('div', { class: 'tm-rule' }, [
          h('div', { class: 'tm-link' }, [h('input', { class: 'input', value: f.name || '', placeholder: 'نام فرم', style: 'width:12em', oninput: e => { f.name = e.target.value; } }),
            'به ستون', statusSelect(board, f.status, v => { f.status = v; }, true),
            h('input', { class: 'input', value: (f.assignees || []).join('، '), placeholder: 'مسئولان (نام کاربری)', style: 'width:12em', oninput: e => { f.assignees = csv(e.target.value); } }),
            h('label', { class: 'tm-row' }, [h('input', { type: 'checkbox', checked: !!f.open, onchange: e => { f.open = e.target.checked; } }), ' برای همه']),
            h('button', { type: 'button', class: 'tm-chip-x', text: '×', 'aria-label': 'حذف فرم', onclick: () => { board.forms.splice(i, 1); paint(); } })]),
          h('textarea', { class: 'input', rows: 2, value: f.intro || '', placeholder: 'توضیح بالای فرم', oninput: e => { f.intro = e.target.value; } }),
          assignEditor(f), qs]));
      });
      box.append(h('button', { type: 'button', class: 'btn btn-sm', text: '+ فرم', onclick: () => {
        board.forms.push({ name: '', fields: [{ label: 'عنوان درخواست', key: 'title', kind: 'text', required: true }], assignees: [], labels: [] }); paint(); } }));
    };
    paint();
    return box;
  }

  // ---------------------------------------------------------------- boot
  async function boot() {
    // Without the task manager, the steps that wait for somebody are still theirs.
    try { await loadAll(); } catch (e) { S.flowOnly = true; S.workspaces = []; return openSpecial('flow', false); }
    const u = readURL();
    if (u.view) S.view = u.view;
    // A process link (taskflow.js opens its case or start form): the inbox
    // underneath, not a board the reader may not see.
    const q = new URLSearchParams(location.search);
    if (q.get('case') || q.get('flow')) return openSpecial('flow', false);
    if (u.item) { await openItem({ id: u.item }); if (S.board) { S.current = S.board.id; drawSide(); drawHead(); drawView(); } return; }
    if (isSpecial(u.board) || !u.board) {
      const first = S.workspaces.flatMap(w => w.boards || [])[0];
      if (!u.board && first) return openBoard(first.id, false);
      return openSpecial(u.board || 'my', false);
    }
    openBoard(u.board, false);
  }
  window.addEventListener('taskflow:changed', () => {
    if (S.current === 'flow') openSpecial('flow', false); else if (S.board && S.board.process) openBoard(S.board.id, false);
  });
  window.addEventListener('popstate', () => {
    const u = readURL();
    S.view = u.view || 'kanban';
    if (isSpecial(u.board)) openSpecial(u.board, false); else if (u.board) openBoard(u.board, false);
  });
  window.DocfindTasks = { boot };
})();
