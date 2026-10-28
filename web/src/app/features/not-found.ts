import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink } from '@angular/router';
import { EmptyState } from '../shared/ui/empty-state';

@Component({
  selector: 'mv-not-found',
  imports: [EmptyState, RouterLink],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<mv-empty title="There is nothing at this address">It may have been removed. <a routerLink="/">Back to the overview</a>.</mv-empty>`,
})
export class NotFound {}
