// Theme: the reader's stored choice, else their system setting. The inline
// snippet in <head> (see src/components/BaseHead.astro) applies it before the
// first paint; this keeps the button, the browser's bar colour and the screenshots
// that come in a light version in step with it.
import { track } from './analytics';

export const STORAGE_KEY = 'theme';

export function theme(): void {
  const root = document.documentElement;
  const button = document.getElementById('theme');
  const bar = document.querySelector<HTMLMetaElement>('meta[name=theme-color]');

  function paint(dark: boolean): void {
    if (dark) root.dataset.theme = 'dark';
    else delete root.dataset.theme;
    if (button) {
      button.setAttribute('aria-pressed', String(dark));
      button.setAttribute('aria-label', dark ? 'Light theme' : 'Dark theme');
    }
    if (bar) bar.content = dark ? '#11141b' : '#f5f7fa';
    document.querySelectorAll<HTMLSourceElement>('source[data-light]').forEach((s) => {
      s.media = dark ? 'not all' : 'all';
    });
  }

  paint(root.dataset.theme === 'dark');
  if (!button) return;
  button.hidden = false;
  button.addEventListener('click', () => {
    const dark = root.dataset.theme !== 'dark';
    paint(dark);
    track('change_theme', { theme: dark ? 'dark' : 'light' });
    try {
      localStorage.setItem(STORAGE_KEY, dark ? 'dark' : 'light');
    } catch {
      /* a browser with storage blocked still gets the theme, just not kept */
    }
  });
}
