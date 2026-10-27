// Dashboard: the phone menu, the light/dark switch, and an error bar for failed htmx requests
// (htmx does not swap error pages in, so without this a failure would look like nothing happened).
(function () {
  var root = document.documentElement;
  function bar() { return document.getElementById('greska'); }
  function show(text) {
    var el = bar();
    if (!el) return;
    el.textContent = (text || 'Nešto nije uspjelo.').trim().slice(0, 300);
    el.hidden = false;
    el.scrollIntoView({ block: 'nearest' });
  }
  document.addEventListener('htmx:beforeRequest', function () { var el = bar(); if (el) el.hidden = true; });
  document.addEventListener('htmx:responseError', function (e) { show(e.detail.xhr.responseText); });
  document.addEventListener('htmx:sendError', function () { show('Poslužitelj se ne javlja. Provjeri vezu i pokušaj ponovo.'); });
  document.addEventListener('htmx:afterSwap', function () { root.classList.remove('nav-open'); });

  document.addEventListener('click', function (e) {
    if (e.target.closest('[data-nav-open]')) root.classList.add('nav-open');
    if (e.target.closest('[data-nav-close]')) root.classList.remove('nav-open');
    if (e.target.closest('[data-theme-toggle]')) {
      var light = root.getAttribute('data-theme') !== 'light';
      if (light) root.setAttribute('data-theme', 'light'); else root.removeAttribute('data-theme');
      try { localStorage.setItem('admin-theme', light ? 'light' : 'dark'); } catch (err) {}
    }
  });
  document.addEventListener('keydown', function (e) { if (e.key === 'Escape') root.classList.remove('nav-open'); });
})();
