// @ts-nocheck -- the old site's hand-written script for this page,
// carried over as it was. Rewrite it as typed TypeScript when you touch it.
import gsap from 'gsap';
(function () {
  var fig = document.getElementById('ag');
  if (!fig || typeof gsap === 'undefined') return;
  var lanes = [].slice.call(fig.querySelectorAll('.ag-lane'));
  var q = function (l, s) { return l.querySelector(s); };
  var qq = function (l, s) { return [].slice.call(l.querySelectorAll(s)); };
  var set = function (el, k, v) { if (el) el.setAttribute(k, v); };

  // One lane's two states. "up" is also what a reader who asked for no motion
  // is left looking at: everything running, the address answering.
  function phase(l, up) {
    qq(l, '.ag-svc').forEach(function (s) { set(s, 'data-up', up ? '1' : '0'); });
    set(q(l, '.ag-net'), 'data-up', up ? '1' : '0');
    set(l, 'data-live', up ? '1' : '0');
    var stat = q(l, '.ag-stat');
    stat.textContent = up ? '200 OK' : 'thinking';
    set(stat, 'data-ok', up ? '1' : '0');
    gsap.set(q(l, '.ag-ok'), {opacity: up ? 1 : 0});
  }

  if (window.matchMedia && matchMedia('(prefers-reduced-motion:reduce)').matches) {
    lanes.forEach(function (l) { phase(l, true); });
    return;
  }

  // Each lane runs the same cycle, started a beat apart, so the reader always
  // sees three agents at three different points of it.
  var tls = lanes.map(function (l, i) {
    var dots = qq(l, '.ag-dot'), svcs = qq(l, '.ag-svc'), net = q(l, '.ag-net');
    var stat = q(l, '.ag-stat'), tok = q(l, '.ag-tok'), req = q(l, '.ag-req');
    var glow = q(l, '.ag-glow'), tick = q(l, '.ag-ok');
    var say = function (text, ok) {
      return function () { stat.textContent = text; set(stat, 'data-ok', ok ? '1' : '0'); };
    };
    var tl = gsap.timeline({repeat: -1, repeatDelay: 0.8, delay: i * 1.6});
    tl.call(function () { phase(l, false); }, null, 0)
      .to(dots, {opacity: 0.2, duration: 0.3, repeat: 5, yoyo: true, stagger: {each: 0.12}}, 0)
      .call(say('commit'), null, 1.3)
      .fromTo(tok, {opacity: 0, attr: {cy: 144}}, {opacity: 1, attr: {cy: 186}, duration: 0.7, ease: 'power1.in'}, 1.3)
      .to(tok, {opacity: 0, duration: 0.2}, 1.95)
      .call(function () { set(l, 'data-live', '1'); }, null, 2.0)
      .call(say('building'), null, 2.05)
      .fromTo(glow, {opacity: 0}, {opacity: 1, duration: 0.45}, 2.0);
    svcs.forEach(function (sv, k) {
      tl.call(function () { set(sv, 'data-up', '1'); }, null, 2.25 + k * 0.33);
    });
    tl.call(function () { set(net, 'data-up', '1'); }, null, 3.3)
      .call(say('checking'), null, 3.35)
      .fromTo(req, {opacity: 0, attr: {cy: 186}}, {opacity: 1, attr: {cy: 146}, duration: 0.7, ease: 'power1.out'}, 3.5)
      .to(req, {opacity: 0, duration: 0.2}, 4.2)
      .fromTo(tick, {opacity: 0, scale: 0.5}, {opacity: 1, scale: 1, duration: 0.4, ease: 'back.out(2.2)', transformOrigin: '50% 50%'}, 4.0)
      .call(say('200 OK', true), null, 4.35)
      // It holds, up and answering, and then goes quiet for the next round.
      .call(function () { phase(l, false); }, null, 6.6)
      .to([glow, tick], {opacity: 0, duration: 0.5}, 6.6)
      .to(dots, {opacity: 1, duration: 0.3}, 6.6);
    return tl;
  });

  // The wire keeps flowing, so a lane looks connected while it waits.
  gsap.to(fig.querySelectorAll('.ag-wire'),
    {attr: {'stroke-dashoffset': -15}, duration: 0.9, repeat: -1, ease: 'none'});

  // The lanes run without stopping, on screen or off. They used to pause when
  // the figure scrolled out of view, which saved a little work and meant a
  // reader who scrolled back found the scene mid-stride at whatever point they
  // had left it. Running throughout costs three short GSAP timelines over an
  // SVG — no layout, no network — and the scene is always where it would have
  // been. A reader who asks for less motion still gets none of it: that case
  // returns above, before a timeline is ever built.
})();
