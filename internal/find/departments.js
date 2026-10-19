// Departments on the settings page: created, renamed, moved and deleted
// here, with each person in any number of them and one of them primary.
// Mounted by the settings page; the per-user dialog is opened from a user row.
(function () {
  'use strict';
  window.DocfindDepartments = { mount, openFor };
  let card = null, data = null, api = null, say = null;

  function esc(s) { return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])); }

  // The settings page's api() answers with the Response itself, not its JSON:
  // read as JSON it listed no departments and swallowed every error. Read
  // either shape, and a refusal as the error it is.
  function asJSON(apiFn) {
    return async (path, opts) => {
      const r = await apiFn(path, opts);
      if (!r || typeof r.json !== 'function') return r;
      const d = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(d.error || 'انجام نشد.');
      return d;
    };
  }

  function mount(cardEl, apiFn, sayFn) {
    card = cardEl; api = asJSON(apiFn); say = sayFn;
    if (!card) return;
    card.querySelector('#depAdd').addEventListener('click', () => save({ name: card.querySelector('#depName').value, parent_id: Number(card.querySelector('#depParent').value) || null }));
    card.querySelector('#depList').addEventListener('click', onListClick);
    load();
  }

  async function load() {
    try {
      data = await api('/api/departments');
      draw();
    } catch (e) { if (e.message !== 'unauthenticated') note(e.message); }
  }
  function note(t, ok) { say(card.querySelector('#depMsg'), t, ok); }

  // By parent, name sorted, drawn as a tree.
  function draw() {
    const deps = data.departments || [];
    const byParent = {};
    for (const d of deps) (byParent[d.parent_id || 0] = byParent[d.parent_id || 0] || []).push(d);
    const count = {};
    for (const m of data.memberships || []) count[m.department_id] = (count[m.department_id] || 0) + 1;
    const opts = ['<option value="">(بدون والد)</option>'].concat(deps.filter(d => d.kind !== 'position').map(d => `<option value="${d.id}">${esc(d.name)}</option>`)).join('');
    card.querySelector('#depParent').innerHTML = opts;
    const line = (d, depth) => `<div class="dep-row" style="padding-inline-start:${depth * 18 + 4}px">
      <span>${esc(d.name)}${d.kind === 'position' ? ' <span class="hint">(سمت)</span>' : ''}${count[d.id] ? ' <span class="chip chip-sm">' + count[d.id].toLocaleString('fa-IR') + ' نفر</span>' : ''}${d.active ? '' : ' <span class="hint">(غیرفعال)</span>'}${d.head ? ' <span class="hint">· سرپرست: ' + esc(d.head) + '</span>' : ''}</span>
      <span class="dep-acts"><button type="button" class="btn btn-sm" data-edit="${d.id}">ویرایش</button>
        <button type="button" class="btn btn-sm" data-head="${d.id}" title="کار «مدیر درخواست‌کننده» در فرآیندها به سرپرست واحد اصلی او می‌رسد">سرپرست</button>
        <button type="button" class="btn btn-sm" data-del="${d.id}">حذف</button></span></div>`;
    const walk = (pid, depth) => (byParent[pid] || []).sort((a, b) => a.name.localeCompare(b.name, 'fa')).map(d => line(d, depth) + walk(d.id, depth + 1)).join('');
    card.querySelector('#depList').innerHTML = walk(0, 0) || '<p class="hint">هنوز واحدی نیست. نام واحد را بنویسید و «افزودن» را بزنید.</p>';
    window._depById = Object.fromEntries(deps.map(d => [d.id, d]));
  }

  async function save(x) {
    if (!String(x.name || '').trim()) { note('نام واحد لازم است.'); return; }
    try {
      await api('/api/departments', { method: x.id ? 'PUT' : 'POST', body: JSON.stringify(x) });
      card.querySelector('#depName').value = '';
      note('ذخیره شد.', true);
      load();
    } catch (e) { note(e.message); }
  }
  async function onListClick(e) {
    const ed = e.target.closest('[data-edit]'), del = e.target.closest('[data-del]'), hd = e.target.closest('[data-head]');
    // Every save sends the whole department, its head included: a PUT
    // without one would set it to nobody.
    const whole = d => ({ id: d.id, name: d.name, parent_id: d.parent_id || null, kind: d.kind, active: d.active, head: d.head || '' });
    if (ed) {
      const d = window._depById[ed.dataset.edit];
      const nn = await inlineName(ed, d.name);
      if (nn != null && nn !== d.name) save(Object.assign(whole(d), { name: nn }));
      return;
    }
    if (hd) {
      const d = window._depById[hd.dataset.head];
      const who = await inlineHead(hd, d);
      if (who != null && who !== (d.head || '')) save(Object.assign(whole(d), { head: who }));
      return;
    }
    if (del) {
      const d = window._depById[del.dataset.del];
      if (!confirm('واحد «' + d.name + '» حذف شود؟ زیرواحدهایش یک پله بالا می‌روند و اعضا فقط از آن خارج می‌شوند.')) return;
      try { await api('/api/departments?id=' + d.id, { method: 'DELETE' }); note('حذف شد.', true); load(); }
      catch (err) { note(err.message); }
    }
  }
  // Who heads a department, chosen inline: its own members first, then
  // everybody else who is in some department.
  function inlineHead(anchor, d) {
    return new Promise(res => {
      const ms = data.memberships || [];
      const own = [...new Set(ms.filter(m => m.department_id === d.id).map(m => m.username))].sort();
      const rest = [...new Set(ms.map(m => m.username))].filter(u => !own.includes(u)).sort();
      const sel = document.createElement('select');
      sel.className = 'select'; sel.style.maxWidth = '220px';
      sel.setAttribute('aria-label', 'سرپرست ' + d.name);
      const add = (v, t, g) => { const o = document.createElement('option'); o.value = v; o.textContent = t; o.selected = v === (d.head || ''); (g || sel).append(o); };
      add('', '(هیچ‌کس)');
      for (const [label, list] of [['اعضای این واحد', own], ['دیگران', rest]]) {
        if (!list.length) continue;
        const g = document.createElement('optgroup'); g.label = label; sel.append(g);
        list.forEach(u => add(u, u, g));
      }
      anchor.replaceWith(sel); sel.focus();
      let over = false;
      const done = ok => { if (over) return; over = true; const v = sel.value; sel.replaceWith(anchor); res(ok ? v : null); };
      sel.addEventListener('change', () => done(true));
      sel.addEventListener('keydown', ev => { if (ev.key === 'Escape') done(false); });
      sel.addEventListener('blur', () => done(false), { once: true });
    });
  }
  // A one-field inline rename, since a modal prompt is blocked in the app.
  function inlineName(anchor, cur) {
    return new Promise(res => {
      const inp = document.createElement('input');
      inp.className = 'input'; inp.value = cur; inp.style.maxWidth = '220px';
      anchor.replaceWith(inp); inp.focus(); inp.select();
      const done = ok => { const v = inp.value; inp.replaceWith(anchor); res(ok ? v : null); };
      inp.addEventListener('keydown', ev => { if (ev.key === 'Enter') done(true); if (ev.key === 'Escape') done(false); });
      inp.addEventListener('blur', () => done(true), { once: true });
    });
  }
  // The per-user dialog: which departments this person is in, one primary.
  async function openFor(username) {
    const dlg = document.getElementById('depDlg');
    if (!data) { try { data = await api('/api/departments'); } catch (e) { return; } }
    document.getElementById('depWho').textContent = username;
    const mine = (data.memberships || []).filter(m => m.username === username);
    const inSet = new Set(mine.map(m => m.department_id));
    const primary = (mine.find(m => m.primary) || {}).department_id || 0;
    const deps = (data.departments || []).filter(d => d.active).sort((a, b) => a.name.localeCompare(b.name, 'fa'));
    document.getElementById('depPick').innerHTML = deps.map(d => `<label class="dep-pick">
      <input type="checkbox" value="${d.id}" ${inSet.has(d.id) ? 'checked' : ''}>
      <input type="radio" name="depPrimary" value="${d.id}" ${d.id === primary ? 'checked' : ''} title="اصلی">
      ${esc(d.name)}</label>`).join('') || '<p class="hint">واحدی نیست.</p>';
    const msg = document.getElementById('depDlgMsg'); msg.className = 'msg notice hidden';
    if (!dlg.open) dlg.showModal();
    document.getElementById('depDlgSave').onclick = async () => {
      const ids = Array.from(document.querySelectorAll('#depPick input[type=checkbox]:checked')).map(i => Number(i.value));
      const pr = Number((document.querySelector('#depPick input[name=depPrimary]:checked') || {}).value) || 0;
      try {
        await api('/api/users/departments', { method: 'PUT', body: JSON.stringify({ username, ids, primary: ids.includes(pr) ? pr : 0 }) });
        data = await api('/api/departments');
        dlg.close();
        if (window.toast) window.toast('واحدهای ' + username + ' ذخیره شد.', 'ok');
      } catch (e) { say(msg, e.message); }
    };
  }
})();
