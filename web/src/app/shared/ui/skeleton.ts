import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/** The shape of what is coming, while it is read: a layout that holds is a layout that does not jump. */
@Component({
  selector: 'mv-skeleton',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<span class="skel" [style.width]="width()" [style.height]="height()" aria-hidden="true"></span><span class="sr-only">Loading</span>`,
  styles: `:host { display: block }`,
})
export class Skeleton {
  readonly width = input('100%');
  readonly height = input('1em');
}
