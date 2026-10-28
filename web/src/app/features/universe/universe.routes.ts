import { Routes } from '@angular/router';
import { UniversePage } from './universe-page';

// The page is the header and the tabs; each tab is an address of its own, so a
// link to the code of one universe can be sent to somebody.
export const routes: Routes = [
  {
    path: '',
    component: UniversePage,
    children: [
      { path: '', pathMatch: 'full', title: 'Universe services', loadComponent: () => import('./services-tab').then((m) => m.ServicesTab) },
      { path: 'code', title: 'Universe code', loadComponent: () => import('./code-tab').then((m) => m.CodeTab) },
      { path: 'details', title: 'Universe details', loadComponent: () => import('./details-tab').then((m) => m.DetailsTab) },
      { path: '**', redirectTo: '' },
    ],
  },
];
