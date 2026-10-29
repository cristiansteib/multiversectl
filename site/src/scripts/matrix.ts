// Escape closes a comparison note until the pointer or focus moves to another
// row, as notes that open on hover must allow (WCAG 1.4.13).
export function matrix(): void {
  const tables = Array.from(document.querySelectorAll<HTMLElement>('.matrix'));
  if (!tables.length) return;

  tables.forEach((m) => {
    let row: Element | null = null;
    function moved(e: Event): void {
      const r = (e.target as Element | null)?.closest('tr') ?? null;
      if (r !== row) {
        row = r;
        m.classList.remove('hush');
      }
    }
    m.addEventListener('mouseover', moved);
    m.addEventListener('focusin', moved);
    m.addEventListener('mouseleave', () => {
      row = null;
      m.classList.remove('hush');
    });
  });

  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    tables.forEach((m) => m.classList.add('hush'));
  });
}
