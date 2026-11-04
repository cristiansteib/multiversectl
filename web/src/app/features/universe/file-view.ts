import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { marked } from 'marked';
import { highlightAll, languageOf } from './highlight';

/**
 * A file as it stands now, not as a diff: its raw text, coloured by its
 * language, or — for markdown — rendered.
 *
 * Both paths produce HTML that goes in through [innerHTML], which Angular
 * sanitises: highlight.js escapes the text and adds only its own classes, and
 * marked's output (a stranger's document, which may carry raw HTML) is stripped
 * of anything executable before it is shown. The file is never trusted.
 */
@Component({
  selector: 'mv-file-view',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (mode() === 'rendered') {
      <article class="md" [innerHTML]="rendered()"></article>
    } @else if (html(); as h) {
      <pre class="raw mono" [class.wrap]="wrap()"><code [innerHTML]="h"></code></pre>
    } @else {
      <pre class="raw mono" [class.wrap]="wrap()">{{ content() }}</pre>
    }`,
  styles: `
    :host { display: block }
    .raw { margin: 0; overflow: auto; background: var(--sunk); font-size: .8rem; line-height: 1.5; padding: 10px 14px; white-space: pre; tab-size: 2; color: var(--text) }
    .raw.wrap { white-space: pre-wrap; overflow-wrap: anywhere }
    .raw code { font: inherit }

    /* The rendered document, in the panel's own type and tokens. */
    .md { padding: 16px 20px 22px; max-width: 76ch; color: var(--text); font-size: .95rem; line-height: 1.65; overflow-wrap: anywhere }
    .md > :first-child { margin-top: 0 }
    .md h1, .md h2, .md h3, .md h4 { line-height: 1.25; font-weight: 600; letter-spacing: -.01em; margin: 1.4em 0 .5em }
    .md h1 { font-size: 1.5rem; padding-bottom: .25em; border-bottom: 1px solid var(--line) }
    .md h2 { font-size: 1.25rem; padding-bottom: .2em; border-bottom: 1px solid var(--line-soft) }
    .md h3 { font-size: 1.08rem } .md h4 { font-size: .98rem }
    .md p, .md ul, .md ol, .md blockquote, .md table, .md pre { margin: 0 0 .9em }
    .md ul, .md ol { padding-left: 1.5em } .md li { margin: .2em 0 }
    .md a { color: var(--accent); text-underline-offset: 3px }
    .md code { font-family: var(--mono); font-size: .88em; background: var(--sunk); padding: .12em .4em; border-radius: var(--r-sm) }
    .md pre { background: var(--sunk); border: 1px solid var(--line-soft); border-radius: var(--r-md); padding: 12px 14px; overflow: auto }
    .md pre code { background: none; padding: 0; font-size: .82rem; line-height: 1.5 }
    .md blockquote { border-left: 3px solid var(--line); padding-left: 1em; color: var(--muted); margin-left: 0 }
    .md hr { border: 0; border-top: 1px solid var(--line); margin: 1.6em 0 }
    .md table { border-collapse: collapse; font-size: .9em } .md th, .md td { border: 1px solid var(--line); padding: 6px 10px; text-align: left }
    .md th { background: var(--sunk); font-weight: 600 }
    .md img { max-width: 100% }

    /* highlight.js tokens in the panel's palette, as the diff view does it. */
    :host ::ng-deep .hljs-comment, :host ::ng-deep .hljs-quote { color: var(--faint); font-style: italic }
    :host ::ng-deep .hljs-keyword, :host ::ng-deep .hljs-selector-tag,
    :host ::ng-deep .hljs-literal, :host ::ng-deep .hljs-doctag { color: var(--s9, var(--info)) }
    :host ::ng-deep .hljs-string, :host ::ng-deep .hljs-regexp, :host ::ng-deep .hljs-addition { color: var(--ok) }
    :host ::ng-deep .hljs-number, :host ::ng-deep .hljs-symbol, :host ::ng-deep .hljs-bullet { color: var(--warn) }
    :host ::ng-deep .hljs-title, :host ::ng-deep .hljs-name,
    :host ::ng-deep .hljs-section, :host ::ng-deep .hljs-title\\.function_ { color: var(--accent) }
    :host ::ng-deep .hljs-attr, :host ::ng-deep .hljs-attribute,
    :host ::ng-deep .hljs-variable, :host ::ng-deep .hljs-template-variable { color: var(--info) }
    :host ::ng-deep .hljs-type, :host ::ng-deep .hljs-built_in, :host ::ng-deep .hljs-class .hljs-title { color: var(--s7, var(--accent)) }
    :host ::ng-deep .hljs-meta, :host ::ng-deep .hljs-meta .hljs-keyword { color: var(--muted) }
    :host ::ng-deep .hljs-emphasis { font-style: italic } :host ::ng-deep .hljs-strong { font-weight: 600 }`,
})
export class FileViewer {
  readonly content = input<string>('');
  readonly path = input<string>('');
  readonly mode = input<'raw' | 'rendered'>('raw');
  readonly wrap = input(false);

  private readonly language = computed(() => languageOf(this.path()));
  protected readonly html = computed(() => highlightAll(this.content(), this.language()));
  protected readonly rendered = computed(() => marked.parse(this.content(), { async: false, gfm: true, breaks: false }) as string);
}
