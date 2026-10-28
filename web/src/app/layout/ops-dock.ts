import { ChangeDetectionStrategy, Component, ElementRef, effect, inject, viewChild } from '@angular/core';
import { Ops } from '../core/ops.service';
import { Badge, Tone } from '../shared/ui/badge';
import { Icon } from '../shared/ui/icon';

const STATE: Record<string, [Tone, string]> = { running: ['info', 'Running'], done: ['ok', 'Done'], failed: ['bad', 'Failed'] };

/** What the page started, at the foot of it: one line while it runs, its output when asked for. */
@Component({
  selector: 'mv-ops-dock',
  imports: [Badge, Icon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './ops-dock.html',
  styleUrl: './ops-dock.css',
})
export class OpsDock {
  protected readonly ops = inject(Ops);
  private readonly log = viewChild<ElementRef<HTMLElement>>('log');
  protected readonly state = (s: string): [Tone, string] => STATE[s] ?? ['mute', s];

  constructor() {
    // Follow the output as it arrives, unless the person has scrolled up to read.
    effect(() => {
      this.ops.shown()?.lines.length;
      const el = this.log()?.nativeElement;
      if (el && el.scrollTop + el.clientHeight >= el.scrollHeight - 40) queueMicrotask(() => (el.scrollTop = el.scrollHeight));
    });
    effect(() => {
      if (this.ops.open()) queueMicrotask(() => { const el = this.log()?.nativeElement; if (el) el.scrollTop = el.scrollHeight; });
    });
  }
}
