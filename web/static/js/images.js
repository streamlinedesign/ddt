/*!
 * Swiss image viewer, in the page's theme colours (falls back to black on white)
 * Add data-viewer to any <img> and include this script. Click opens, click anywhere closes.
 *
 * Optional attributes on the <img>:
 *   data-full="big.jpg"        higher-resolution file to show in the viewer
 *   data-caption="Title"       caption (falls back to the alt text)
 */
(() => {
  'use strict';
  if (window.__swissViewer) return;
  window.__swissViewer = true;

  const SELECTOR = 'img[data-viewer]';

  const CSS = `
img[data-viewer]{cursor:zoom-in}
img[data-viewer]:focus-visible{outline:2px solid var(--accent,#e30613);outline-offset:3px}

.sv{position:fixed;inset:0;z-index:2147483000;display:grid;
  grid-template-columns:repeat(12,minmax(0,1fr));grid-template-rows:auto minmax(0,1fr);
  column-gap:24px;row-gap:16px;padding:20px 24px 24px;background:var(--bg,#fff);color:var(--fg,#111);
  font-family:var(--font-ui,"Helvetica Neue",Helvetica,Arial,sans-serif);-webkit-font-smoothing:antialiased;
  cursor:zoom-out;outline:none;-webkit-tap-highlight-color:transparent;
  opacity:0;visibility:hidden;transition:opacity .22s ease,visibility 0s linear .22s}
.sv.is-open{opacity:1;visibility:visible;transition:opacity .22s ease}
.sv *{box-sizing:border-box;margin:0}

.sv-bar{grid-column:1/-1;display:flex;justify-content:space-between;align-items:center;
  padding-top:10px;border-top:2px solid var(--fg,#111);font-size:12px;line-height:1;font-weight:500;
  letter-spacing:.08em;text-transform:uppercase}
.sv-index{display:flex;align-items:center;gap:10px;font-variant-numeric:tabular-nums}
.sv-index::before{content:"";width:10px;height:10px;background:var(--accent,#e30613)}

.sv-figure{position:relative;grid-column:1/10;grid-row:2;min-height:0}
.sv-img{position:absolute;inset:0;width:100%;height:100%;object-fit:contain;
  object-position:left top;opacity:0;transform:translateY(8px);
  transition:opacity .3s ease,transform .3s ease}
.sv.is-loaded .sv-img{opacity:1;transform:none}

.sv-meta{grid-column:10/-1;grid-row:2;align-self:end;border-top:1px solid var(--fg,#111);padding-top:12px}
.sv-title{font-size:clamp(22px,2.6vw,36px);font-weight:700;line-height:1.05;
  letter-spacing:-.025em;overflow-wrap:anywhere}
.sv-detail{margin-top:14px;font-size:12px;line-height:1.4;letter-spacing:.08em;
  text-transform:uppercase;color:var(--fg-muted,#666);font-variant-numeric:tabular-nums}

@media (max-width:720px){
  .sv{column-gap:12px;padding:14px 16px 18px;grid-template-rows:auto minmax(0,1fr) auto}
  .sv-figure{grid-column:1/-1}
  .sv-meta{grid-column:1/-1;grid-row:3;align-self:start}
}
@media (prefers-reduced-motion:reduce){
  .sv,.sv.is-open,.sv-img{transition:none}
  .sv-img{transform:none}
}`;

  function init() {
    const style = document.createElement('style');
    style.textContent = CSS;
    document.head.appendChild(style);

    const root = document.createElement('div');
    root.className = 'sv';
    root.tabIndex = -1;
    root.setAttribute('role', 'dialog');
    root.setAttribute('aria-modal', 'true');
    root.setAttribute('aria-label', 'Image viewer');
    root.innerHTML = `
      <header class="sv-bar">
        <span class="sv-index"></span>
        <span>Close &times;</span>
      </header>
      <figure class="sv-figure"><img class="sv-img" alt=""></figure>
      <footer class="sv-meta">
        <h2 class="sv-title"></h2>
        <p class="sv-detail"></p>
      </footer>`;
    document.body.appendChild(root);

    const els = {
      index: root.querySelector('.sv-index'),
      img: root.querySelector('.sv-img'),
      title: root.querySelector('.sv-title'),
      detail: root.querySelector('.sv-detail'),
    };

    let lastFocus = null;
    let prevOverflow = '';
    const pad = (n) => String(n).padStart(2, '0');
    const isOpen = () => root.classList.contains('is-open');

    function open(img) {
      lastFocus = document.activeElement;

      const all = [...document.querySelectorAll(SELECTOR)];
      els.index.textContent = `${pad(all.indexOf(img) + 1)} / ${pad(all.length)}`;

      const caption = (img.dataset.caption || img.alt || '').trim();
      els.title.textContent = caption;
      els.title.hidden = !caption;
      els.detail.textContent = '';

      root.classList.remove('is-loaded');
      els.img.onload = () => {
        els.detail.textContent = `${els.img.naturalWidth} \u00d7 ${els.img.naturalHeight} px`;
        root.classList.add('is-loaded');
      };
      els.img.onerror = () => {
        els.detail.textContent = 'Image unavailable';
      };
      els.img.alt = img.alt || '';
      els.img.removeAttribute('src');
      els.img.src = img.dataset.full || img.currentSrc || img.src;

      prevOverflow = document.documentElement.style.overflow;
      document.documentElement.style.overflow = 'hidden';
      root.classList.add('is-open');
      root.focus({ preventScroll: true });
    }

    function close() {
      if (!isOpen()) return;
      root.classList.remove('is-open');
      document.documentElement.style.overflow = prevOverflow;
      if (lastFocus && lastFocus.focus) lastFocus.focus({ preventScroll: true });
    }

    // Anywhere on the viewer closes it, image included.
    root.addEventListener('click', close);

    // Delegated, so images added later work too. preventDefault keeps a wrapping
    // <a href="full.jpg"> from navigating when the script is running.
    document.addEventListener('click', (e) => {
      const img = e.target.closest && e.target.closest(SELECTOR);
      if (!img) return;
      e.preventDefault();
      open(img);
    });

    document.addEventListener('keydown', (e) => {
      if (isOpen()) {
        if (e.key === 'Escape') close();
        if (e.key === 'Tab') e.preventDefault(); // keep focus on the viewer
        return;
      }
      const img = e.target.closest && e.target.closest(SELECTOR);
      if (img && (e.key === 'Enter' || e.key === ' ')) {
        e.preventDefault();
        open(img);
      }
    });

    // Make the images reachable by keyboard.
    document.querySelectorAll(SELECTOR).forEach((img) => {
      if (!img.hasAttribute('tabindex')) img.tabIndex = 0;
      if (!img.hasAttribute('role')) img.setAttribute('role', 'button');
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();