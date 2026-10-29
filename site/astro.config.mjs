// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

// The site this replaces is a folder of plain .html files, and its URLs are
// already published: /start/install.html, /concepts/networking.html. build
// format "file" keeps exactly those paths, so nothing needs a redirect and the
// relative links written inside the content ("../tasks/server.html") keep
// resolving the way they always did.
export default defineConfig({
  site: 'https://multiversectl.dev',
  output: 'static',
  trailingSlash: 'never',
  build: {
    format: 'file',
    // One stylesheet for the whole site, under a content hash, which is what
    // docgen's ?v= query did by hand.
    inlineStylesheets: 'never',
  },
  integrations: [
    sitemap({
      // The 404 is not a page to index.
      filter: (url) => !/\/404(\.html)?$/.test(url),
      // The integration lists pages as clean paths; this site publishes them
      // under build.format 'file', so each address gets its .html back (the
      // root stays the root).
      serialize: (item) => {
        const u = new URL(item.url);
        if (u.pathname !== '/' && !u.pathname.endsWith('.html')) u.pathname += '.html';
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
