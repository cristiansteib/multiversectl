import { ChangeDetectionStrategy, Component } from '@angular/core';
import { Shell } from './layout/shell';

@Component({
  selector: 'app-root',
  imports: [Shell],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '<mv-shell />',
})
export class App {}
