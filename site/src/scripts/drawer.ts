// Drawer under 900px: a toggle button, closed by Escape or by choosing a link.
// The hero page has no sidenav, so every piece of this is optional.
export function drawer(): void {
  const side = document.getElementById('side');
  const toggle = document.getElementById('navtoggle');
  if (!side || !toggle) return;

  function open(on: boolean): void {
    side!.classList.toggle('open', on);
    toggle!.setAttribute('aria-expanded', String(on));
  }

  toggle.addEventListener('click', () => open(toggle.getAttribute('aria-expanded') !== 'true'));
  side.addEventListener('click', (e) => {
    if ((e.target as Element | null)?.closest('a')) open(false);
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && side.classList.contains('open')) {
      open(false);
      toggle.focus();
    }
  });
}
