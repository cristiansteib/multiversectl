// Everything the chrome does once a page is loaded. Each piece finds its own
// elements and does nothing when they are not there, so the hero page — which
// has no sidenav and no table of contents — runs the same bundle.
import { copyButtons } from './copy';
import { drawer } from './drawer';
import { matrix } from './matrix';
import { tabs } from './tabs';
import { theme } from './theme';
import { toc } from './toc';

document.documentElement.classList.add('js');
copyButtons();
tabs();
drawer();
theme();
matrix();
toc();
