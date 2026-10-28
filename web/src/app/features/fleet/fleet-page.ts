import { ChangeDetectionStrategy, Component } from '@angular/core';

@Component({
  selector: 'mv-fleet-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '<h1>Universes</h1><p class="lede">Coming up.</p>',
})
export class FleetPage {}
