// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

// The documentation lives under /docs/ and every address is extensionless:
// /docs/start/install/, /docs/concepts/networking/. The landing page keeps the
// root. build format "directory" writes each page as <path>/index.html, which
// is what lets GitHub Pages serve a path with no extension.
//
// The addresses this replaces (/start/install.html and the rest) are published
// and indexed, so scripts/redirects.mjs writes a stub at every one of them
// pointing here. Nothing links to a stub; they exist for what is already out
// there.
export default defineConfig({
  site: 'https://multiversectl.dev',
  output: 'static',
  trailingSlash: 'always',
  build: {
    format: 'directory',
    // One stylesheet for the whole site, under a content hash, which is what
    // docgen's ?v= query did by hand.
    inlineStylesheets: 'never',
  },
  integrations: [
    sitemap({
      // The 404 is not a page to index.
      filter: (url) => !/\/404\/?$/.test(url),
      // Every address ends in a slash, which is what the canonical says and
      // what GitHub Pages serves; without this the sitemap would list the
      // extensionless form that only 301s to it.
      serialize: (item) => {
        const u = new URL(item.url);
        if (!u.pathname.endsWith('/')) u.pathname += '/';
        return { ...item, url: u.href };
      },
    }),
  ],
  devToolbar: { enabled: false },
  vite: {
    build: {
      // Keep the shared chrome script a file of its own instead of inlining a
      // copy into every page: the HTML is served must-revalidate and the
      // hashed asset immutable, so one request is cached across the whole site.
      assetsInlineLimit: 0,
    },
  },
});
