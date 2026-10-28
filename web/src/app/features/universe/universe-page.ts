import { ChangeDetectionStrategy, Component } from '@angular/core';

@Component({
  selector: 'mv-universe-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '<h1>Universe</h1><p class="lede">Coming up.</p>',
})
export class UniversePage {}
