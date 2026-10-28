import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { LocView } from '../../core/models';
import { Icon } from './icon';

/**
 * Where something lives is drawn the same way everywhere: the platform's mark,
 * its name, then the path on it (github:owner/repo, file:~/path), so the kind
 * of place reads before the name does.
 */
@Component({
  selector: 'mv-place',
  imports: [Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (loc(); as l) {
      <span class="place" [attr.title]="l.url || l.short">
        <mv-icon [name]="l.platform" />
        <span class="plat">{{ platform() }}</span><span class="sep">:</span><span class="path">{{ path() }}</span>
        @if (within()) { <span class="within">{{ within() }}</span> }
      </span>
    }`,
  styles: `
    :host { display: inline-flex; min-width: 0; max-width: 100% }
    .place { display: inline-flex; align-items: baseline; gap: 4px; min-width: 0; font-family: var(--mono); font-size: .92em }
    mv-icon { align-self: center; color: var(--muted) }
    .plat, .sep { color: var(--muted) } .path { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0 }
    .within { color: var(--muted); margin-left: 6px }`,
})
export class Place {
  readonly loc = input<LocView | null | undefined>();
  readonly within = input<string>();
  private readonly cut = computed(() => this.loc()?.short.indexOf(':') ?? -1);
  protected readonly platform = computed(() => this.loc()?.short.slice(0, this.cut()) ?? '');
  protected readonly path = computed(() => this.loc()?.short.slice(this.cut() + 1) ?? '');
}
