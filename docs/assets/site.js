(function () {
  var d = document, root = d.documentElement, status = d.getElementById('status');
  root.classList.add('js');

  function say(msg) { status.textContent = msg; setTimeout(function () { status.textContent = ''; }, 2000); }

  // Copy buttons: one real <button> per code block; prompts are not copied.
  [].forEach.call(d.querySelectorAll('.code'), function (box) {
    var b = d.createElement('button'), pre = box.querySelector('pre');
    b.type = 'button'; b.className = 'copy'; b.textContent = 'Copy';
    b.setAttribute('aria-label', 'Copy code block');
    b.addEventListener('click', function () {
      var c = pre.cloneNode(true);
      [].forEach.call(c.querySelectorAll('.p'), function (p) { p.remove(); });
      var text = c.textContent;
      if (navigator.clipboard) navigator.clipboard.writeText(text).then(function () { say('Copied to clipboard'); }, function () { say('Press Ctrl+C to copy'); });
      else say('Press Ctrl+C to copy');
    });
    box.appendChild(b);
  });

  // Install tabs.
  var tabs = [].slice.call(d.querySelectorAll('[role=tab]'));
  function select(t) {
    tabs.forEach(function (x) {
      var on = x === t;
      x.setAttribute('aria-selected', on);
      x.tabIndex = on ? 0 : -1;
      d.getElementById(x.getAttribute('aria-controls')).hidden = !on;
    });
    t.focus();
  }
  tabs.forEach(function (t, i) {
    t.addEventListener('click', function () { select(t); });
    t.addEventListener('keydown', function (e) {
      var n = { ArrowRight: i + 1, ArrowLeft: i - 1, Home: 0, End: tabs.length - 1 }[e.key];
      if (n === undefined) return;
      e.preventDefault();
      select(tabs[(n + tabs.length) % tabs.length]);
    });
  });

  // Drawer under 900px: a toggle button, closed by Escape or by choosing a link.
  var side = d.getElementById('side'), toggle = d.getElementById('navtoggle');
  function drawer(open) { side.classList.toggle('open', open); toggle.setAttribute('aria-expanded', open); }
  toggle.addEventListener('click', function () { drawer(toggle.getAttribute('aria-expanded') !== 'true'); });
  side.addEventListener('click', function (e) { if (e.target.closest('a')) drawer(false); });
  d.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && side.classList.contains('open')) { drawer(false); toggle.focus(); }
  });

  // Theme: dark unless the reader asked for light. The head applies a stored
  // choice before the first paint; this keeps the button, the browser's bar
  // colour and the screenshots that come in a light version in step with it.
  var theme = d.getElementById('theme'), bar = d.querySelector('meta[name=theme-color]');
  function paint(light) {
    if (light) root.dataset.theme = 'light'; else delete root.dataset.theme;
    theme.setAttribute('aria-pressed', light);
    bar.content = light ? '#f5f7fa' : '#11141b';
    [].forEach.call(d.querySelectorAll('source[data-light]'), function (s) { s.media = light ? 'all' : 'not all'; });
  }
  paint(root.dataset.theme === 'light');
  theme.hidden = false;
  theme.addEventListener('click', function () {
    var light = root.dataset.theme !== 'light';
    paint(light);
    try { localStorage.setItem('theme', light ? 'light' : 'dark'); } catch (e) {}
  });

  // Scroll-spy over the table of contents. The menu is a list of pages now,
  // and the page you are on is marked when it is rendered.
  var links = {};
  [].forEach.call(d.querySelectorAll('.toc a[href^="#"]'), function (a) { links[a.hash.slice(1)] = a; });
  if ('IntersectionObserver' in window) {
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (en) {
        if (!en.isIntersecting) return;
        Object.keys(links).forEach(function (k) { links[k].removeAttribute('aria-current'); });
        links[en.target.id].setAttribute('aria-current', 'true');
      });
    }, { rootMargin: '-15% 0px -75% 0px' });
    Object.keys(links).forEach(function (k) { var s = d.getElementById(k); if (s) io.observe(s); });
  }
})();
