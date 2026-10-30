import { say } from './status';

// Copy buttons: one real <button> per code block; prompts are not copied.
export function copyButtons(): void {
  document.querySelectorAll<HTMLElement>('.code').forEach((box) => {
    const pre = box.querySelector('pre');
    if (!pre) return;
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'copy';
    b.textContent = 'Copy';
    b.setAttribute('aria-label', 'Copy code block');
    b.addEventListener('click', () => {
      const c = pre.cloneNode(true) as HTMLElement;
      c.querySelectorAll('.p').forEach((p) => p.remove());
      const text = c.textContent ?? '';
      if (navigator.clipboard) {
        navigator.clipboard.writeText(text).then(
          () => say('Copied to clipboard'),
          () => say('Press Ctrl+C to copy'),
        );
      } else {
        say('Press Ctrl+C to copy');
      }
    });
    box.appendChild(b);
  });
}
