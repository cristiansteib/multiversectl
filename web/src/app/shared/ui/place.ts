import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { LocView } from '../../core/models';
import { webUrlOf } from '../../core/util';
import { Icon } from './icon';

/**
 * Where something lives is drawn the same way everywhere: the platform's mark,
 * its name, then the path on it (github:owner/repo, file:~/path), so the kind
 * of place reads before the name does. A repository on the web is a link to it,
 * in a new tab.
 */
@Component({
  selector: 'mv-place',
  imports: [Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (loc(); as l) {
      @if (href(); as url) {
        <a class="place link" [href]="url" target="_blank" rel="noopener noreferrer"
           [attr.title]="'Open ' + l.short + ' in a new tab'" (click)="$event.stopPropagation()">
          <mv-icon [name]="l.platform" />
          <span class="plat">{{ platform() }}</span><span class="sep">:</span><span class="path">{{ path() }}</span>
          @if (within()) { <span class="within">{{ within() }}</span> }
        </a>
      } @else {
        <span class="place" [attr.title]="l.url || l.short">
          <mv-icon [name]="l.platform" />
          <span class="plat">{{ platform() }}</span><span class="sep">:</span><span class="path">{{ path() }}</span>
          @if (within()) { <span class="within">{{ within() }}</span> }
        </span>
      }
    }`,
  styles: `
    :host { display: inline-flex; min-width: 0; max-width: 100% }
    .place { display: inline-flex; align-items: baseline; gap: 4px; min-width: 0; font-family: var(--mono); font-size: .92em; color: inherit; text-decoration: none }
    .place.link:hover .path { color: var(--accent); text-decoration: underline; text-underline-offset: 3px }
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

  protected readonly href = computed(() => webUrlOf(this.loc()));
}
