import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import { ToolVersion } from '../../core/models';
import { Badge } from '../../shared/ui/badge';
import { Icon } from '../../shared/ui/icon';

/**
 * The tools the command line drives. A tool that is found says what it is for;
 * one that is missing says why, which is the one thing a person came here to learn.
 */
@Component({
  selector: 'mv-tools-table',
  imports: [Badge, Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="table-wrap">
      <table>
        <thead><tr><th>Tool</th><th>Status</th><th>Used for</th><th>Found at</th></tr></thead>
        <tbody>
          @for (t of tools(); track t.name) {
            <tr [class.missing]="!t.ok">
              <th scope="row" class="name">{{ t.name }}</th>
              <td>
                @if (t.ok) { <span class="mono">{{ t.version }}</span> }
                @else { <mv-badge tone="warn"><mv-icon name="warn" /> Missing</mv-badge> }
              </td>
              <td [class.why]="!t.ok" [class.muted]="t.ok">{{ t.note || (t.ok ? '' : 'No reason was given.') }}</td>
              <td class="mono path">{{ t.path || '—' }}</td>
            </tr>
          }
        </tbody>
      </table>
    </div>`,
  styles: `
    .name { color: var(--text); font-weight: 600; font-size: .92rem }
    .why { color: var(--warn); overflow-wrap: anywhere } .path { overflow-wrap: anywhere; color: var(--muted) }
    .missing { background: color-mix(in srgb, var(--warn-bg) 45%, transparent) }`,
})
export class ToolsTable {
  readonly tools = input.required<ToolVersion[]>();
}
