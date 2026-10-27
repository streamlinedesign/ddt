/* Izvori: the source chips fall and can be thrown about. matter.js loads
   only when the section comes into view, and never with reduced motion. */
(function () {
  const play = document.querySelector('[data-play]');
  if (!play || !('IntersectionObserver' in window) || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
  let started = false, running = false, runner = null, engine = null;
  const start = () => {
    const M = window.Matter; if (!M || started) return; started = true;
    const chips = [...play.querySelectorAll('.chip')];
    const sizes = chips.map(c => [c.offsetWidth, c.offsetHeight]);
    play.classList.add('live');
    const W = () => play.clientWidth, H = () => play.clientHeight;
    engine = M.Engine.create({ gravity: { y: 1 } });
    /* only a floor: a logo thrown out of one side comes back in at the other */
    const floor = M.Bodies.rectangle(W() / 2, H() + 30, W() * 3, 60, { isStatic: true });
    const bodies = chips.map((c, i) => M.Bodies.rectangle(
      30 + Math.random() * Math.max(10, W() - 60), -60 - i * 50, sizes[i][0], sizes[i][1],
      { chamfer: { radius: c.classList.contains('chip-logo') ? 4 : sizes[i][1] / 2 }, restitution: .35, friction: .3, angle: (Math.random() - .5) * .6 }));
    M.Composite.add(engine.world, [floor].concat(bodies));
    M.Events.on(engine, 'afterUpdate', () => bodies.forEach((b, i) => {
      const w = sizes[i][0];
      if (b.position.x < -w / 2) M.Body.setPosition(b, { x: W() + w / 2 - 1, y: b.position.y });
      else if (b.position.x > W() + w / 2) M.Body.setPosition(b, { x: -w / 2 + 1, y: b.position.y });
      if (b.position.y > H() + 200) M.Body.setPosition(b, { x: W() / 2, y: -80 });
      chips[i].style.transform = 'translate(' + (b.position.x - w / 2) + 'px,' + (b.position.y - sizes[i][1] / 2) + 'px) rotate(' + b.angle + 'rad)';
    }));
    new ResizeObserver(() => { M.Body.setPosition(floor, { x: W() / 2, y: H() + 30 }); }).observe(play);
    const shake = document.querySelector('[data-play-shake]');
    if (shake) {
      shake.hidden = false;
      shake.addEventListener('click', () => bodies.forEach(b => {
        M.Body.setVelocity(b, { x: (Math.random() - .5) * 24, y: -10 - Math.random() * 14 });
        M.Body.setAngularVelocity(b, (Math.random() - .5) * .4);
      }));
    }
    /* dragging: a spring from the chip to the pointer */
    let hold = null;
    const at = e => { const r = play.getBoundingClientRect(); return { x: e.clientX - r.left, y: e.clientY - r.top }; };
    chips.forEach((c, i) => c.addEventListener('pointerdown', e => {
      e.preventDefault(); c.setPointerCapture(e.pointerId); c.classList.add('held');
      const p = at(e), b = bodies[i];
      hold = M.Constraint.create({ pointA: p, bodyB: b, pointB: { x: p.x - b.position.x, y: p.y - b.position.y }, stiffness: .2, damping: .1 });
      M.Composite.add(engine.world, hold);
    }));
    play.addEventListener('pointermove', e => { if (hold) hold.pointA = at(e); });
    const drop = () => { if (!hold) return; M.Composite.remove(engine.world, hold); hold = null; chips.forEach(c => c.classList.remove('held')); };
    play.addEventListener('pointerup', drop); play.addEventListener('pointercancel', drop); play.addEventListener('lostpointercapture', drop);
    runner = M.Runner.create(); M.Runner.run(runner, engine); running = true;
  };
  new IntersectionObserver(es => es.forEach(e => {
    if (e.isIntersecting && !window.Matter && !play.dataset.loading) {
      play.dataset.loading = '1';
      const s = document.createElement('script'); s.src = play.dataset.play; s.onload = start; document.head.append(s);
    } else if (e.isIntersecting) start();
    if (!runner) return;
    if (e.isIntersecting && !running) { window.Matter.Runner.run(runner, engine); running = true; }
    else if (!e.isIntersecting && running) { window.Matter.Runner.stop(runner); running = false; }
  }), { rootMargin: '200px' }).observe(play);
})();
