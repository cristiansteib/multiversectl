import { ChangeDetectionStrategy, Component, ElementRef, HostListener, computed, inject, input, signal } from '@angular/core';
import { ApiError } from '../../core/api.service';
import { Ops } from '../../core/ops.service';
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
      <form class="pop" role="dialog" [attr.aria-label]="'Delete universe ' + universe()" (submit)="submit($event)">
        <p>Deletes its worktrees (uncommitted work is lost), containers, volumes and state. The shared clones are not touched.</p>
        <label>
          <span>Type <b class="mono">{{ universe() }}</b> to confirm</span>
          <input type="text" name="confirm" autocomplete="off" autocapitalize="off" spellcheck="false" autofocus
                 [placeholder]="universe()" [value]="typed()" (input)="typed.set($any($event.target).value)" />
        </label>
        @if (error()) { <mv-error [message]="error()!" /> }
        <div class="row">
          <button type="button" class="btn ghost small" (click)="open.set(false)">Cancel</button>
          <button type="submit" class="btn danger solid small" [disabled]="!ready() || busy()" [attr.aria-busy]="busy()">{{ force() ? 'Delete anyway' : 'Delete' }}</button>
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
    .row { display: flex; justify-content: flex-end; gap: 8px }`,
})
export class RemoveUniversePopover {
  private readonly ops = inject(Ops);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  readonly multiverse = input.required<string>();
  readonly universe = input.required<string>();
  readonly disabled = input(false);
  readonly hint = input<string | null>(null);

  protected readonly open = signal(false);
  protected readonly typed = signal('');
  protected readonly error = signal<string | null>(null);
  protected readonly force = signal(false);
  protected readonly busy = signal(false);
  protected readonly ready = computed(() => this.typed() === this.universe());

  protected toggle(): void {
    this.open.update((o) => !o);
    this.typed.set('');
    this.error.set(null);
    this.force.set(false);
  }

  @HostListener('document:click', ['$event']) onDocClick(e: Event): void {
    if (this.open() && !this.host.nativeElement.contains(e.target as Node)) this.open.set(false);
  }
  @HostListener('keydown.escape') onEscape(): void { this.open.set(false); }

  protected async submit(e: Event): Promise<void> {
    e.preventDefault();
    if (!this.ready() || this.busy()) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      await this.ops.run({ action: 'rm', multiverse: this.multiverse(), universe: this.universe(), confirm: this.typed(), force: this.force() });
      this.open.set(false);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409 && err.body['dirty']) {
        this.force.set(true);
        this.error.set(`${err.message}. Press the button again to discard it.`);
      } else {
        this.error.set((err as Error).message);
      }
    } finally {
      this.busy.set(false);
    }
  }
}
