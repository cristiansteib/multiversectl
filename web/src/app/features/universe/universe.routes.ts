import { Routes } from '@angular/router';
import { UniversePage } from './universe-page';

// The page is the header and its side navigation; each section is an address of its own, so a
// link to the code of one universe can be sent to somebody.
export const routes: Routes = [
  {
    path: '',
    component: UniversePage,
    children: [
      { path: '', pathMatch: 'full', title: 'Universe', loadComponent: () => import('./overview-tab').then((m) => m.OverviewTab) },
      { path: 'containers', title: 'Universe containers', loadComponent: () => import('./services-tab').then((m) => m.ServicesTab) },
      { path: 'code', title: 'Universe code', loadComponent: () => import('./code-tab').then((m) => m.CodeTab) },
      { path: 'settings', title: 'Universe settings', loadComponent: () => import('./details-tab').then((m) => m.DetailsTab) },
      { path: 'details', redirectTo: 'settings' },
      { path: 'services', redirectTo: 'containers' },
      { path: '**', redirectTo: '' },
    ],
  },
];
