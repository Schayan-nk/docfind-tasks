// Processes in the task manager (taskflow.go, internal/tasks/flow.go):
// the inbox of open steps, starting a case, and a case's own window -- what it
// holds, who did what, and the form of the step that waits for the reader.
// tasks.js shows the inbox as its «صندوق کار» view and opens a process
// board's items here. Everything that came from a person is set as text.
(function () {
  'use strict';
  function h(tag, props, kids) {
    const n = document.createElement(tag);
    for (const [k, v] of Object.entries(props || {})) {
      if (v == null || v === false) continue;
      if (k === 'class') n.className = v;
      else if (k === 'text') n.textContent = v;
      else if (k.startsWith('on')) n.addEventListener(k.slice(2), v);
      else if (k in n && typeof n[k] !== 'function') n[k] = v;
      else n.setAttribute(k, v);
    }
    for (const c of [].concat(kids == null ? [] : kids)) if (c != null && c !== false) n.append(c);
    return n;
  }
  async function api(path, opts) {
    const r = await fetch(path, Object.assign({ credentials: 'same-origin' }, opts || {}));
    if (r.status === 401) { location.replace('/login?next=' + encodeURIComponent(location.pathname + location.search)); throw new Error('signed out'); }
    const d = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(d.error || 'انجام نشد.');
    return d;
  }
  const send = (path, body) => api(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}) });
  const fa = s => String(s).replace(/\d/g, c => '۰۱۲۳۴۵۶۷۸۹'[c]);
  const latin = s => String(s == null ? '' : s).replace(/[۰-۹]/g, c => '۰۱۲۳۴۵۶۷۸۹'.indexOf(c)).replace(/[٠-٩]/g, c => '٠١٢٣٤٥٦٧٨٩'.indexOf(c));
  const group = s => String(s).replace(/\B(?=(\d{3})+(?!\d))/g, ',');
  const DP = () => window.docfindDatePicker;
  const pad = n => String(n).padStart(2, '0');
  function toJalali(iso) {
    if (!iso || !DP()) return iso || '';
    const [y, m, d] = String(iso).slice(0, 10).split('-').map(Number);
    const [jy, jm, jd] = DP().gregorianToJalali(y, m, d);
    return jy + '/' + pad(jm) + '/' + pad(jd);
  }
  function fromJalali(s) {
    if (!s || !DP()) return '';
    const p = DP().parseDate(latin(s));
    if (!p) return s;
    const [y, m, d] = p[0] < 1700 ? DP().jalaliToGregorian(p[0], p[1], p[2]) : p;
    return y + '-' + pad(m) + '-' + pad(d);
  }
  const when = iso => {
    if (!iso) return '';
    const d = new Date(iso);
    return fa(toJalali(d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate())) + ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes()));
  };

  let people = [];
  const nameOf = u => { const p = people.find(x => x.user === u); return p ? (p.name || u) : u; };

  // A choice's options: {value, label}, or the plain list older intake forms
  // were saved with.
  const choicesOf = f => (f.choices && f.choices.length ? f.choices : (f.options || []).map(o => ({ value: o, label: o })));
  // Options or a value that come from docfind's lists or a database; an
  // imported map's "customers" is only a mark (internal/tasks/fielddata.go).
  const isSourced = f => ['people', 'departments'].includes(f.source) || /^db:/.test(f.source || '');

  // ------------------------------------------------------------- a value
  function shown(f, v) {
    if (v == null || v === '') return '—';
    const label = x => { const c = choicesOf(f).find(c => c.value === String(x)); return c ? c.label : String(x); };
    switch (f.kind) {
      case 'choice': return f.source === 'people' ? nameOf(v) : label(v);
      case 'multi': return [].concat(v).map(label).join('، ');
      case 'money': return fa(group(Math.round(Number(v)))) + ' ریال';
      case 'number': return fa(v);
      case 'calc': return calcText(v);
      case 'file': return [].concat(v || []).map(x => x.name).join('، ') || '—';
      case 'date': return fa(toJalali(v));
      case 'check': return v === true || v === '1' || v === 1 ? 'بله' : 'خیر';
      case 'person': return nameOf(v);
      case 'grid': return fa([].concat(v).length) + ' ردیف';
    }
    return String(v);
  }
  // A calculated value: grouped, in Persian digits, as many decimals as it has.
  function calcText(v) {
    const n = Number(v);
    if (v == null || v === '' || !isFinite(n)) return '—';
    const [int, dec] = String(Math.abs(n)).split('.');
    return (n < 0 ? '−' : '') + fa(group(int) + (dec ? '٫' + dec : ''));
  }
  // A question's files: each a link that downloads it (taskfiles.go).
  const sizeText = n => (n >= 1 << 20 ? fa((n / (1 << 20)).toFixed(1)) + ' مگابایت' : fa(Math.max(1, Math.round(n / 1024))) + ' کیلوبایت');
  function fileLinks(v, remove) {
    const files = [].concat(v || []).filter(x => x && x.id);
    if (!files.length) return h('span', { text: '—' });
    return h('ul', { class: 'tf-files' }, files.map((x, i) => h('li', {}, [
      h('a', { href: '/api/tasks/files/' + encodeURIComponent(x.id), download: x.name || '', text: x.name || 'پیوست' }),
      h('span', { class: 'hint', text: sizeText(Number(x.size) || 0) }),
      remove ? h('button', { type: 'button', class: 'btn btn-sm btn-ghost', 'aria-label': 'برداشتن ' + (x.name || ''), text: 'برداشتن', onclick: () => remove(i) }) : null])));
  }
  function gridTable(f, rows) {
    const cols = f.columns || [];
    return h('div', { class: 'table-scroll' }, h('table', { class: 'data' }, [
      h('thead', {}, h('tr', {}, cols.map(c => h('th', { text: c.label })))),
      h('tbody', {}, [].concat(rows || []).map(r => h('tr', {}, cols.map(c => h('td', { text: shown(c, r[c.key]) })))))]));
  }

  // ------------------------------------------------------------- a field
  // field draws one question and returns {el, read}; read is what goes to the
  // server (the server checks it again).
  function field(f, value) {
    if (f.kind === 'heading') return { el: h('h4', { class: 'tf-heading', text: f.label }), read: null };
    const id = 'tf-' + f.key + '-' + Math.random().toString(36).slice(2, 7);
    const lab = h('label', { class: 'label', for: id, text: f.label + (f.required ? ' *' : '') });
    let input, read;
    switch (f.kind) {
      case 'textarea':
        input = h('textarea', { class: 'input', id, rows: 3, value: value || '' });
        read = () => input.value;
        break;
      case 'number':
        input = h('input', { class: 'input', id, inputmode: 'decimal', dir: 'ltr', value: value == null ? '' : value });
        read = () => latin(input.value).trim();
        break;
      case 'money':
        input = h('input', { class: 'input', id, inputmode: 'numeric', dir: 'ltr', value: value == null || value === '' ? '' : group(value) });
        input.addEventListener('blur', () => { const d = latin(input.value).replace(/[^\d]/g, ''); input.value = d ? group(d) : ''; });
        read = () => latin(input.value).replace(/[^\d]/g, '');
        break;
      case 'date':
        input = h('input', { class: 'input', id, value: value ? toJalali(value) : '' });
        if (DP()) DP().attach(input);
        read = () => input.value ? fromJalali(input.value) : '';
        break;
      case 'choice': {
        // A dropdown, radios, or -- display "search" -- a dropdown with a box
        // to narrow it; options may come from the server later (setChoices).
        let current = value == null ? '' : String(value);
        if (f.display === 'radio') {
          input = h('div', { class: 'tf-checks tf-radios', id, role: 'radiogroup', 'aria-label': f.label });
          var setChoices = cs => {
            input.textContent = '';
            cs.forEach(c => input.append(h('label', {}, [h('input', { type: 'radio', name: id, value: c.value, checked: c.value === current,
              onchange: () => { current = c.value; } }), ' ' + c.label])));
            if (!cs.length) input.append(h('span', { class: 'hint', text: isSourced(f) ? 'گزینه‌ای نیامد.' : 'گزینه‌ای ندارد.' }));
          };
          read = () => current;
        } else {
          const sel = h('select', { class: 'select', id, onchange: e => { current = e.target.value; } });
          const seen = new Map();
          setChoices = cs => {
            sel.textContent = '';
            sel.append(h('option', { value: '', text: '—' }));
            cs.forEach(c => { seen.set(c.value, c.label); sel.append(h('option', { value: c.value, text: c.label, selected: c.value === current })); });
            // The pick stays, named as it was last seen, when a search leaves it out.
            if (current && !cs.some(c => c.value === current)) sel.append(h('option', { value: current, text: seen.get(current) || shown(f, current), selected: true }));
          };
          input = sel;
          read = () => sel.value;
          if (f.display === 'search') {
            const box = h('input', { class: 'input tf-search', type: 'search', placeholder: 'جست‌وجو…', 'aria-label': 'جست‌وجو در ' + f.label });
            input = h('div', { class: 'tf-picker' }, [box, sel]);
            input.search = box;
          }
        }
        setChoices(isSourced(f) ? [] : choicesOf(f));
        input.setChoices = setChoices;
        break;
      }
      case 'multi': {
        let picked = [].concat(value || []).map(String);
        let boxes = [];
        input = h('div', { class: 'tf-checks', id, role: 'group' });
        input.setChoices = cs => {
          picked = boxes.length ? boxes.filter(b => b.checked).map(b => b.value) : picked;
          boxes = cs.map(c => h('input', { type: 'checkbox', value: c.value, checked: picked.includes(c.value) }));
          input.textContent = '';
          cs.forEach((c, i) => input.append(h('label', {}, [boxes[i], ' ' + c.label])));
          if (!cs.length && isSourced(f)) input.append(h('span', { class: 'hint', text: 'گزینه‌ای نیامد.' }));
        };
        input.setChoices(isSourced(f) ? [] : choicesOf(f));
        read = () => boxes.filter(b => b.checked).map(b => b.value);
        break;
      }
      case 'check':
        input = h('input', { type: 'checkbox', id, checked: value === true || value === '1' });
        read = () => input.checked ? '1' : '0';
        break;
      case 'person':
        input = h('select', { class: 'select', id }, [h('option', { value: '', text: '—' })]
          .concat(people.map(p => h('option', { value: p.user, text: p.name || p.user, selected: value === p.user }))));
        read = () => input.value;
        break;
      case 'grid':
        return gridField(f, value, lab);
      case 'file': {
        // Each file is sent as it is picked; the answer is the list of them.
        let files = [].concat(value || []).filter(x => x && x.id);
        const pick = h('input', { type: 'file', id, multiple: true, class: 'tf-file-pick' });
        const list = h('div');
        const say = h('span', { class: 'hint', role: 'status' });
        const paint = () => { list.textContent = ''; list.append(fileLinks(files, i => { files.splice(i, 1); paint(); input.dispatchEvent(new Event('change', { bubbles: true })); })); };
        pick.addEventListener('change', async () => {
          const chosen = [...pick.files];
          pick.value = '';
          for (const [i, file] of chosen.entries()) {
            say.textContent = 'در حال فرستادن ' + fa(i + 1) + ' از ' + fa(chosen.length) + '…';
            const body = new FormData();
            body.append('file', file);
            try {
              const r = await fetch('/api/tasks/files', { method: 'POST', credentials: 'same-origin', body });
              const d = await r.json().catch(() => ({}));
              if (!r.ok) throw new Error(d.error || 'فرستاده نشد.');
              files.push({ id: d.id, name: d.name, size: d.size });
            } catch (e) { say.textContent = '«' + file.name + '»: ' + e.message; paint(); return; }
          }
          say.textContent = '';
          paint();
          input.dispatchEvent(new Event('change', { bubbles: true }));
        });
        input = h('div', { class: 'tf-filebox' }, [pick, list, say]);
        paint();
        read = () => files.map(x => ({ id: x.id, name: x.name, size: x.size }));
        break;
      }
      case 'calc': {
        const out = h('output', { class: 'input tf-calc', id, dir: 'ltr', 'aria-live': 'polite', text: calcText(value) });
        const kids = [h('span', { class: 'label', text: f.label }), out];
        if (f.hint) kids.push(h('p', { class: 'hint', text: f.hint }));
        return { el: h('div', { class: 'tm-field' }, kids), read: null, lab: null, set: v => { out.textContent = calcText(v); } };
      }
      default:
        input = h('input', { class: 'input', id, value: value == null ? '' : value });
        read = () => input.value;
    }
    const kids = [lab, input];
    if (isSourced(f)) {
      // Options (or, for a text question, its value) the server will send.
      const set = input.setChoices || (v => {
        const first = v[0] ? v[0].value : '';
        input.value = f.kind === 'money' && first ? group(latin(first).replace(/[^\d]/g, '')) : first;
      });
      var load = set;
    }
    const range = f.min != null && f.max != null ? 'بین ' + fa(f.min) + ' و ' + fa(f.max)
      : f.min != null ? 'دست‌کم ' + fa(f.min) : f.max != null ? 'حداکثر ' + fa(f.max) : '';
    if (f.hint || range) kids.push(h('p', { class: 'hint', text: [f.hint, range].filter(Boolean).join(' · ') }));
    const wide = f.kind === 'textarea' || f.kind === 'multi' || f.kind === 'file';
    return { el: h('div', { class: 'tm-field' + (wide ? ' tf-wide' : '') }, kids), read, lab, load, search: input.search };
  }
  // A table: a row per line, each cell a column's field; rows added and taken.
  function gridField(f, value, lab) {
    const rows = [];
    const body = h('tbody');
    const add = v => {
      const cells = (f.columns || []).map(c => field(c, (v || {})[c.key]));
      const tr = h('tr', {}, cells.map(c => { const lb = c.el.querySelector('label'); if (lb) lb.classList.add('sr-only'); return h('td', {}, c.el); }));
      const row = { tr, cells };
      tr.append(h('td', {}, h('button', { type: 'button', class: 'btn btn-sm btn-ghost', text: 'حذف ردیف',
        onclick: () => { rows.splice(rows.indexOf(row), 1); tr.remove(); } })));
      rows.push(row); body.append(tr);
    };
    [].concat(value || []).forEach(add);
    const el = h('div', { class: 'tm-field tf-wide' }, [lab, h('div', { class: 'table-scroll' }, h('table', { class: 'data' }, [
      h('thead', {}, h('tr', {}, (f.columns || []).map(c => h('th', { text: c.label + (c.required ? ' *' : '') })).concat([h('th', {})]))), body])),
      h('button', { type: 'button', class: 'btn btn-sm', text: '+ ردیف', onclick: () => add() })]);
    const read = () => rows.map(r => { const o = {}; (f.columns || []).forEach((c, i) => { o[c.key] = r.cells[i].read(); }); return o; });
    return { el, read, lab };
  }

  // A question's conditions (internal/tasks/rules.go): the same tests the
  // server makes on what is sent, made here as people type.
  const textsOf = v => v == null ? [] : Array.isArray(v) ? v.map(x => latin(x).trim())
    : [typeof v === 'boolean' ? (v ? '1' : '0') : latin(v).trim()];
  const numOf = s => { const t = latin(s).replace(/[,٬\s_]/g, ''); return t === '' || isNaN(Number(t)) ? null : Number(t); };
  const sameText = (a, b) => {
    b = latin(b).trim();
    if (a.toLowerCase() === b.toLowerCase()) return true;
    const x = numOf(a), y = numOf(b);
    return x != null && y != null && x === y;
  };
  const isBlank = v => v == null || (typeof v === 'string' && !v.trim()) || (Array.isArray(v) && !v.length);
  function holds(rules, vals, any) {
    const test = r => {
      const v = vals[r.field], want = r.value || [];
      if (r.op === 'set') return !isBlank(v);
      if (r.op === 'unset') return isBlank(v);
      const have = textsOf(v), match = w => have.some(x => sameText(x, w));
      switch (r.op) {
        case 'eq': return match(want[0]);
        case 'ne': return !match(want[0]);
        case 'in': return want.some(match);
        case 'nin': return !want.some(match);
        case 'gt': case 'lt': {
          const n = numOf(have[0] == null ? '' : have[0]), w = numOf(want[0]);
          return n != null && w != null && (r.op === 'gt' ? n > w : n < w);
        }
      }
      return false;
    };
    rules = rules || [];
    return any && rules.length ? rules.some(test) : rules.every(test);
  }
  // A calculated question's value over the answers (internal/tasks/calc.go).
  function calcOf(f, vals) {
    const E = window.DocfindExpr;
    if (!E) return null;
    const e = E.parse(E.formula(f.formula));
    if (e.error) return null;
    const n = E.number(e.value(vals));
    if (n == null) return null;
    const p = 10 ** Math.max(0, Math.min(6, f.decimals || 0));
    return Math.round(n * p) / p;
  }

  // where: { board, form } or { board, node } for a form being filled, or
  // { test: true } for the form maker's preview, whose queries are drafts.
  function form(fields, data, where) {
    const parts = (fields || []).filter(f => f.mode !== 'hidden').map(f => {
      if (f.mode === 'view') {
        const v = (data || {})[f.key];
        return { el: h('div', { class: 'tm-field' + (f.kind === 'grid' ? ' tf-wide' : '') }, [h('span', { class: 'label', text: f.label }),
          f.kind === 'grid' ? gridTable(f, v) : f.kind === 'file' ? fileLinks(v) : h('div', { text: shown(f, v) })]), read: null, f };
      }
      let v = (data || {})[f.key];
      if ((v == null || v === '') && f.default && window.DocfindExpr) v = window.DocfindExpr.valueOf(f.default, data || {});
      return Object.assign(field(f, v), { f });
    });
    const el = h('div', { class: 'tf-form' }, parts.map(p => p.el));
    // Which questions are shown, settled as the server settles it: a hidden
    // question's answer is not sent, so it counts as empty for the others.
    let hidden = new Set();
    let locked = new Set();
    const values = () => {
      const out = Object.assign({}, data || {});
      parts.forEach(p => {
        if (hidden.has(p.f.key)) delete out[p.f.key];
        else if (p.read && !locked.has(p.f.key)) out[p.f.key] = p.read();
        else if (p.calc != null) out[p.f.key] = p.calc;
      });
      return out;
    };
    // The calculated ones, in order, each seeing those before it.
    const work = () => parts.forEach(p => {
      if (p.f.kind !== 'calc' || p.f.mode) return;
      p.calc = hidden.has(p.f.key) ? null : calcOf(p.f, values());
      if (p.set) p.set(p.calc);
    });
    const apply = () => {
      for (let pass = 0; pass < 4; pass++) {
        work();
        const vals = values();
        const next = new Set(parts.filter(p => !holds(p.f.show_if, vals, p.f.show_any)).map(p => p.f.key));
        const same = next.size === hidden.size && [...next].every(k => hidden.has(k));
        hidden = next;
        if (same) break;
      }
      work();
      const vals = values();
      locked = new Set(parts.filter(p => p.read && (p.f.readonly_if || []).length && holds(p.f.readonly_if, vals, p.f.readonly_any)).map(p => p.f.key));
      parts.forEach(p => {
        p.el.hidden = hidden.has(p.f.key);
        const ro = locked.has(p.f.key);
        p.el.querySelectorAll('input, select, textarea, button').forEach(x => { x.disabled = ro; });
        if (p.lab) p.lab.textContent = p.f.label + (!ro && (p.f.required || ((p.f.required_if || []).length && holds(p.f.required_if, vals, p.f.required_any))) ? ' *' : '');
      });
    };
    if (parts.some(p => (p.f.show_if || []).length || (p.f.required_if || []).length || (p.f.readonly_if || []).length || p.f.kind === 'calc')) {
      el.addEventListener('input', apply);
      el.addEventListener('change', apply);
      apply();
    }
    // Questions whose options or value come from docfind's lists or a
    // database: asked for when the form opens, again when an answer they name
    // changes, and -- a list to search -- as people type in its box.
    const sourced = parts.filter(p => p.load && isSourced(p.f) && !p.f.mode);
    // What a question's query names: the server's list, or -- a draft in the
    // form maker's preview -- read off its SQL here.
    const depsOf = f => f.deps || [...new Set((String(f.sql || '').match(/@@[A-Za-z_][A-Za-z0-9_]{0,63}/g) || []).map(x => x.slice(2)))];
    const sigOf = p => JSON.stringify(depsOf(p.f).map(k => values()[k]));
    const ask = async (p, q) => {
      const vals = values();
      let res;
      try {
        if (where && where.test) {
          res = await api('/api/tasks/fielddata/test', { method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ source: p.f.source, sql: p.f.sql || '', kind: p.f.kind, values: vals, q: q || '' }) });
          if (!res.ok) throw new Error(res.error);
          res = { choices: res.choices || (res.rows || []).map(r => ({ value: r[0], label: r[1] || r[0] })) };
        } else if (where && where.board) {
          res = await api('/api/tasks/fielddata', { method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ board: where.board, form: where.form || '', node: where.node || '', field: p.f.key, values: vals, q: q || '' }) });
        } else return;
        p.load(res.choices || []);
        p.el.classList.remove('tf-failed');
      } catch (e) {
        p.el.classList.add('tf-failed');
        if (p.lab) p.lab.title = e.message;
      }
      apply();
    };
    sourced.forEach(p => {
      p.sig = sigOf(p);
      ask(p);
      if (p.search) {
        let t = 0;
        p.search.addEventListener('input', () => { clearTimeout(t); t = setTimeout(() => ask(p, p.search.value.trim()), 300); });
      }
    });
    if (sourced.some(p => depsOf(p.f).length)) {
      let t = 0;
      const again = () => {
        clearTimeout(t);
        t = setTimeout(() => sourced.forEach(p => {
          if (!depsOf(p.f).length) return;
          const s = sigOf(p);
          if (s !== p.sig) { p.sig = s; ask(p); }
        }), 300);
      };
      el.addEventListener('input', again);
      el.addEventListener('change', again);
    }
    // Hidden and read-only questions are not sent; the server keeps the case's own.
    const read = () => { const out = {}; parts.forEach(p => { if (p.read && !hidden.has(p.f.key) && !locked.has(p.f.key)) out[p.f.key] = p.read(); }); return out; };
    return { el, read };
  }

  // ------------------------------------------------------------- dialog
  let dlg;
  function dialog() {
    if (!dlg) {
      dlg = h('dialog', { class: 'tm-dialog tf-dialog', 'aria-label': 'پرونده' });
      document.body.append(dlg);
    }
    dlg.textContent = '';
    return dlg;
  }
  const close = () => { if (dlg && dlg.open) dlg.close(); };
  function shell(title, sub) {
    const d = dialog();
    const body = h('div', { class: 'tm-dialog-body' });
    const foot = h('div', { class: 'tm-dialog-foot' });
    const msg = h('div', { class: 'err', role: 'alert', hidden: true });
    d.append(h('div', { class: 'tf-head' }, [h('h3', { text: title }), sub ? h('p', { class: 'hint', text: sub }) : null,
      h('button', { type: 'button', class: 'btn btn-ghost btn-sm tf-close', text: 'بستن', onclick: close })]), msg, body, foot);
    if (!d.open) d.showModal();
    // The server names the field it wants («its label»): take the reader there.
    const fail = t => {
      msg.textContent = t; msg.hidden = false;
      const m = /«(.+?)»/.exec(t);
      const lab = m && [...body.querySelectorAll('label.label')].find(l => l.textContent.replace(/ \*$/, '') === m[1]);
      const ctl = lab && document.getElementById(lab.htmlFor);
      if (ctl && ctl.focus) { ctl.setAttribute('aria-invalid', 'true'); ctl.scrollIntoView({ block: 'center' }); ctl.focus(); }
      else msg.scrollIntoView({ block: 'nearest' });
    };
    return { body, foot, fail };
  }

  // ------------------------------------------------------------- a case
  async function openCase(id) {
    let c;
    try { c = await api('/api/tasks/flow/cases/' + encodeURIComponent(id)); } catch (e) { const s = shell('پرونده'); s.fail(e.message); return; }
    drawCase(c);
  }
  function drawCase(c) {
    people = c.people || people;
    const s = shell(c.title, c.process + ' · ' + (c.done ? 'پایان یافته' : 'در جریان') + ' · آغاز به دست ' + nameOf(c.requester) + ' · ' + when(c.created));
    // The steps open now, and mine with its form.
    for (const o of c.open) {
      const who = o.assignees.length ? o.assignees.map(nameOf).join('، ') : 'یکی از: ' + (o.pool || []).map(nameOf).join('، ');
      const card = h('section', { class: 'card tf-step' + (o.mine ? ' mine' : '') }, [
        h('h4', { text: o.step }), h('p', { class: 'hint', text: who + ' · از ' + when(o.since) })]);
      if (o.claimable) {
        card.append(h('button', { type: 'button', class: 'btn btn-primary', text: 'برداشتن این کار', onclick: async ev => {
          ev.target.disabled = true;
          try { drawCase(await send('/api/tasks/flow/cases/' + encodeURIComponent(c.id) + '/claim/' + encodeURIComponent(o.id))); }
          catch (e) { ev.target.disabled = false; s.fail(e.message); }
        } }));
      }
      // My step's form in its card, its send in the foot, where it stays in view.
      if (o.mine) {
        const f = form(o.fields, c.data, { board: c.board_id, node: o.node });
        card.append(f.el);
        const several = c.open.filter(x => x.mine).length > 1;
        s.foot.append(h('button', { type: 'button', class: 'btn btn-primary', text: several ? 'انجام شد: ' + o.step : 'انجام شد و ارسال', onclick: async ev => {
          ev.target.disabled = true;
          try { const next = await send('/api/tasks/flow/cases/' + encodeURIComponent(c.id) + '/steps/' + encodeURIComponent(o.id), { answers: f.read() }); drawCase(next); window.dispatchEvent(new Event('taskflow:changed')); }
          catch (e) { ev.target.disabled = false; s.fail(e.message); }
        } }));
      }
      s.body.append(card);
    }
    if (!c.open.length) s.body.append(h('p', { class: 'hint', text: c.done ? 'این پرونده به پایان رسیده است.' : 'گامی باز نیست.' }));
    // What the case holds, with every field's label, in the map's order; open
    // unless a form of mine is above it.
    const keys = (c.order || Object.keys(c.data || {})).filter(k => c.fields[k] && c.data[k] !== '' && c.data[k] != null);
    const asked = c.open.some(o => o.mine && (o.fields || []).some(f => f.mode !== 'view' && f.mode !== 'hidden' && f.kind !== 'heading'));
    if (keys.length) {
      const dl = h('dl', { class: 'tf-data' }, keys.map(k => {
        const f = c.fields[k];
        return h('div', { class: f.kind === 'grid' || f.kind === 'textarea' ? 'tf-wide' : null }, [h('dt', { text: f.label }),
          h('dd', {}, f.kind === 'grid' ? gridTable(f, c.data[k]) : f.kind === 'file' ? fileLinks(c.data[k]) : shown(f, c.data[k]))]);
      }));
      s.body.append(h('details', { class: 'tf-section', open: !asked }, [h('summary', { text: 'اطلاعات پرونده' }), dl]));
    }
    // Who did what, and when.
    s.body.append(h('details', { class: 'tf-section' }, [h('summary', { text: 'تاریخچه (' + fa(c.steps.length) + ' گام)' }),
      h('ol', { class: 'tf-history' }, c.steps.map(r => h('li', {}, [h('b', { text: r.name }), ': ' + nameOf(r.by) + '، ' + when(r.at)])))]));
  }

  // ------------------------------------------------------------- start
  async function startCase(boardId) {
    let d;
    try { d = await api('/api/tasks/flow/boards/' + encodeURIComponent(boardId) + '/start'); } catch (e) { const s = shell('آغاز فرآیند'); s.fail(e.message); return; }
    people = d.people || people;
    const s = shell(d.process, 'گام نخست: ' + d.step);
    const f = form(d.fields, {}, { board: boardId, node: d.node });
    s.body.append(f.el);
    s.foot.append(h('button', { type: 'button', class: 'btn btn-primary', text: 'آغاز پرونده', onclick: async ev => {
      ev.target.disabled = true;
      try { const r = await send('/api/tasks/flow/boards/' + encodeURIComponent(boardId) + '/cases', { answers: f.read() }); window.dispatchEvent(new Event('taskflow:changed')); openCase(r.id); }
      catch (e) { ev.target.disabled = false; s.fail(e.message); }
    } }));
  }

  // ------------------------------------------------------------- inbox
  function draw(v, d) {
    v.textContent = '';
    const inbox = d.inbox || [];
    v.append(h('h3', { class: 'tm-sub', text: 'گام‌هایی که منتظر شماست' }));
    if (!inbox.length) v.append(h('p', { class: 'hint', text: 'کاری از فرآیندها منتظر شما نیست.' }));
    else v.append(h('ul', { class: 'tm-list tf-inbox' }, inbox.map(e => h('li', {}, h('button', { type: 'button', class: 'linkbtn', onclick: () => openCase(e.item_id) }, [
      h('b', { text: e.step }), h('span', { text: e.title }),
      h('span', { class: 'hint', text: e.board + ' · از ' + when(e.since) + (e.pool ? ' · برای برداشتن' : '') })])))));
    const start = d.start || [];
    if (start.length) {
      v.append(h('h3', { class: 'tm-sub', text: 'آغاز فرآیند تازه' }));
      v.append(h('div', { class: 'tf-start' }, start.map(p => h('button', { type: 'button', class: 'btn', text: p.name, onclick: () => startCase(p.board_id) }))));
    }
  }

  // form is also the board's intake forms' (tasks.js): the same controls for
  // every kind, with the page's own list of colleagues for a person field.
  window.TaskFlow = { draw, openCase, startCase, form: (fields, data, who, where) => { if (who) people = who; return form(fields, data, where); } };
  // A link straight to a case or a start: /tasks?case=<id>, ?flow=start&process=<board>
  // (the New menu's) -- not ?board=, which would open the board itself.
  const q = new URLSearchParams(location.search);
  if (q.get('case')) openCase(q.get('case'));
  else if (q.get('flow') === 'start' && (q.get('process') || q.get('board'))) startCase(q.get('process') || q.get('board'));
})();
