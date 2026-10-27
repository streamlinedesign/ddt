// Runs before the dashboard paints, so a light-theme admin never sees a dark flash.
try { if (localStorage.getItem('admin-theme') === 'light') document.documentElement.setAttribute('data-theme', 'light'); } catch (e) {}
