// Scroll-spy over the table of contents. The menu is a list of pages, and the
// page you are on is marked when it is rendered, so only this column moves.
export function toc(): void {
  const links: Record<string, HTMLAnchorElement> = {};
  document.querySelectorAll<HTMLAnchorElement>('.toc a[href^="#"]').forEach((a) => {
    links[a.hash.slice(1)] = a;
  });
  const ids = Object.keys(links);
  if (!ids.length || !('IntersectionObserver' in window)) return;

  const io = new IntersectionObserver(
    (entries) => {
      entries.forEach((en) => {
        if (!en.isIntersecting) return;
        ids.forEach((k) => links[k]?.removeAttribute('aria-current'));
        links[en.target.id]?.setAttribute('aria-current', 'true');
      });
    },
    { rootMargin: '-15% 0px -75% 0px' },
  );
  ids.forEach((k) => {
    const s = document.getElementById(k);
    if (s) io.observe(s);
  });
}
