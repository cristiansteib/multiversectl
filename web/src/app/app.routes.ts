import { Routes } from '@angular/router';

// Each section is its own lazy chunk and owns its folder under features/.
export const routes: Routes = [
  { path: '', pathMatch: 'full', title: 'Overview', loadComponent: () => import('./features/overview/overview-page').then((m) => m.OverviewPage) },
  { path: 'fleet', title: 'Universes', loadComponent: () => import('./features/fleet/fleet-page').then((m) => m.FleetPage) },
  { path: 'multiverse/:name', title: 'Multiverse', loadComponent: () => import('./features/multiverse/multiverse-page').then((m) => m.MultiversePage) },
  { path: 'universe/:multiverse/:universe', title: 'Universe', loadChildren: () => import('./features/universe/universe.routes').then((m) => m.routes) },
  { path: 'catalog', title: 'Catalog', loadComponent: () => import('./features/catalog/catalog-page').then((m) => m.CatalogPage) },
  { path: 'stats', title: 'Resources', loadComponent: () => import('./features/stats/stats-page').then((m) => m.StatsPage) },
  { path: 'system', title: 'System', loadComponent: () => import('./features/system/system-page').then((m) => m.SystemPage) },
  { path: '**', title: 'Not found', loadComponent: () => import('./features/not-found').then((m) => m.NotFound) },
];
