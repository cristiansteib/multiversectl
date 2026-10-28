import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { LocView } from '../../core/models';
import { branchUrlOf, webUrlOf } from '../../core/util';
import { Icon } from './icon';

/**
 * A repository, as its path without the host (owner/repo), in the colour of the
 * platform it is on; a link to it when it is on the web. With a branch it is that
 * branch's page: the chip is the repository, or one of its branches.
 */
@Component({
  selector: 'mv-repo-chip',
  imports: [Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (href(); as url) {
      <a class="chip" [class]="kind()" [href]="url" target="_blank" rel="noopener noreferrer" [attr.title]="title()">
        <mv-icon [name]="icon()" /><span class="txt">{{ text() }}</span>
      </a>
    } @else {
      <span class="chip" [class]="kind()" [attr.title]="title()"><mv-icon [name]="icon()" /><span class="txt">{{ text() }}</span></span>
    }`,
  styles: `
    :host { display: inline-flex; min-width: 0; max-width: 100% }
    .chip { display: inline-flex; align-items: center; gap: 6px; min-width: 0; max-width: 100%; padding: 3px 10px; border: 1px solid var(--line); border-radius: 999px;
      background: var(--panel); color: var(--text); font-size: .84rem; text-decoration: none }
    a.chip:hover { border-color: var(--accent) }
    .txt { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0 }
    .repo.github mv-icon { color: var(--text) } .repo.gitlab mv-icon { color: #fc6d26 } .repo.git mv-icon, .repo.file mv-icon { color: var(--info) }
    .branch mv-icon { color: var(--s7) } .branch .txt { font-family: var(--mono); font-size: .92em }`,
})
export class RepoChip {
  /** Where the repository lives, as the server worked it out. */
  readonly loc = input<LocView | null | undefined>();
  /** Shown when the place is not known (yet). */
  readonly name = input<string>('');
  /** A branch of it: the chip is then the branch, linked to its page. */
  readonly branch = input<string>('');

  protected readonly isBranch = computed(() => !!this.branch());
  protected readonly kind = computed(() => (this.isBranch() ? 'branch' : 'repo ' + (this.loc()?.platform ?? 'git')));
  protected readonly icon = computed(() => (this.isBranch() ? 'branch' : (this.loc()?.platform ?? 'file')));
  protected readonly text = computed(() => (this.isBranch() ? this.branch() : (this.loc()?.path?.replace(/\.git$/, '') || this.name())));
  protected readonly href = computed(() => (this.isBranch() ? branchUrlOf(this.loc(), this.branch()) : webUrlOf(this.loc())));
  protected readonly title = computed(() => {
    const what = this.isBranch() ? `Branch ${this.branch()} of ${this.loc()?.short ?? this.name()}` : `Repository ${this.loc()?.short ?? this.name()}`;
    return this.href() ? `${what} · Opens in a new tab` : what;
  });
}
