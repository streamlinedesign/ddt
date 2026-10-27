/* runs in <head> before the page paints, so the reader never sees the wrong theme */
try {
  var c = localStorage.getItem('theme');
  if (c === 'dark' || c === 'sepia') document.documentElement.setAttribute('data-theme', c);
} catch (e) {}
