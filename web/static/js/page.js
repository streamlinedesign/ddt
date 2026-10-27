(function () {
  const root = document.documentElement;

  function read(key, fallback) {
    try { return localStorage.getItem(key) || fallback; } catch (e) { return fallback; }
  }
  function write(key, value) {
    try { localStorage.setItem(key, value); } catch (e) {}
  }

  const state = {
    theme: read('theme', 'light'),
  };

  function setActive(selector, attr, value) {
    document.querySelectorAll(selector).forEach(btn => {
      const on = btn.dataset[attr] === value;
      btn.classList.toggle('active', on);
      btn.setAttribute('aria-pressed', String(on));
    });
  }

  function applyTheme(theme) {
    state.theme = theme;
    if (theme === 'dark' || theme === 'sepia') {
      root.setAttribute('data-theme', theme);
    } else {
      root.removeAttribute('data-theme');
    }
    write('theme', theme);
    setActive('.switch-btn[data-theme]', 'theme', theme);
  }

  applyTheme(state.theme);

  document.querySelectorAll('.switch-btn[data-theme]').forEach(btn => {
    btn.addEventListener('click', () => applyTheme(btn.dataset.theme));
  });

  /* weather city: remembered per reader; the print carries every city's forecast and shows one */
  const city = document.getElementById('city');
  if (city) {
    city.value = read('city', 'Zagreb');
    const show = () => {
      const day = document.querySelector('.weather-day[data-city="' + city.value + '"]') || document.querySelector('.weather-day');
      document.querySelectorAll('.weather-day').forEach(el => el.hidden = el !== day);
      document.querySelectorAll('.weather-city').forEach(el => el.textContent = day.dataset.city);
      const src = document.querySelector('.weather .panel-source');
      if (src && day.dataset.url) src.href = day.dataset.url;
    };
    show();
    city.addEventListener('change', () => { write('city', city.value); show(); });
  }
})();

/* tabs: any [data-tabs] block, buttons [data-tab] show the matching [data-panel] */
document.querySelectorAll('[data-tabs]').forEach(box => {
  const buttons = [...box.querySelectorAll('[data-tab]')];
  const show = key => {
    buttons.forEach(b => { const on = b.dataset.tab === key; b.classList.toggle('is-active', on); b.setAttribute('aria-selected', on); });
    box.querySelectorAll('[data-panel]').forEach(p => { const on = p.dataset.panel === key; p.classList.toggle('is-active', on); p.hidden = !on; });
  };
  buttons.forEach((b, i) => {
    b.addEventListener('click', () => show(b.dataset.tab));
    b.addEventListener('keydown', e => {
      const d = { ArrowRight: 1, ArrowLeft: -1 }[e.key];
      if (!d) return;
      const next = buttons[(i + d + buttons.length) % buttons.length];
      e.preventDefault(); show(next.dataset.tab); next.focus();
    });
  });
});

/* Hacker News: show only the host of each link */
document.querySelectorAll('.hn-url a').forEach(a => {
  try { a.textContent = new URL(a.href).hostname.replace(/^www\./, ''); } catch (e) {}
});
