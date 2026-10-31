import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { PatchView as Patch } from '../../core/models';
import { ErrorBox } from '../../shared/ui/error-box';
import { Skeleton } from '../../shared/ui/skeleton';
import { highlightLine, languageOf } from './highlight';
import { classifyPatch } from './patch';

interface Drawn {
  kind: string;
  /** The +, − or space the line starts with, drawn apart from the code. */
  mark: string;
  /** The line without its mark, for when there is nothing to colour. */
  code: string;
  /** The same code with its tokens marked up, or null to print `code`. */
  html: string | null;
}

/**
 * One file's diff, a line at a time.
 *
 * The code is coloured by the language the file's name implies. The mark a line
 * starts with is drawn apart from the code, so a removal is a removal by its
 * background and not by its text being red: a line coloured both by what it is
 * and by what it says cannot do either clearly.
 *
 * The markup comes from highlight.js, which escapes the text it is given, and
 * goes in through [innerHTML], which Angular sanitises — it keeps the spans and
 * their classes and drops anything else. The patch is a stranger's code, so it
 * passes two checks rather than one, neither of them trusting the other.
 */
@Component({
  selector: 'mv-patch-view',
  imports: [ErrorBox, Skeleton],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (!patch()) { <mv-skeleton height="4rem" /> }
    @else if (failure(); as e) { <mv-error [message]="e" /> }
    @else {
      <div class="patch mono" [class.full]="full()" tabindex="0" role="region" aria-label="Diff">
        @for (l of lines(); track $index) {
          @if (l.html !== null) {
            <div class="pl lit" [class]="l.kind"><span class="mk">{{ l.mark }}</span><span [innerHTML]="l.html"></span></div>
          } @else {
            <div class="pl" [class]="l.kind">{{ l.mark }}{{ l.code }}</div>
          }
        }
        @if (cut()) { <div class="pl meta">… cut: the rest of this diff is not shown here.</div> }
      </div>
    }`,
  styles: `
    .patch { overflow: auto; max-height: 70vh; background: var(--sunk); border-top: 1px solid var(--line-soft); font-size: .8rem; line-height: 1.5; padding: 6px 0 }
    /* Alone on its pane, the diff runs at its own length and the page scrolls. */
    .patch.full { max-height: none; border-top: 0; border-radius: 0 0 var(--r-lg) var(--r-lg) }
    .pl { white-space: pre; padding: 0 14px; width: max-content; min-width: 100% }
    .ins { background: var(--ok-bg); color: var(--ok) } .rem { background: var(--bad-bg); color: var(--bad) }
    .hunk { color: var(--info); background: var(--info-bg) } .meta { color: var(--faint) }
    /* A coloured line says what it is with its background and its mark; the
       text itself is left to the language's own colours. */
    .lit { color: var(--text) }
    .lit.ins .mk { color: var(--ok) } .lit.rem .mk { color: var(--bad) }
    .mk { opacity: .9 }

    /* highlight.js, in the panel's own palette rather than one of its themes:
       a theme is a page of colours chosen for somebody else's background and
       would not follow this one into dark mode. */
    :host ::ng-deep .hljs-comment, :host ::ng-deep .hljs-quote { color: var(--faint); font-style: italic }
    :host ::ng-deep .hljs-keyword, :host ::ng-deep .hljs-selector-tag,
    :host ::ng-deep .hljs-literal, :host ::ng-deep .hljs-doctag { color: var(--s9, var(--info)) }
    :host ::ng-deep .hljs-string, :host ::ng-deep .hljs-regexp,
    :host ::ng-deep .hljs-addition { color: var(--ok) }
    :host ::ng-deep .hljs-number, :host ::ng-deep .hljs-symbol,
    :host ::ng-deep .hljs-bullet { color: var(--warn) }
    :host ::ng-deep .hljs-title, :host ::ng-deep .hljs-name,
    :host ::ng-deep .hljs-section, :host ::ng-deep .hljs-title\\.function_ { color: var(--accent) }
    :host ::ng-deep .hljs-attr, :host ::ng-deep .hljs-attribute,
    :host ::ng-deep .hljs-variable, :host ::ng-deep .hljs-template-variable { color: var(--info) }
    :host ::ng-deep .hljs-type, :host ::ng-deep .hljs-built_in,
    :host ::ng-deep .hljs-class .hljs-title { color: var(--s7, var(--accent)) }
    :host ::ng-deep .hljs-meta, :host ::ng-deep .hljs-meta .hljs-keyword { color: var(--muted) }
    :host ::ng-deep .hljs-deletion { color: var(--bad) }
    :host ::ng-deep .hljs-emphasis { font-style: italic }
    :host ::ng-deep .hljs-strong { font-weight: 600 }`,
})
export class PatchViewer {
  readonly patch = input<Patch | { error: string } | undefined>();
  /** The file this diff is of. Its name is what decides the language. */
  readonly path = input<string>('');
  /** Alone on its pane: no height of its own, the page scrolls it. */
  readonly full = input(false);

  protected readonly failure = computed(() => { const p = this.patch(); return p && 'error' in p ? p.error : null; });
  private readonly real = computed(() => { const p = this.patch(); return p && !('error' in p) ? p : null; });
  private readonly language = computed(() => languageOf(this.path()));
  protected readonly cut = computed(() => !!this.real()?.truncated);

  protected readonly lines = computed<Drawn[]>(() => {
    const lang = this.language();
    return classifyPatch(this.real()?.patch ?? '').map((l) => {
      // A header or a hunk marker is not code and is left alone.
      if (l.kind === 'hunk' || l.kind === 'meta') return { kind: l.kind, mark: '', code: l.text, html: null };
      const mark = l.text.slice(0, 1);
      const code = l.text.slice(1);
      return { kind: l.kind, mark, code, html: highlightLine(code, lang) };
    });
  });
}
