import { ChangeDetectionStrategy, Component } from '@angular/core';

@Component({
  selector: 'mv-multiverse-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '<h1>Multiverse</h1><p class="lede">Coming up.</p>',
})
export class MultiversePage {}
