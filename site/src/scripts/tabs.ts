import { track } from './analytics';

// Install tabs: one tablist per page, driven by the roving tabindex pattern.
export function tabs(): void {
  const items = Array.from(document.querySelectorAll<HTMLElement>('[role=tab]'));
  if (!items.length) return;

  function select(t: HTMLElement): void {
    items.forEach((x) => {
      const on = x === t;
      x.setAttribute('aria-selected', String(on));
      x.tabIndex = on ? 0 : -1;
      const panel = document.getElementById(x.getAttribute('aria-controls') ?? '');
      if (panel) panel.hidden = !on;
    });
    t.focus();
  }

  items.forEach((t, i) => {
    t.addEventListener('click', () => {
      select(t);
      track('select_install_method', { method: t.textContent?.trim() ?? '' });
    });
    t.addEventListener('keydown', (e) => {
      const moves: Record<string, number> = {
        ArrowRight: i + 1,
        ArrowLeft: i - 1,
        Home: 0,
        End: items.length - 1,
      };
      const n = moves[e.key];
      if (n === undefined) return;
      e.preventDefault();
      const next = items[(n + items.length) % items.length];
      if (next) select(next);
    });
  });
}
