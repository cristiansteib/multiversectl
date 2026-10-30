import { say } from './status';

/**
 * Copy buttons, one per code block that asks for one.
 *
 * What lands on the clipboard depends on what the block holds, which
 * Terminal.astro states in data-copy:
 *
 *   input   a shell. Only what the user typed — the .in spans — one command
 *           per line. Not the prompt the shell drew, not the output it
 *           printed, not the comments the page added. Pasting the result into
 *           a shell runs exactly what the example ran.
 *   all     a file. The whole thing, because the whole thing is what you would
 *           paste into an editor.
 *   absent  output alone. No button: there is nothing here anyone would paste.
 */
export function copyButtons(): void {
  document.querySelectorAll<HTMLElement>('.code[data-copy]').forEach((box) => {
    const pre = box.querySelector('pre');
    if (!pre) return;

    const mode = box.dataset.copy;
    const text = () => {
      if (mode === 'input') {
        const typed = [...pre.querySelectorAll('.in')].map((e) => e.textContent ?? '');
        return typed.join('\n');
      }
      return pre.textContent ?? '';
    };

    // A shell block with nothing marked as input has nothing to give.
    if (mode === 'input' && !pre.querySelector('.in')) return;

    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'copy';
    b.textContent = 'Copy';
    b.setAttribute('aria-label', mode === 'input'
      ? 'Copy the commands in this example'
      : 'Copy the contents of this file');
    b.addEventListener('click', () => {
      const out = text();
      if (navigator.clipboard) {
        navigator.clipboard.writeText(out).then(
          () => say(mode === 'input'
            ? `Copied ${out.split('\n').length === 1 ? 'the command' : 'the commands'} to clipboard`
            : 'Copied to clipboard'),
          () => say('Press Ctrl+C to copy'),
        );
      } else {
        say('Press Ctrl+C to copy');
      }
    });
    box.appendChild(b);
  });
}
