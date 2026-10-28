import { ChangeDetectionStrategy, Component } from '@angular/core';

@Component({
  selector: 'mv-overview-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '<h1>Overview</h1><p class="lede">Coming up.</p>',
})
export class OverviewPage {}
