import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { Events } from '../core/events.service';
import { Updater } from '../core/updater.service';
import { CopyButton } from '../shared/ui/copy-button';
import { Icon } from '../shared/ui/icon';
import { Modal } from '../shared/ui/modal';

/** What a newer release brings, and the button that installs it. */
@Component({
  selector: 'mv-update-dialog',
  imports: [Modal, CopyButton, Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (updater.dialog() && updater.notice(); as n) {
      <mv-modal title="Update multiversectl" (closed)="updater.hide()">
        <p class="versions">
          <span class="mono">{{ n.current }}</span> <mv-icon name="chevron" /> <strong class="mono">{{ n.latest }}</strong>
          @if (brings()) { <span class="muted">{{ brings() }}</span> }
        </p>
        @if (n.highlights?.length) {
          <ul class="fixes">
            @for (f of n.highlights; track f) { <li>{{ f }}</li> }
            @if (rest(); as more) { <li class="muted">{{ more }}</li> }
          </ul>
        }
        @if (events.readOnly()) {
          <p class="muted">This server is read-only, so it cannot update itself. On the machine it runs on:</p>
          <mv-copy text="multiversectl self-update" />
        } @else {
          <p class="muted">The release is downloaded and checked, both programs are replaced, and the web view restarts. This page reloads when it is back. Running universes are not touched.</p>
        }
        <div class="modal-actions">
          <a class="btn ghost" [href]="n.url" target="_blank" rel="noopener noreferrer">Release notes <mv-icon name="external" /></a>
          <span class="grow"></span>
          <button type="button" class="btn ghost" (click)="updater.hide()">Not now</button>
          @if (!events.readOnly()) {
            <button type="button" class="btn primary" [disabled]="updater.running()" (click)="updater.install()">
              <mv-icon name="arrowDown" /> Update and restart
            </button>
          }
        </div>
      </mv-modal>
    }`,
  styles: `
    .versions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin: 0 0 12px; font-size: 1.1rem }
    .fixes { margin: 0 0 14px; padding-left: 20px } .fixes li { margin-bottom: 4px }`,
})
export class UpdateDialog {
  protected readonly updater = inject(Updater);
  protected readonly events = inject(Events);
  protected readonly brings = computed(() => {
    const n = this.updater.notice();
    if (!n) return '';
    const count = (k: number, one: string, many: string) => `${k}${n.more ? '+' : ''} ${k === 1 && !n.more ? one : many}`;
    return n.fixes ? count(n.fixes, 'fix', 'fixes') : n.changes ? count(n.changes, 'change', 'changes') : '';
  });
  protected readonly rest = computed(() => {
    const n = this.updater.notice();
    if (!n) return null;
    const more = n.fixes - (n.highlights?.length ?? 0);
    return more > 0 ? `and ${more}${n.more ? '+' : ''} more` : n.more ? 'and older ones' : null;
  });
}
