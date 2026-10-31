// The release the site was built from, so a reader knows which docs they are
// reading. The release workflow builds the site after it tags, and passes
// `git describe --tags` as SITE_VERSION: "v0.0.96" for the release itself, or
// "v0.0.95-2-gabc1234" for a build from commits after it. A build without it,
// on a laptop, is "dev".

const repo = 'https://github.com/cristiansteib/multiversectl';

export interface SiteVersion {
  /** What the top bar shows: "v0.0.96", "v0.0.95+2" or "dev". */
  label: string;
  /** Says it in words, for the tooltip. */
  title: string;
  /** The release's notes, or what changed since it; none for "dev". */
  url?: string;
}

export function siteVersion(described = process.env.SITE_VERSION ?? ''): SiteVersion {
  const ahead = described.match(/^(v\d+\.\d+\.\d+)-(\d+)-g([0-9a-f]+)$/);
  if (ahead) {
    const [, tag, n, sha] = ahead;
    return {
      label: `${tag}+${n}`,
      title: `${n} commit${n === '1' ? '' : 's'} after ${tag}`,
      url: `${repo}/compare/${tag}...${sha}`,
    };
  }
  if (/^v\d+\.\d+\.\d+$/.test(described)) {
    return { label: described, title: `The documentation of ${described}`, url: `${repo}/releases/tag/${described}` };
  }
  return { label: 'dev', title: 'Built from a working copy, not from a release' };
}
