#!/usr/bin/env node
// multiversectl is a Go binary, distributed through GitHub releases. This
// package holds the name on npm and points at the real installers rather
// than pretending to be one: downloading a binary from a postinstall script
// is the kind of thing that should be a deliberate choice, not a side effect
// of installing a package.
const lines = [
  "multiversectl is distributed as a single binary, not as an npm package.",
  "",
  "  curl -fsSL https://raw.githubusercontent.com/cristiansteib/multiversectl/main/install.sh | sh",
  "  brew install cristiansteib/tap/multiversectl",
  "  mise use -g ubi:cristiansteib/multiversectl",
  "",
  "Releases:      https://github.com/cristiansteib/multiversectl/releases",
  "Documentation: https://cristiansteib.github.io/multiversectl/",
];
console.error(lines.join("\n"));
process.exit(1);
