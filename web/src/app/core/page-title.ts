import { Injectable, inject } from '@angular/core';
import { Title } from '@angular/platform-browser';
import { RouterStateSnapshot, TitleStrategy } from '@angular/router';

/** The tab says which screen this is: "Universes · multiversectl". */
@Injectable({ providedIn: 'root' })
export class PageTitle extends TitleStrategy {
  private readonly title = inject(Title);
  override updateTitle(state: RouterStateSnapshot): void {
    const t = this.buildTitle(state);
    this.title.setTitle(t ? `${t} · multiversectl` : 'multiversectl');
  }
}
