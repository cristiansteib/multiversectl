import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { Events } from '../core/events.service';
import { Updater } from '../core/updater.service';
import { brings } from '../core/update-notice';
import { CopyButton } from '../shared/ui/copy-button';
import { Icon } from '../shared/ui/icon';

/**
 * A newer release, on every page, until it is answered: update now (one press),
 * what it brings, remind me later, or skip this version. While it installs it says
 * so; the page reloads by itself when the new version answers.
 */
@Component({
  selector: 'mv-update-banner',
  imports: [CopyButton, Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (updater.running()) {
      <div class="update" role="status">
        <span class="spin" aria-hidden="true"></span>
        <strong>Updating multiversectl{{ updater.notice() ? ' to ' + updater.notice()!.latest : '' }}…</strong>
        <span class="muted">The web view restarts, and this page reloads when it is back. Running universes are not touched.</span>
      </div>
    } @else if (updater.bannerVisible() && updater.notice(); as n) {
      <div class="update" role="status">
        <strong>Update available</strong>
        <span>multiversectl {{ n.latest }} is out; you are on {{ n.current }}@if (summary()) {, {{ summary() }} since}.</span>
        <span class="grow"></span>
        @if (events.readOnly()) {
          <mv-copy text="multiversectl self-update" />
        } @else {
          <button type="button" class="btn small primary" (click)="updater.install()"><mv-icon name="arrowDown" /> Update now</button>
        }
        <button type="button" class="btn small ghost" (click)="updater.show()">What's new</button>
        <button type="button" class="btn small ghost" (click)="updater.later()" title="Hide this for a day">Remind me later</button>
        <button type="button" class="btn small ghost" (click)="updater.skip()" title="Do not show this version again; a newer one will be shown">Skip this version</button>
      </div>
    }`,
  styles: `
    .update { display: flex; align-items: center; gap: 8px 12px; flex-wrap: wrap; padding: 9px 20px; background: var(--info-bg); border-bottom: 1px solid var(--info) }`,
})
export class UpdateBanner {
  protected readonly updater = inject(Updater);
  protected readonly events = inject(Events);
  protected readonly summary = computed(() => { const n = this.updater.notice(); return n ? brings(n) : ''; });
}
