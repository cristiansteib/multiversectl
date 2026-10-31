// The cookie banner. Analytics storage is denied until the reader accepts (the
// default is set in src/components/BaseHead.astro, which also applies a stored
// "granted" before the first hit). Advertising signals are never granted: the
// site runs no ads, so there is nothing to ask about.
//
// The choice is kept in localStorage under 'consent'. Without JavaScript there
// is no banner and the default stands: denied.
import { track } from './analytics';

const KEY = 'consent';

type Choice = 'granted' | 'denied';

function stored(): Choice | null {
  try {
    const v = localStorage.getItem(KEY);
    return v === 'granted' || v === 'denied' ? v : null;
  } catch {
    return null;
  }
}

function save(choice: Choice): void {
  try {
    localStorage.setItem(KEY, choice);
  } catch {
    /* storage blocked: the choice lasts until the page closes */
  }
  window.gtag?.('consent', 'update', { analytics_storage: choice });
}

function banner(): HTMLElement {
  const el = document.createElement('div');
  el.className = 'consent';
  el.setAttribute('role', 'region');
  el.setAttribute('aria-label', 'Cookie consent');
  el.innerHTML = `
    <p>This site uses Google Analytics cookies to count visits and see which pages help.
    Nothing is collected unless you accept.</p>
    <div class="consent-actions">
      <button type="button" class="btn btn-2" data-consent="denied">Reject</button>
      <button type="button" class="btn btn-1" data-consent="granted">Accept</button>
    </div>`;
  return el;
}

export function consent(): void {
  const reopen = document.getElementById('cookie-settings');
  let open: HTMLElement | null = null;

  function show(): void {
    if (open) return;
    open = banner();
    open.addEventListener('click', (e) => {
      const choice = (e.target as HTMLElement).closest<HTMLElement>('[data-consent]')?.dataset.consent;
      if (choice !== 'granted' && choice !== 'denied') return;
      save(choice);
      if (choice === 'granted') track('consent_granted');
      open?.remove();
      open = null;
      reopen?.focus();
    });
    document.body.appendChild(open);
  }

  if (reopen) {
    reopen.hidden = false;
    reopen.addEventListener('click', show);
  }
  if (stored() === null) show();
}
