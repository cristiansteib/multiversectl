import { ChangeDetectionStrategy, Component, ElementRef, HostListener, effect, inject, input, signal, viewChild } from '@angular/core';
import { Ops } from '../../core/ops.service';
import { RemoveUniverseForm } from './remove-universe';
import { ErrorBox } from '../ui/error-box';
import { Icon } from '../ui/icon';

/**
 * The remove button beside Start and Stop, and the small widget that floats
 * under it asking for the universe's name. It is the same act as the dialog on
 * the Details tab — worktrees, containers and volumes go — so it asks the same:
 * the name typed, and a second, deliberate press for uncommitted work.
 */
@Component({
  selector: 'mv-remove-universe-popover',
  imports: [ErrorBox, Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <button type="button" class="btn danger" [disabled]="disabled()" [attr.title]="hint()" aria-haspopup="dialog" [attr.aria-expanded]="open()" (click)="toggle()">
      <mv-icon name="trash" /> Delete
    </button>
    @if (open()) {
      <form class="pop" role="dialog" [attr.aria-label]="'Delete universe ' + universe()" (submit)="f.submit($event)">
        <p>Deletes its worktrees (uncommitted work is lost), containers, volumes and state. The shared clones are not touched.</p>
        <label>
          <span>Type <b class="mono">{{ universe() }}</b> to confirm</span>
          <input type="text" name="confirm" autocomplete="off" autocapitalize="off" spellcheck="false" #box
                 [placeholder]="universe()" [value]="f.typed()" (input)="f.typed.set($any($event.target).value)" />
        </label>
        @if (f.error()) { <mv-error [message]="f.error()!" /> }
        <div class="row">
          <button type="button" class="btn ghost small" (click)="open.set(false)">Cancel</button>
          <button type="submit" class="btn danger solid small" [disabled]="!f.ready() || f.busy()" [attr.aria-busy]="f.busy()">{{ f.force() ? 'Delete anyway' : 'Delete' }}</button>
        </div>
      </form>
    }`,
  styles: `
    :host { position: relative; display: inline-block }
    .pop { position: absolute; right: 0; top: calc(100% + 8px); z-index: 20; width: min(340px, calc(100vw - 32px)); padding: 14px; display: flex; flex-direction: column; gap: 10px;
      background: var(--panel); border: 1px solid var(--bad); border-radius: var(--r-lg); box-shadow: 0 10px 30px rgb(0 0 0 / .28) }
    p { margin: 0; font-size: .86rem; color: var(--muted) }
    label { display: flex; flex-direction: column; gap: 5px; font-size: .86rem }
    input { padding: 7px 10px; color: var(--text); font: inherit; background: var(--bg, var(--panel)); border: 1px solid var(--line); border-radius: var(--r-md) }
    input:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px }
    .row { display: flex; justify-content: flex-end; gap: 8px }
    /* Where the actions wrap, the button may sit at the left edge, so a box anchored to its right edge would leave the screen. */
    @media (max-width: 560px) { :host { position: static } .pop { position: fixed; left: 16px; right: 16px; top: 96px; width: auto } }`,
})
export class RemoveUniversePopover {
  private readonly ops = inject(Ops);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  readonly multiverse = input.required<string>();
  readonly universe = input.required<string>();
  readonly disabled = input(false);
  readonly hint = input<string | null>(null);

  protected readonly open = signal(false);
  protected readonly f = new RemoveUniverseForm(this.ops, () => this.multiverse(), () => this.universe(), () => this.open.set(false));

  private readonly box = viewChild<ElementRef<HTMLInputElement>>('box');

  constructor() {
    // `autofocus` does nothing on an element added after the page loaded, so the caret is put there by hand.
    effect(() => this.box()?.nativeElement.focus());
  }

  protected toggle(): void {
    this.open.update((o) => !o);
    this.f.reset();
  }

  @HostListener('document:click', ['$event']) onDocClick(e: Event): void {
    if (this.open() && !this.host.nativeElement.contains(e.target as Node)) this.open.set(false);
  }
  @HostListener('keydown.escape') onEscape(): void { this.open.set(false); }
}
