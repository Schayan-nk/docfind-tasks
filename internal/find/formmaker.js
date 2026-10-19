// The form maker (5.56.x): one editor for a board's intake forms and a
// process step's questions, beside a live preview drawn by the same renderer
// people fill in (taskflow.js). Opened over whatever asked for it:
//
//   const ok = await window.FormMaker.open(fields, { title, people, modes, locked });
//
// fields is changed in place only when «تأیید» is pressed. modes: show each
// question's "filled / shown / hidden" (a process step). locked(q): its key
// may not change -- the routes between steps and the saved answers use it.
// The server checks everything again (internal/tasks/answers.go); what is
// checked here is what it would otherwise fix silently: a key it does not
// accept is renamed on save, which would break whatever refers to it.
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
  const KINDS = [['text', 'متن کوتاه'], ['textarea', 'متن بلند'], ['number', 'عدد'], ['money', 'مبلغ (ریال)'], ['date', 'تاریخ'],
    ['choice', 'انتخابی'], ['multi', 'چندانتخابی'], ['check', 'تیک'], ['person', 'یکی از همکاران'], ['grid', 'جدول'], ['calc', 'محاسبه'], ['file', 'پیوست (فایل)'], ['heading', 'عنوان بخش']];
  const COLUMN_KINDS = ['text', 'number', 'money', 'date', 'choice', 'check', 'person'];
  const MODES = [['', 'پر می‌شود'], ['view', 'فقط نمایش'], ['hidden', 'پنهان']];
  const OPS = [['eq', 'برابر است با'], ['ne', 'برابر نیست با'], ['in', 'یکی از اینهاست'], ['nin', 'هیچ‌کدام از اینها نیست'],
    ['set', 'پر شده است'], ['unset', 'خالی است'], ['gt', 'بیشتر است از'], ['lt', 'کمتر است از']];
  const noValue = op => op === 'set' || op === 'unset';
  const KEY = /^[A-Za-z_][A-Za-z0-9_]{0,63}$/;
  const kindName = k => (KINDS.find(x => x[0] === k) || [k, k])[1];
  const fa = s => String(s).replace(/\d/g, c => '۰۱۲۳۴۵۶۷۸۹'[c]);
  const latin = s => String(s).replace(/[۰-۹]/g, d => d.charCodeAt(0) - 0x06F0).replace(/[٠-٩]/g, d => d.charCodeAt(0) - 0x0660);
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
  const iconBtn = (name, label, onclick, disabled) => h('button', { type: 'button', class: 'btn btn-sm btn-ghost fm-ic', title: label, 'aria-label': label, disabled, onclick }, icon(name));
  // A formula's problem, or '' (expr.js reads it as the server does).
  const formulaError = src => {
    const E = window.DocfindExpr;
    if (!E || !String(src || '').trim()) return '';
    const e = E.parse(E.formula(src));
    return e.error ? 'فرمول خوانده نشد: ' + e.error : '';
  };
  const patternError = p => { try { if (p) new RegExp('^(?:' + p + ')$', 'u'); return ''; } catch (_) { return 'الگو خوانده نشد.'; } };

  // A key not taken yet: q1, q2, ... (c1 ... for a table's columns).
  function freshKey(list, prefix) {
    const taken = new Set(list.map(q => String(q.key || '').toLowerCase()));
    for (let i = list.length + 1; ; i++) if (!taken.has(prefix + i)) return prefix + i;
  }

  // Options one per line. An existing option keeps its value -- routes test
  // the values (ok == "1"): found by its label, or, renamed, by its place
  // when its old label is gone; only a new line gets a new value, so taking
  // B out of A, B, C leaves C with the value it had.
  function relabel(old, text) {
    const labels = String(text).split('\n').map(x => x.trim()).filter(Boolean);
    const byLabel = new Map(old.map(c => [c.label, c.value]));
    const used = new Set();
    let next = old.reduce((m, c) => Math.max(m, Number(c.value) || 0), 0);
    return labels.map((l, i) => {
      let v = byLabel.get(l);
      if (v == null || used.has(v)) {
        const o = old[i];
        v = o && !used.has(o.value) && !labels.includes(o.label) ? o.value : null;
        while (v == null || used.has(v)) v = String(++next);
      }
      used.add(v);
      return { value: v, label: l };
    });
  }
  const choicesOf = q => (q.choices && q.choices.length ? q.choices : (q.options || []).map(o => ({ value: o, label: o })));

  // What the server would refuse or quietly change, said before saving.
  function problems(list, where, known) {
    const out = [], seen = new Map();
    const keys = new Set(list.concat(known || []).map(q => q.key));
    list.forEach((q, i) => {
      const name = (where ? where + ' / ' : '') + (q.label || 'پرسش ' + fa(i + 1));
      if (!String(q.label || '').trim()) out.push(name + ': بی‌عنوان است و ذخیره نمی‌شود.');
      if (q.kind !== 'heading' || q.key) {
        if (!KEY.test(q.key || '')) out.push(name + ': شناسه باید با حرف لاتین شروع شود و فقط حرف لاتین، رقم یا _ داشته باشد.');
        const k = String(q.key || '').toLowerCase();
        if (k && seen.has(k)) out.push(name + ': شناسهٔ «' + q.key + '» تکراری است.');
        seen.set(k, true);
      }
      if ((q.kind === 'choice' || q.kind === 'multi') && !q.source && !choicesOf(q).length) out.push(name + ': گزینه ندارد.');
      if (String(q.source || '').startsWith('db:') && !String(q.sql || '').trim()) out.push(name + ': پرس‌وجوی پایگاه داده ندارد.');
      if (q.kind === 'calc' && !String(q.formula || '').trim()) out.push(name + ': فرمول ندارد.');
      else if (q.kind === 'calc' && formulaError(q.formula)) out.push(name + ': ' + formulaError(q.formula));
      if (String(q.default || '').trim().startsWith('=') && formulaError(q.default)) out.push(name + '، پیش‌فرض: ' + formulaError(q.default));
      if (q.pattern && patternError(q.pattern)) out.push(name + ': ' + patternError(q.pattern));
      if (q.min != null && q.max != null && Number(q.min) > Number(q.max)) out.push(name + ': کمینه از بیشینه بزرگ‌تر است.');
      for (const r of (q.show_if || []).concat(q.required_if || [], q.readonly_if || [])) {
        if (!keys.has(r.field) || r.field === q.key) out.push(name + ': شرطی به پرسشی بسته است که در این فرم نیست.');
        else if (!noValue(r.op) && !(r.value || []).filter(v => String(v).trim()).length) out.push(name + ': شرطی مقدار ندارد.');
      }
      if (q.kind === 'grid') {
        if (!(q.columns || []).length) out.push(name + ': جدول ستون ندارد.');
        out.push(...problems(q.columns || [], q.label || 'جدول'));
      }
    });
    return out;
  }

  // The questions a condition may test: the others on this form, then -- a
  // process step -- whatever earlier steps asked (opts.known).
  function others(list, q, opts) {
    const seen = new Set([q.key]), out = [];
    for (const x of list.concat(opts.known || [])) {
      if (!x.key || seen.has(x.key) || x.kind === 'heading' || x.kind === 'grid') continue;
      seen.add(x.key);
      out.push(x);
    }
    return out;
  }

  // One list of conditions: «show only when» or «required only when», all of
  // which must hold (internal/tasks/rules.go).
  function ruleEditor(q, prop, title, list, opts, paint, anyProp) {
    return rulesBox(q, prop, title, others(list, q, opts), paint, anyProp);
  }
  // The same over any questions (pool): a process's routes use it too
  // (processedit.js). paint redraws; paint.soon is for typing.
  // anyProp, when given, is where "any one of them" is kept (show_any...):
  // with two rules or more the list says whether all or any must hold.
  function rulesBox(q, prop, title, pool, paint, anyProp) {
    const rules = q[prop] || [];
    const head = h('div', { class: 'fm-rule' }, [h('span', { class: 'hint', text: title })]);
    if (anyProp && rules.length > 1) {
      head.append(h('select', { class: 'select', 'aria-label': 'همه یا یکی', onchange: e => { if (e.target.value === '1') q[anyProp] = true; else delete q[anyProp]; paint.soon(); } },
        [['', 'همهٔ این‌ها درست باشد'], ['1', 'یکی از این‌ها درست باشد']].map(([v, n]) => h('option', { value: v, text: n, selected: !!q[anyProp] === !!v }))));
    } else if (anyProp) delete q[anyProp];
    const box = h('div', { class: 'fm-rules fm-wide' }, [head]);
    rules.forEach((r, i) => {
      const ref = pool.find(x => x.key === r.field);
      const cs = ref ? choicesOf(ref) : [];
      const many = r.op === 'in' || r.op === 'nin';
      let val = null;
      if (!noValue(r.op)) {
        if (ref && ref.kind === 'check') {
          val = h('select', { class: 'select', 'aria-label': 'مقدار', onchange: e => { r.value = [e.target.value]; paint.soon(); } },
            [['1', 'تیک خورده'], ['0', 'تیک نخورده']].map(([v, n]) => h('option', { value: v, text: n, selected: (r.value || [])[0] === v })));
          if (!(r.value || []).length) r.value = ['1'];
        } else if (cs.length && !many) {
          val = h('select', { class: 'select', 'aria-label': 'مقدار', onchange: e => { r.value = [e.target.value]; paint.soon(); } },
            [h('option', { value: '', text: '—' })].concat(cs.map(c => h('option', { value: c.value, text: c.label, selected: (r.value || [])[0] === c.value }))));
        } else if (cs.length) {
          val = h('span', { class: 'fm-checks' }, cs.map(c => h('label', {}, [h('input', { type: 'checkbox', checked: (r.value || []).includes(c.value), onchange: e => {
            const set = new Set(r.value || []);
            if (e.target.checked) set.add(c.value); else set.delete(c.value);
            r.value = [...set]; paint.soon();
          } }), ' ' + c.label])));
        } else {
          val = h('input', { class: 'input', style: 'width:11em', placeholder: many ? 'با ویرگول جدا کنید' : 'مقدار', value: (r.value || []).join('، '),
            'aria-label': 'مقدار', oninput: e => { r.value = many ? e.target.value.split(/[,،]/).map(x => x.trim()).filter(Boolean) : [e.target.value.trim()]; paint.soon(); } });
        }
      }
      box.append(h('div', { class: 'fm-rule' }, [
        h('select', { class: 'select', 'aria-label': 'پرسش', onchange: e => { r.field = e.target.value; r.value = []; paint(); } },
          [h('option', { value: '', text: 'پرسش…' })].concat(pool.map(x => h('option', { value: x.key, text: x.label || x.key, selected: x.key === r.field })))),
        h('select', { class: 'select', 'aria-label': 'شرط', onchange: e => { r.op = e.target.value; if (noValue(r.op)) r.value = []; paint(); } },
          OPS.map(([v, n]) => h('option', { value: v, text: n, selected: v === r.op }))),
        val,
        h('button', { type: 'button', class: 'btn btn-sm btn-ghost', text: '×', 'aria-label': 'حذف شرط', onclick: () => {
          rules.splice(i, 1);
          if (rules.length) q[prop] = rules; else delete q[prop];
          paint();
        } })]));
    });
    box.append(h('button', { type: 'button', class: 'btn btn-sm btn-ghost', text: '+ شرط', disabled: !pool.length, onclick: () => {
      rules.push({ field: pool[0].key, op: 'eq', value: [] });
      q[prop] = rules;
      paint();
    } }));
    return box;
  }

  // Where a question's options (or value) come from: a fixed list, docfind's
  // lists, or -- written by an administrator -- a database query
  // (internal/tasks/fielddata.go). The sources are asked for once.
  const SOURCED = ['choice', 'multi', 'text', 'textarea', 'number', 'money'];
  let sourcesP = null;
  const sources = () => sourcesP || (sourcesP = fetch('/api/tasks/fielddata/sources', { credentials: 'same-origin' })
    .then(r => (r.ok ? r.json() : { lists: [], databases: [], may_sql: false })).catch(() => ({ lists: [], databases: [], may_sql: false })));
  function dataBox(q, list, opts, paint) {
    const choice = q.kind === 'choice' || q.kind === 'multi';
    const pick = h('select', { class: 'select', 'aria-label': 'منبع', onchange: e => {
      const v = e.target.value;
      if (v) q.source = v; else { delete q.source; delete q.sql; }
      if (!String(q.source || '').startsWith('db:')) delete q.sql;
      paint();
    } }, [h('option', { value: '', text: choice ? 'گزینه‌های ثابت' : 'پر کردن به دست کاربر' })]);
    const box = h('div', { class: 'fm-data fm-wide' }, [h('div', { class: 'fm-rule' }, [h('span', { class: 'hint', text: choice ? 'گزینه‌ها از:' : 'مقدار از:' }), pick])]);
    if (q.kind === 'choice') {
      box.firstChild.append(h('span', { class: 'hint', text: 'نمایش:' }),
        h('select', { class: 'select', 'aria-label': 'نمایش', onchange: e => { if (e.target.value) q.display = e.target.value; else delete q.display; paint.soon(); } },
          [['', 'فهرست کشویی'], ['radio', 'دکمه‌های رادیویی'], ['search', 'فهرست با جست‌وجو']].map(([v, n]) => h('option', { value: v, text: n, selected: (q.display || '') === v }))));
    }
    sources().then(s => {
      const add = (v, n) => pick.append(h('option', { value: v, text: n, selected: q.source === v }));
      if (choice) (s.lists || []).forEach(l => add(l.id, l.name));
      (s.databases || []).forEach(d => add('db:' + d.id, 'پایگاه داده: ' + d.name));
      if (q.source && ![...pick.options].some(o => o.value === q.source)) add(q.source, q.source.replace(/^db:/, 'پایگاه داده: '));
      if (String(q.source || '').startsWith('db:')) box.append(sqlBox(q, list, opts, paint, !!s.may_sql));
    });
    return box;
  }
  function sqlBox(q, list, opts, paint, may) {
    const out = h('div', { class: 'fm-sql-out', role: 'status' });
    const area = h('textarea', { class: 'input fm-formula', dir: 'ltr', rows: 4, value: q.sql || '', readOnly: !may, 'aria-label': 'پرس‌وجوی پایگاه داده',
      placeholder: 'SELECT id, name FROM customers WHERE city = @@city ORDER BY name', oninput: e => { q.sql = e.target.value; paint.soon(); } });
    const test = h('button', { type: 'button', class: 'btn btn-sm', text: 'آزمودن', disabled: !may, onclick: async () => {
      out.textContent = 'در حال اجرا…';
      try {
        const r = await fetch('/api/tasks/fielddata/test', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ source: q.source, sql: q.sql || '', values: {} }) }).then(x => x.json());
        out.textContent = '';
        if (!r.ok) { out.append(h('span', { class: 'err', text: r.error || 'اجرا نشد.' })); return; }
        out.append(h('span', { class: 'hint', text: fa((r.rows || []).length) + ' ردیف نخست' + ((r.deps || []).length ? ' · بسته به: ' + r.deps.join('، ') : '') }));
        if ((r.rows || []).length) out.append(h('div', { class: 'table-scroll' }, h('table', { class: 'data' }, h('tbody', {}, r.rows.slice(0, 8).map(row => h('tr', {}, row.map(c => h('td', { dir: 'auto', text: c }))))))));
      } catch (e) { out.textContent = e.message; }
    } });
    return h('div', { class: 'fm-sql' }, [area,
      may ? h('p', { class: 'hint' }, [(q.kind === 'choice' || q.kind === 'multi' ? 'ستون اول مقدار است و ستون دوم برچسب. ' : 'ستون اولِ ردیف اول پاسخ را پر می‌کند. '),
        h('code', { dir: 'ltr', text: '@@key' }), ' جای پاسخ پرسشِ key را می‌گیرد و با تغییر آن پاسخ دوباره خوانده می‌شود. فقط یک ', h('code', { dir: 'ltr', text: 'SELECT' }), '.'])
        : h('p', { class: 'hint', text: 'پرس‌وجوی پایگاه داده را فقط مدیر docfind می‌نویسد یا تغییر می‌دهد.' }),
      h('div', { class: 'fm-rule' }, [test]), out]);
  }

  // A calculated question's formula: the keys it may use, offered to put in
  // where the cursor is; whether it reads, said as it is typed.
  function calcBox(q, list, opts, paint) {
    const err = h('span', { class: 'err fm-formula-err', role: 'status', text: formulaError(q.formula) });
    const input = h('input', { class: 'input fm-formula', dir: 'ltr', value: q.formula || '', placeholder: 'qty * price', 'aria-label': 'فرمول',
      oninput: e => { q.formula = e.target.value; err.textContent = formulaError(q.formula); paint.soon(); } });
    const keys = others(list, q, opts).filter(x => x.kind !== 'heading' && x.kind !== 'grid');
    const put = k => {
      const at = input.selectionStart == null ? input.value.length : input.selectionStart;
      input.value = input.value.slice(0, at) + k + input.value.slice(input.selectionEnd == null ? at : input.selectionEnd);
      input.dispatchEvent(new Event('input'));
      input.focus();
      input.setSelectionRange(at + k.length, at + k.length);
    };
    return h('div', { class: 'fm-calc fm-wide' }, [
      h('div', { class: 'fm-rule' }, [h('span', { class: 'hint', text: 'فرمول' }), input,
        h('label', { class: 'fm-check' }, [h('span', { class: 'hint', text: 'رقم اعشار' }),
          h('select', { class: 'select', 'aria-label': 'رقم اعشار', onchange: e => { q.decimals = Number(e.target.value); paint.soon(); } },
            [0, 1, 2, 3, 4].map(n => h('option', { value: n, text: fa(n), selected: (q.decimals || 0) === n })))])]),
      err,
      keys.length ? h('div', { class: 'fm-keys' }, [h('span', { class: 'hint', text: 'پرسش‌ها:' })].concat(keys.map(x =>
        h('button', { type: 'button', class: 'btn btn-sm btn-ghost fm-key', title: x.label, onclick: () => put(x.key) }, [h('bdi', { text: x.key }), ' ' + (x.label || '')])))) : null,
      h('p', { class: 'hint', text: 'عملگرها: + − * / و پرانتز؛ تابع‌ها: round(عدد، رقم)، min، max، abs و if(شرط، آنگاه، وگرنه). نمونه:' }),
      h('code', { class: 'fm-example', dir: 'ltr', text: 'round(max(0, sales - costs) * rate / 100)' })]);
  }

  // One question's card: the common settings in a row, the rest below.
  function card(list, i, opts, paint, isColumn) {
    const q = list[i];
    const locked = !isColumn && opts.locked && opts.locked(q);
    const kinds = isColumn ? KINDS.filter(([k]) => COLUMN_KINDS.includes(k)) : KINDS;
    const move = d => { const j = i + d; if (j < 0 || j >= list.length) return; [list[i], list[j]] = [list[j], list[i]]; paint(); };
    const row = h('div', { class: 'fm-row' }, [
      h('input', { class: 'input fm-label', value: q.label || '', placeholder: q.kind === 'heading' ? 'عنوان بخش' : 'متن پرسش', 'aria-label': 'متن پرسش',
        oninput: e => { q.label = e.target.value; paint.soon(); } }),
      h('select', { class: 'select', 'aria-label': 'نوع', onchange: e => {
        q.kind = e.target.value;
        if ((q.kind === 'choice' || q.kind === 'multi') && !choicesOf(q).length) q.choices = [{ value: '1', label: 'گزینهٔ ۱' }];
        if (q.kind === 'grid' && !(q.columns || []).length) q.columns = [{ key: 'c1', label: 'ستون ۱', kind: 'text' }];
        paint();
      } }, kinds.map(([k, n]) => h('option', { value: k, text: n, selected: k === (q.kind || 'text') }))),
      q.kind === 'heading' || q.kind === 'calc' ? null : h('label', { class: 'fm-check' }, [h('input', { type: 'checkbox', checked: !!q.required, onchange: e => { q.required = e.target.checked; paint.soon(); } }), ' لازم']),
      (q.show_if || []).length ? h('span', { class: 'chip chip-sm', text: 'شرطی', title: 'فقط وقتی شرط‌هایش درست باشد نشان داده می‌شود' }) : null,
      (q.required_if || []).length ? h('span', { class: 'chip chip-sm', text: 'لازمِ شرطی', title: 'وقتی شرط‌هایش درست باشد لازم است' }) : null,
      (q.readonly_if || []).length ? h('span', { class: 'chip chip-sm', text: 'قفلِ شرطی', title: 'وقتی شرط‌هایش درست باشد اینجا تغییر نمی‌کند' }) : null,
      q.default ? h('span', { class: 'chip chip-sm', text: 'پیش‌فرض', title: 'وقتی خالی باز شود این مقدار را می‌گیرد' }) : null,
      opts.modes && !isColumn ? h('select', { class: 'select', 'aria-label': 'در این گام', onchange: e => { q.mode = e.target.value; paint.soon(); } },
        MODES.map(([k, n]) => h('option', { value: k, text: n, selected: k === (q.mode || '') }))) : null,
      h('span', { class: 'fm-acts' }, [
        iconBtn('arrow-up', 'بالا بردن', () => move(-1), i === 0),
        iconBtn('arrow-down', 'پایین بردن', () => move(1), i === list.length - 1),
        iconBtn('copy', 'نسخهٔ دوم', () => {
          const copy = JSON.parse(JSON.stringify(q, (k, v) => (k === '_old' ? undefined : v)));
          copy.key = freshKey(list, isColumn ? 'c' : 'q');
          copy.label = (copy.label || '') + ' (۲)';
          list.splice(i + 1, 0, copy); paint();
        }),
        iconBtn('trash-2', 'حذف پرسش', () => { list.splice(i, 1); paint(); }),
        h('button', { type: 'button', class: 'btn btn-sm btn-ghost', text: 'جزئیات', 'aria-expanded': String(opts.open.has(q)),
          onclick: () => { if (opts.open.has(q)) opts.open.delete(q); else opts.open.add(q); paint(); } }),
      ]),
    ]);
    const more = h('div', { class: 'fm-more', hidden: !opts.open.has(q) }, [
      h('label', { class: 'fm-field' }, [h('span', { class: 'hint', text: 'شناسه' + (locked ? ' (ثابت: مسیرها و پاسخ‌ها به آن بسته‌اند)' : '') }),
        h('input', { class: 'input', dir: 'ltr', value: q.key || '', disabled: locked, oninput: e => { q.key = e.target.value.trim(); paint.soon(); } })]),
      isColumn || q.kind === 'heading' ? null : h('label', { class: 'fm-field fm-wide' }, [h('span', { class: 'hint', text: 'راهنما زیر پرسش' }),
        h('input', { class: 'input', value: q.hint || '', oninput: e => { q.hint = e.target.value; paint.soon(); } })]),
      isColumn || !['text', 'textarea', 'number', 'money', 'date', 'choice', 'person'].includes(q.kind || 'text') ? null
        : h('label', { class: 'fm-field fm-wide' }, [h('span', { class: 'hint', text: 'پیش‌فرض: متن، یا با = فرمولی از پاسخ‌های پیشین' }),
          h('input', { class: 'input', value: q.default || '', placeholder: 'مثلاً تهران، یا = amount * 2', oninput: e => { q.default = e.target.value; if (!q.default.trim()) delete q.default; paint.soon(); } })]),
      q.kind === 'number' || q.kind === 'money' ? h('div', { class: 'fm-rule fm-wide' }, [
        h('label', { class: 'fm-field' }, [h('span', { class: 'hint', text: 'کمینه' }), h('input', { class: 'input', dir: 'ltr', inputmode: 'decimal', value: q.min == null ? '' : q.min,
          oninput: e => { const v = latin(e.target.value).trim(); if (v === '' || isNaN(Number(v))) delete q.min; else q.min = Number(v); paint.soon(); } })]),
        h('label', { class: 'fm-field' }, [h('span', { class: 'hint', text: 'بیشینه' }), h('input', { class: 'input', dir: 'ltr', inputmode: 'decimal', value: q.max == null ? '' : q.max,
          oninput: e => { const v = latin(e.target.value).trim(); if (v === '' || isNaN(Number(v))) delete q.max; else q.max = Number(v); paint.soon(); } })])]) : null,
      q.kind === 'text' || q.kind === 'textarea' ? h('div', { class: 'fm-rule fm-wide' }, [
        h('label', { class: 'fm-field' }, [h('span', { class: 'hint', text: 'الگو (عبارت منظم)' }), h('input', { class: 'input', dir: 'ltr', value: q.pattern || '', placeholder: '09\\d{9}',
          oninput: e => { q.pattern = e.target.value; if (!q.pattern.trim()) { delete q.pattern; delete q.pattern_msg; } paint.soon(); } })]),
        h('label', { class: 'fm-field fm-wide' }, [h('span', { class: 'hint', text: 'پیام وقتی جور نباشد' }), h('input', { class: 'input', value: q.pattern_msg || '', placeholder: 'شمارهٔ همراه ۱۱ رقمی است.',
          oninput: e => { q.pattern_msg = e.target.value; paint.soon(); } })])]) : null,
      isColumn ? null : ruleEditor(q, 'show_if', 'نمایش فقط وقتی:', list, opts, paint, 'show_any'),
      isColumn || q.kind === 'heading' || q.kind === 'calc' ? null : ruleEditor(q, 'required_if', 'لازم فقط وقتی:', list, opts, paint, 'required_any'),
      isColumn || q.kind === 'heading' || q.kind === 'calc' ? null : ruleEditor(q, 'readonly_if', 'اینجا قفل (فقط خواندنی) وقتی:', list, opts, paint, 'readonly_any'),
    ]);
    const options = (q.kind === 'choice' || q.kind === 'multi') && !q.source ? h('label', { class: 'fm-field fm-wide' }, [h('span', { class: 'hint', text: 'گزینه‌ها، هر کدام در یک سطر' }),
      h('textarea', { class: 'input', rows: Math.min(8, Math.max(3, choicesOf(q).length + 1)), value: choicesOf(q).map(c => c.label).join('\n'),
        oninput: e => { q.choices = relabel(choicesOf(q), e.target.value); q.options = undefined; paint.soon(); } })]) : null;
    const data = !isColumn && SOURCED.includes(q.kind) ? dataBox(q, list, opts, paint) : null;
    const box = h('div', { class: 'fm-card' + (q.kind === 'heading' ? ' fm-heading' : '') }, [row, data, options, q.kind === 'calc' && !isColumn ? calcBox(q, list, opts, paint) : null, more]);
    if (q.kind === 'grid' && !isColumn) {
      q.columns = q.columns || [];
      const cols = h('div', { class: 'fm-cols' }, [h('div', { class: 'hint', text: 'ستون‌های جدول' })]);
      q.columns.forEach((_, j) => cols.append(card(q.columns, j, opts, paint, true)));
      cols.append(h('button', { type: 'button', class: 'btn btn-sm', text: '+ ستون', onclick: () => {
        q.columns.push({ key: freshKey(q.columns, 'c'), label: '', kind: 'text' }); paint();
      } }));
      box.append(cols);
    }
    return box;
  }

  let dlg;
  function open(fields, opts) {
    opts = opts || {};
    return new Promise(done => {
      if (!dlg) {
        dlg = h('dialog', { class: 'tm-dialog fm-dialog', 'aria-label': 'سازندهٔ فرم' });
        document.body.append(dlg);
      }
      dlg.textContent = '';
      // Edited on a copy; the caller's list changes only on «تأیید».
      const work = JSON.parse(JSON.stringify(fields || []));
      // Which cards show their key and hint: the page's, never saved.
      opts = Object.assign({}, opts, { open: new WeakSet() });
      (fields || []).forEach((f, i) => { if (f._old) work[i]._old = true; });
      const list = h('div', { class: 'fm-list' });
      const preview = h('div', { class: 'fm-preview' });
      const issues = h('ul', { class: 'fm-issues', role: 'status' });
      let timer = 0;
      const drawPreview = () => {
        preview.textContent = '';
        preview.append(h('h3', { text: 'پیش‌نمایش' }));
        const shown = work.filter(q => q.mode !== 'hidden' && String(q.label || '').trim());
        if (!shown.length) { preview.append(h('p', { class: 'hint', text: 'هنوز پرسشی نیست.' })); }
        else if (window.TaskFlow) preview.append(window.TaskFlow.form(JSON.parse(JSON.stringify(shown)), {}, opts.people, { test: true }).el);
        const ps = problems(work, null, opts.known);
        issues.textContent = '';
        ps.forEach(p => issues.append(h('li', { text: p })));
        issues.hidden = !ps.length;
      };
      const paint = () => {
        list.textContent = '';
        work.forEach((_, i) => list.append(card(work, i, opts, paint, false)));
        list.append(h('div', { class: 'fm-add' }, [
          h('button', { type: 'button', class: 'btn btn-sm', text: '+ پرسش', onclick: () => { work.push({ key: freshKey(work, 'q'), label: '', kind: 'text' }); paint(); } }),
          h('button', { type: 'button', class: 'btn btn-sm btn-ghost', text: '+ عنوان بخش', onclick: () => { work.push({ key: freshKey(work, 'h'), label: '', kind: 'heading' }); paint(); } })]));
        drawPreview();
      };
      paint.soon = () => { clearTimeout(timer); timer = setTimeout(drawPreview, 250); };
      const finish = ok => {
        if (ok) {
          const ps = problems(work, null, opts.known);
          if (ps.length) { issues.scrollIntoView({ block: 'nearest' }); return; }
          fields.splice(0, fields.length, ...work);
        }
        dlg.close();
        done(ok);
      };
      dlg.append(
        h('div', { class: 'fm-head' }, [h('h2', { text: opts.title || 'سازندهٔ فرم' }),
          h('span', { class: 'hint', text: fa(work.length) + ' پرسش' })]),
        issues,
        h('div', { class: 'fm-body' }, [list, preview]),
        h('div', { class: 'tm-dialog-foot' }, [
          h('button', { type: 'button', class: 'btn btn-primary', text: 'تأیید', onclick: () => finish(true) }),
          h('span', { class: 'spacer' }),
          h('button', { type: 'button', class: 'btn btn-ghost', text: 'انصراف', onclick: () => finish(false) })]));
      dlg.oncancel = e => { e.preventDefault(); finish(false); };
      paint();
      dlg.showModal();
      const first = list.querySelector('input');
      if (first) first.focus();
    });
  }

  // Conditions in words, for a route's label: «نوع = کالا و مبلغ > ۵».
  function describe(rules, pool, any) {
    return (rules || []).map(r => {
      const ref = (pool || []).find(x => x.key === r.field);
      const name = ref ? (ref.label || r.field) : r.field;
      const label = v => { const c = ref && choicesOf(ref).find(c => c.value === v); return c ? c.label : (ref && ref.kind === 'check' ? (v === '1' ? 'تیک خورده' : 'تیک نخورده') : fa(v)); };
      const vs = (r.value || []).map(label).join('، ');
      return { eq: name + ' = ' + vs, ne: name + ' ≠ ' + vs, in: name + ': ' + vs, nin: name + ' نه: ' + vs, set: name + ' پر شده', unset: name + ' خالی',
        gt: name + ' > ' + vs, lt: name + ' < ' + vs }[r.op] || name;
    }).join(any ? ' یا ' : ' و ');
  }

  window.FormMaker = { open, problems, kindName, rules: rulesBox, describe, choicesOf };
})();
