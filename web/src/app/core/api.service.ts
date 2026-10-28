import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, firstValueFrom } from 'rxjs';

/** A failed request: the server's own sentence, and what came with it. */
export class ApiError extends Error {
  constructor(message: string, readonly status: number, readonly body: Record<string, unknown>) {
    super(message);
  }
}

export type Query = Record<string, string | number | boolean | undefined>;

/**
 * Every request to the server goes through here. A write carries the header the
 * server asks for on anything that is not a read: a page on another site cannot
 * send it, and the session cookie is SameSite=Strict besides.
 */
@Injectable({ providedIn: 'root' })
export class Api {
  private readonly http = inject(HttpClient);

  get<T>(path: string, query: Query = {}): Promise<T> {
    const params: Record<string, string> = {};
    for (const [k, v] of Object.entries(query)) if (v !== undefined && v !== false) params[k] = String(v === true ? 1 : v);
    return this.send(this.http.get<T>('/api/' + path, { params }));
  }

  post<T>(path: string, payload: unknown): Promise<T> {
    return this.send(this.http.post<T>('/api/' + path, payload, { headers: { 'X-Mvctl': '1' } }));
  }

  private async send<T>(req: Observable<T>): Promise<T> {
    try {
      return await firstValueFrom(req);
    } catch (e) {
      if (e instanceof HttpErrorResponse) {
        const body = (typeof e.error === 'object' && e.error ? e.error : {}) as Record<string, unknown>;
        throw new ApiError(String(body['error'] ?? e.statusText ?? 'request failed'), e.status, body);
      }
      throw e;
    }
  }
}
