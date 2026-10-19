// expr.js: docfind's condition and formula language on the page --
// the same one internal/tasks/cond.go reads on the server, so a calculated
// question shows as people type what the server will keep. A case variable
// (@@name or the bare name), numbers and quoted strings, == != < > <= >=,
// && || ! (and/or/not), + - * / %, brackets, and in_array, empty, round,
// floor, ceil, abs, min, max, if. Comparison is loose ("1" == 1); in
// arithmetic a value that is not a number is 0, and dividing by 0 gives 0.
//
//   const e = DocfindExpr.parse('qty * price');  // { error } when it does not read
//   e.value({ qty: 3, price: '1,250' })            // 3750
//   DocfindExpr.vars('qty * price')                // ['qty', 'price']
(function () {
  'use strict';
  const latin = s => String(s).replace(/[۰-۹]/g, d => d.charCodeAt(0) - 0x06F0).replace(/[٠-٩]/g, d => d.charCodeAt(0) - 0x0660);
  const OPS = ['===', '!==', '==', '!=', '<>', '<=', '>=', '&&', '||', '<', '>', '!', '(', ')', ',', '+', '-', '*', '/', '%'];
  const FUNCS = { in_array: 2, empty: 1, round: -2, floor: 1, ceil: 1, abs: 1, min: -1, max: -1, if: 3 };

  function lex(src) {
    const t = [];
    for (let i = 0; i < src.length;) {
      const c = src[i];
      if (/\s/.test(c)) { i++; continue; }
      if (c === '@' && '@#=%?$&'.includes(src[i + 1] || '')) {
        let j = i + 2;
        while (j < src.length && /[\p{L}\p{N}_]/u.test(src[j])) j++;
        if (j === i + 2) throw new Error('a variable with no name');
        t.push({ k: 'id', s: src.slice(i + 2, j) }); i = j; continue;
      }
      if (/[\p{L}_]/u.test(c)) {
        let j = i;
        while (j < src.length && /[\p{L}\p{N}_]/u.test(src[j])) j++;
        const w = src.slice(i, j), lw = w.toLowerCase();
        t.push(lw === 'and' ? { k: 'op', s: '&&' } : lw === 'or' ? { k: 'op', s: '||' } : lw === 'not' ? { k: 'op', s: '!' }
          : lw === 'true' ? { k: 'num', s: '1' } : lw === 'false' ? { k: 'num', s: '0' } : { k: 'id', s: w });
        i = j; continue;
      }
      if (/\d/.test(c) || (c === '.' && /\d/.test(src[i + 1] || ''))) {
        let j = i;
        while (j < src.length && /[\d.]/.test(src[j])) j++;
        t.push({ k: 'num', s: src.slice(i, j) }); i = j; continue;
      }
      if (c === '"' || c === "'") {
        let j = i + 1, s = '';
        while (j < src.length && src[j] !== c) { if (src[j] === '\\' && j + 1 < src.length) j++; s += src[j]; j++; }
        if (j >= src.length) throw new Error('a string with no end');
        t.push({ k: 'str', s }); i = j + 1; continue;
      }
      const op = OPS.find(o => src.startsWith(o, i));
      if (!op) throw new Error('"' + c + '" is not understood');
      t.push({ k: 'op', s: op === '===' ? '==' : op === '!==' || op === '<>' ? '!=' : op });
      i += op.length;
    }
    return t;
  }

  function parse(src) {
    src = latin(String(src == null ? '' : src).trim());
    if (!src) return { value: () => true, vars: [] };
    let toks, i = 0;
    try { toks = lex(src); } catch (e) { return { error: e.message }; }
    const peek = () => (toks[i] && toks[i].k === 'op' ? toks[i].s : '');
    const vars = [];
    const bin = (next, ops, make) => () => {
      let l = next();
      while (ops.includes(peek())) { const op = toks[i++].s; const r = next(); l = make(op, l, r); }
      return l;
    };
    const primary = () => {
      const t = toks[i++];
      if (!t) throw new Error('the condition ends too soon');
      if (t.k === 'num') { const n = Number(t.s); if (isNaN(n)) throw new Error('"' + t.s + '" is not a number'); return () => n; }
      if (t.k === 'str') return () => t.s;
      if (t.k === 'id') {
        if (peek() === '(') {
          i++;
          const args = [];
          while (peek() !== ')') {
            args.push(or());
            if (peek() === ',') i++;
            else if (peek() !== ')') throw new Error(t.s + '(: a , or ) was expected');
            if (i >= toks.length) throw new Error('the condition ends too soon');
          }
          i++;
          const fn = t.s.toLowerCase(), want = FUNCS[fn];
          if (want == null) throw new Error(t.s + '() is not a function conditions may call');
          if ((want === -2 && (args.length < 1 || args.length > 2)) || (want === -1 && !args.length) || (want >= 0 && args.length !== want)) throw new Error(t.s + '(): the wrong number of values');
          return d => call(fn, args, d);
        }
        vars.push(t.s);
        return d => (Object.prototype.hasOwnProperty.call(d, t.s) ? d[t.s] : null);
      }
      if (t.s === '(') { const n = or(); if (peek() !== ')') throw new Error('a ( with no )'); i++; return n; }
      throw new Error('"' + t.s + '" is out of place');
    };
    const unary = () => {
      const op = peek();
      if (op === '!') { i++; const n = unary(); return d => !truthy(n(d)); }
      if (op === '-' || op === '+') { i++; const n = unary(); return op === '-' ? d => -num(n(d)) : d => num(n(d)); }
      return primary();
    };
    const product = bin(unary, ['*', '/', '%'], (op, l, r) => d => arith(op, num(l(d)), num(r(d))));
    const sum = bin(product, ['+', '-'], (op, l, r) => d => arith(op, num(l(d)), num(r(d))));
    const cmp = () => {
      const l = sum(), op = peek();
      if (!['==', '!=', '<', '>', '<=', '>='].includes(op)) return l;
      i++;
      const r = sum();
      return d => { const c = loose(l(d), r(d)); return op === '==' ? c === 0 : op === '!=' ? c !== 0 : c != null && ({ '<': c < 0, '>': c > 0, '<=': c <= 0, '>=': c >= 0 })[op]; };
    };
    const and = bin(cmp, ['&&'], (op, l, r) => d => truthy(l(d)) && truthy(r(d)));
    const or = bin(and, ['||'], (op, l, r) => d => truthy(l(d)) || truthy(r(d)));
    let root;
    try {
      root = or();
      if (i < toks.length) throw new Error('unexpected "' + toks[i].s + '"');
    } catch (e) { return { error: e.message }; }
    return { value: d => root(d || {}), vars };
  }

  function arith(op, a, b) {
    if (op === '+') return a + b;
    if (op === '-') return a - b;
    if (op === '*') return a * b;
    if (b === 0) return 0;
    return op === '/' ? a / b : a % b;
  }
  function call(fn, args, d) {
    const v = k => args[k](d);
    switch (fn) {
      case 'in_array': { const needle = v(0), hay = v(1); return (Array.isArray(hay) ? hay : hay == null ? [] : [hay]).some(x => loose(needle, x) === 0); }
      case 'empty': return !truthy(v(0));
      case 'round': { const p = args.length > 1 ? Math.max(0, Math.min(6, Math.round(num(v(1))))) : 0, f = 10 ** p; return Math.round(num(v(0)) * f) / f; }
      case 'floor': return Math.floor(num(v(0)));
      case 'ceil': return Math.ceil(num(v(0)));
      case 'abs': return Math.abs(num(v(0)));
      case 'min': case 'max': { const ns = args.map((_, k) => num(v(k))); return fn === 'min' ? Math.min(...ns) : Math.max(...ns); }
      case 'if': return truthy(v(0)) ? v(1) : v(2);
    }
    return false;
  }
  // A value as a number, or null when it is none (cond.go's number).
  function number(v) {
    if (typeof v === 'number') return isFinite(v) ? v : null;
    if (typeof v === 'boolean') return v ? 1 : 0;
    if (typeof v !== 'string') return null;
    const s = latin(v).replace(/[,٬]/g, '').trim();
    if (s === '' || isNaN(Number(s))) return null;
    return Number(s);
  }
  const num = v => { const n = number(v); return n == null ? 0 : n; };
  // PHP's ==: numbers as numbers, else as text; null (no answer) is "".
  function loose(a, b) {
    if (Array.isArray(a) || Array.isArray(b)) return null;
    a = a == null ? '' : a; b = b == null ? '' : b;
    const x = number(a), y = number(b);
    if (x != null && y != null) return x < y ? -1 : x > y ? 1 : 0;
    const p = String(a), q = String(b);
    return p < q ? -1 : p > q ? 1 : 0;
  }
  function truthy(v) {
    if (v == null) return false;
    if (typeof v === 'boolean') return v;
    if (typeof v === 'number') return v !== 0;
    if (typeof v === 'string') return v !== '' && v !== '0';
    if (Array.isArray(v)) return v.length > 0;
    return true;
  }

  // A formula's text without its leading "=" (calc.go's formulaOf).
  const formula = s => String(s || '').trim().replace(/^=/, '').trim();
  // What a "=formula" or plain text gives over the answers (calc.go's valueOf).
  function valueOf(src, data) {
    src = String(src || '').trim();
    if (!src.startsWith('=')) return src;
    const e = parse(formula(src));
    return e.error ? null : e.value(data);
  }

  window.DocfindExpr = { parse, formula, valueOf, number, vars: src => (parse(formula(src)).vars || []) };
})();
