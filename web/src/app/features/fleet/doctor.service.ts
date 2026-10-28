import { Injectable, inject, signal } from '@angular/core';
import { Api } from '../../core/api.service';
import { DoctorView } from '../../core/models';

export interface DoctorEntry { state: 'checking' | 'done' | 'error'; report?: DoctorView; error?: string }

/**
 * What a doctor run said, kept by what it ran on: the fleet is redrawn whenever
 * something changes, and a result should not vanish with it.
 */
@Injectable({ providedIn: 'root' })
export class Doctor {
  private readonly api = inject(Api);
  readonly results = signal<Record<string, DoctorEntry>>({});

  key = (multiverse: string, universe = ''): string => `${multiverse}/${universe}`;

  async run(multiverse: string, universe = ''): Promise<void> {
    const k = this.key(multiverse, universe);
    this.set(k, { state: 'checking' });
    try {
      const report = await this.api.get<DoctorView>('doctor', universe ? { multiverse, universe } : { multiverse });
      this.set(k, { state: 'done', report });
    } catch (e) {
      this.set(k, { state: 'error', error: (e as Error).message });
    }
  }

  dismiss(multiverse: string, universe = ''): void {
    const k = this.key(multiverse, universe);
    this.results.update(({ [k]: _drop, ...rest }) => rest);
  }

  private set(k: string, e: DoctorEntry): void { this.results.update((r) => ({ ...r, [k]: e })); }
}
