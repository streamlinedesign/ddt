/* Riječililica: five letters, six tries. Lj, nj and dž are two letters.
   The answer sits in data-game, scrambled with the date of the edition, so the check needs no server.
   Every game played stays in this browser under tisak-rijecililica. */
(function () {
  const game = document.querySelector('[data-game]');
  if (!game) return;
  const box = game.closest('.game-wrap') || document;
  const store = {
    get: k => { try { return localStorage.getItem(k); } catch (e) { return null; } },
    set: (k, v) => { try { localStorage.setItem(k, v); } catch (e) {} },
  };
  const day = game.dataset.gameDay || '';
  const answer = (() => {
    try {
      let seed = 0;
      for (const c of day) if (c >= '0' && c <= '9') seed = (seed * 10 + +c) % 251;
      const raw = atob(game.dataset.game), b = new Uint8Array(raw.length);
      for (let i = 0; i < raw.length; i++) b[i] = raw.charCodeAt(i) ^ ((seed + i * 73 + 41) & 255);
      const w = Array.from(new TextDecoder().decode(b));
      return w.length === 5 ? w : null;
    } catch (e) { return null; }
  })();
  const LETTERS = 'abcčćdđefghijklmnoprsštuvzž';
  const rows = [...game.querySelectorAll('.row')], msg = game.querySelector('.game-msg');
  const statsEl = box.querySelector('[data-game-stats]'), shareBtn = box.querySelector('[data-game-share]');
  const keys = {}; game.querySelectorAll('[data-k]').forEach(k => keys[k.dataset.k] = k);
  let saved = {}; try { saved = JSON.parse(store.get('tisak-rijecililica') || '{}') || {}; } catch (e) {}
  saved.games = saved.games || {};
  const me = saved.games[day] || (saved.games[day] = { g: [], done: 0 });
  let typed = [], words = null, loading = null, active = false;
  const say = t => { msg.textContent = t; };
  const save = () => store.set('tisak-rijecililica', JSON.stringify(saved));
  const score = guess => {
    const res = Array(5).fill('no'), left = {};
    answer.forEach((c, i) => { if (guess[i] === c) res[i] = 'ok'; else left[c] = (left[c] || 0) + 1; });
    guess.forEach((c, i) => { if (res[i] !== 'ok' && left[c]) { res[i] = 'near'; left[c]--; } });
    return res;
  };
  const rank = { no: 1, near: 2, ok: 3 };
  const paint = () => {
    Object.values(keys).forEach(k => k.classList.remove('ok', 'near', 'no'));
    const best = {};
    rows.forEach((row, r) => {
      const g = me.g[r] ? Array.from(me.g[r]) : (r === me.g.length ? typed : []);
      const res = me.g[r] ? score(g) : null;
      row.querySelectorAll('.tile').forEach((t, i) => {
        t.textContent = g[i] || '';
        t.className = 'tile' + (g[i] ? ' filled' : '') + (res ? ' ' + res[i] : '');
        t.style.setProperty('--i', i);
        if (res && rank[res[i]] > (rank[best[g[i]]] || 0)) best[g[i]] = res[i];
      });
      row.setAttribute('aria-label', g.join('') + (res ? ': ' + res.map(x => ({ ok: 'točno', near: 'drugdje', no: 'nema' })[x]).join(', ') : ''));
    });
    Object.entries(best).forEach(([c, v]) => keys[c] && keys[c].classList.add(v));
  };
  const stats = () => {
    const done = Object.entries(saved.games).filter(([, x]) => x.done).sort(([a], [b]) => a < b ? -1 : 1);
    const won = done.filter(([, x]) => x.done === 1).length;
    let run = 0, best = 0, prev = null;
    for (const [d, x] of done) {
      const after = prev && new Date(Date.parse(prev) + 864e5).toISOString().slice(0, 10) === d;
      run = x.done === 1 ? (after ? run + 1 : 1) : 0;
      best = Math.max(best, run); prev = d;
    }
    return { played: done.length, pct: done.length ? Math.round(won * 100 / done.length) : 0, run, best };
  };
  const praise = ['Genijalno!', 'Sjajno!', 'Odlično!', 'Vrlo dobro!', 'Dobro!', 'Uf, u zadnji čas!'];
  const finish = () => {
    if (!me.done) return;
    say(me.done === 1 ? praise[me.g.length - 1] : 'Riječ je bila: ' + answer.join('').toUpperCase());
    const s = stats();
    if (statsEl) statsEl.textContent = 'Odigrano ' + s.played + ' · Pobjede ' + s.pct + ' % · Niz ' + s.run + ' · Najduži ' + s.best;
    if (shareBtn) shareBtn.hidden = false;
  };
  /* the guess list is printed into the page; data-game-list is only a fallback. If neither loads, any five letters count */
  const inline = box.querySelector('[data-game-words]');
  const list = () => loading || (loading = (inline ? Promise.resolve(inline.textContent)
    : fetch(game.dataset.gameList).then(r => r.ok ? r.text() : Promise.reject()))
    .then(t => { words = new Set(t.split(/\s+/).map(w => w.trim().toLowerCase()).filter(Boolean)); }).catch(() => { words = null; }));
  const shake = () => { const r = rows[me.g.length]; if (!r) return; r.classList.remove('shake'); void r.offsetWidth; r.classList.add('shake'); };
  const enter = async () => {
    if (typed.length < 5) { say('Premalo slova'); shake(); return; }
    await list();
    const w = typed.join('');
    if (words && words.size && !words.has(w) && w !== answer.join('')) { say('Te riječi nema na popisu'); shake(); return; }
    me.g.push(w); typed = [];
    if (w === answer.join('')) me.done = 1; else if (me.g.length === 6) me.done = 2;
    save(); paint(); say(''); finish();
  };
  const press = k => {
    if (!answer || me.done) return;
    if (k === 'enter') enter();
    else if (k === 'back') { typed.pop(); paint(); }
    else if (typed.length < 5 && LETTERS.includes(k)) { typed.push(k); paint(); say(''); }
  };
  const board = game.querySelector('.board');
  game.addEventListener('click', e => {
    const k = e.target.closest('[data-k]'); active = true;
    if (k) { press(k.dataset.k); board.focus({ preventScroll: true }); } // Enter must not press the key again
  });
  document.addEventListener('pointerdown', e => { active = !!e.target.closest('[data-game]'); });
  game.addEventListener('focusin', () => { active = true; list(); });
  document.addEventListener('keydown', e => {
    if (!active || e.ctrlKey || e.metaKey || e.altKey || e.target.closest('input,select,textarea')) return;
    const k = e.key === 'Enter' ? 'enter' : e.key === 'Backspace' ? 'back' : e.key.toLowerCase();
    if (k !== 'enter' && k !== 'back' && !(k.length === 1 && LETTERS.includes(k))) return;
    if (k === 'enter' && e.target.closest('button:not([data-k])')) return;
    e.preventDefault(); press(k);
  });
  if (shareBtn) shareBtn.addEventListener('click', () => {
    const grid = me.g.map(g => score(Array.from(g)).map(x => ({ ok: '🟩', near: '🟨', no: '⬜' })[x]).join('')).join('\n');
    const text = 'Riječililica br. ' + game.dataset.gameN + ' · ' + (me.done === 1 ? me.g.length : 'X') + '/6\n' + grid;
    if (navigator.share) navigator.share({ text }).catch(() => {});
    else if (navigator.clipboard) navigator.clipboard.writeText(text).then(() => say('Rezultat je kopiran'), () => {});
  });
  if (!answer) say('Igra se nije učitala');
  paint(); finish();
})();
