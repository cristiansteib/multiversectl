import { ChangeDetectionStrategy, Component } from '@angular/core';

@Component({
  selector: 'mv-system-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '<h1>System</h1><p class="lede">Coming up.</p>',
})
export class SystemPage {}
