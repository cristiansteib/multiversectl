import { Pipe, PipeTransform } from '@angular/core';
import { ago } from '../../core/util';

/** "3 h ago": how long it has been, in the one way every screen says it. */
@Pipe({ name: 'ago' })
export class AgoPipe implements PipeTransform {
  transform(iso: string | undefined | null): string { return ago(iso ?? undefined); }
}
