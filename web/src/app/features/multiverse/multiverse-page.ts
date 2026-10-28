import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, input, resource } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { RouterLink } from '@angular/router';
import { Api } from '../../core/api.service';
import { Events } from '../../core/events.service';
import { MultiverseView } from '../../core/models';
import { Ops } from '../../core/ops.service';
import { plural } from '../../core/util';
import { Badge } from '../../shared/ui/badge';
import { CopyButton } from '../../shared/ui/copy-button';
import { EmptyState } from '../../shared/ui/empty-state';
import { ErrorBox } from '../../shared/ui/error-box';
import { Icon } from '../../shared/ui/icon';
import { Place } from '../../shared/ui/place';
import { Skeleton } from '../../shared/ui/skeleton';
import { DoctorButton, DoctorResult } from '../fleet/doctor';
import { MultiverseActions } from '../fleet/multiverse-actions';

/** One installed multiverse: where it lives, how it is reached, what it is made of. */
@Component({
  selector: 'mv-multiverse-page',
  imports: [RouterLink, Badge, CopyButton, EmptyState, ErrorBox, Icon, Place, Skeleton, DoctorButton, DoctorResult, MultiverseActions],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './multiverse-page.html',
  styleUrl: './multiverse-page.css',
})
export class MultiversePage {
  private readonly api = inject(Api);
  private readonly ops = inject(Ops);
  protected readonly events = inject(Events);
  readonly name = input.required<string>();

  protected readonly data = resource({
    params: () => this.name(),
    loader: ({ params }) => this.api.get<MultiverseView>(`multiverses/${encodeURIComponent(params)}`),
  });
  protected readonly universes = computed(() => this.events.fleet()?.multiverses.find((w) => w.name === this.name())?.universes ?? []);
  protected readonly running = computed(() => this.universes().filter((u) => u.running).length);
  protected plural = plural;
  protected entrypoints = (w: MultiverseView): [string, number][] => Object.entries(w.entrypoints ?? {});

  constructor() {
    // A finished update or a new clone changes what this page shows.
    this.ops.finished$.pipe(takeUntilDestroyed(inject(DestroyRef))).subscribe((op) => {
      if (op.multiverse === this.name()) this.data.reload();
    });
  }
}
