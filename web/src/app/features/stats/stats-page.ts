import { ChangeDetectionStrategy, Component } from '@angular/core';

@Component({
  selector: 'mv-stats-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '<h1>Resources</h1><p class="lede">Coming up.</p>',
})
export class StatsPage {}
