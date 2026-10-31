// Custom Google Analytics events. The page view, its title, scrolls, outbound
// clicks and downloads come from the tag in BaseHead.astro with Enhanced
// Measurement; these are the interactions it cannot see.
//
// gtag is absent when a blocker removed the tag or the loader has not arrived
// yet, and the page behaves the same either way.
declare global {
  interface Window {
    gtag?: (command: 'event', name: string, params?: Record<string, string>) => void;
  }
}

export function track(name: string, params?: Record<string, string>): void {
  try {
    window.gtag?.('event', name, params);
  } catch {
    /* analytics must never break the page */
  }
}
