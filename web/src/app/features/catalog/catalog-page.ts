import { ChangeDetectionStrategy, Component } from '@angular/core';

@Component({
  selector: 'mv-catalog-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '<h1>Catalog</h1><p class="lede">Coming up.</p>',
})
export class CatalogPage {}
