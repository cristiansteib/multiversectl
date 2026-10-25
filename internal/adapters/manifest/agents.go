package manifest

import (
	"fmt"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// Agents writes the AGENTS.md of a project's repository. It is short on
// purpose: it says what the repository is, which commands matter, and the few
// rules that keep a shared definition from becoming one person's machine.
func (Writer) Agents(d app.ProjectDraft, source string) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# AGENTS.md\n\n")
	fmt.Fprintf(&b, "This repository defines the **%s** project for [multiversectl](https://github.com/cristiansteib/multiversectl): which repositories it is made of, and how to run numbered, isolated copies of it (*universes*) side by side. It holds no application code.\n\n", d.Name)

	b.WriteString("## Files\n\n")
	b.WriteString("- `multiverse.yaml`: the workspace. `spec.repos` lists the repositories; each one with a `docker-compose.yml` runs as an app of the same name, and `runtime: none` marks one that has none. Add a `spec.apps` section only to say more than that.\n")
	b.WriteString("- `catalog.yaml`: makes this repository an index that lists the workspace above, so `catalog install` and `catalog add` can find it. Nest more catalogs here to group other projects.\n\n")

	b.WriteString("## Repositories\n\n")
	for _, r := range d.Repos {
		fmt.Fprintf(&b, "- `%s`: %s\n", r.Key, r.URL)
	}

	b.WriteString("\n## Working with it\n\n```sh\n")
	fmt.Fprintf(&b, "multiversectl catalog install %s   # put the project on this machine and clone every repository\n", source)
	b.WriteString("multiversectl universe new 1              # a worktree of every repository, on branch u1\n")
	b.WriteString("multiversectl universe up 1               # start it\n")
	b.WriteString("multiversectl universe status             # what is running\n")
	b.WriteString("multiversectl universe urls 1             # the addresses it answers on\n")
	b.WriteString("multiversectl universe rm 1               # remove it: containers, volumes and worktrees\n")
	b.WriteString("multiversectl universe doctor                      # check the host and the workspace\n")
	b.WriteString("multiversectl catalog update              # after this repository changes\n")
	b.WriteString("```\n\n`multiversectl --help` lists everything; a command missing an argument asks for it.\n\n")

	b.WriteString("## Conventions\n\n")
	b.WriteString("- Change the project here, in `multiverse.yaml` and `catalog.yaml`, then commit and push; run `multiversectl catalog update` on each machine. Do not edit an installed copy.\n")
	b.WriteString("- Keep machine-specific values out: no absolute `spec.root`, no secrets, no local paths. Where things live on a machine is chosen when installing.\n")
	b.WriteString("- A universe is isolated: work for one change goes in its own universe, and `universe rm` discards uncommitted work in its worktrees, so commit first.\n")
	b.WriteString("- To add a repository, add it to `spec.repos`, push, and run `catalog update`.\n")
	return []byte(b.String()), nil
}
