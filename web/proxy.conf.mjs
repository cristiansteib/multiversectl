// `ng serve` answers the app with hot reload and hands the server's own paths
// to the running multiversectl-server. Sign in once by opening the link the
// server prints: cookies are shared between ports on localhost, so the proxied
// requests are signed in too.
const target = process.env.MULTIVERSECTL_SERVER ?? 'http://127.0.0.1:7777';

// The server refuses an Origin that is not itself (a page elsewhere must not be
// able to talk to it), so the proxy presents the server's own.
const common = { target, secure: false, headers: { Origin: target } };

export default {
  '/api': { ...common },
  '/ws': { ...common, ws: true },
};
