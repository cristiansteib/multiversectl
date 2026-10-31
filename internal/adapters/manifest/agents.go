package manifest

import (
	"fmt"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// Agents writes the AGENTS.md of a multiverse's repository. It is short on
// purpose: it says what the repository is, which commands matter, and the few
// rules that keep a shared definition from becoming one person's machine.
func (Writer) Agents(d app.ProjectDraft, source string) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# AGENTS.md\n\n")
	fmt.Fprintf(&b, "This repository defines the **%s** multiverse for [multiversectl](https://github.com/cristiansteib/multiversectl): which repositories it is made of, and how to run isolated copies of it (*universes*) side by side. It holds no application code.\n\n", d.Name)

	b.WriteString("## Files\n\n")
	b.WriteString("- `multiverse.yaml`: the multiverse. `spec.repos` lists the repositories; each one with a `docker-compose.yml` runs as an app of the same name; one with none is only checked out. Add a `spec.apps` section only to say more than that.\n")
	b.WriteString("- More multiverses can live beside it, one file each in `multiverse/`; `multiversectl start owner/repo:name` picks one.\n\n")

	b.WriteString("## Repositories\n\n")
	for _, r := range d.Repos {
		fmt.Fprintf(&b, "- `%s`: %s\n", r.Key, r.URL)
	}

	b.WriteString("\n## Working with it\n\n```sh\n")
	fmt.Fprintf(&b, "multiversectl start %s   # install it, make a universe and run it\n", source)
	b.WriteString("multiversectl universe new fix/login      # a worktree of every repository, on branch fix/login\n")
	b.WriteString("multiversectl universe up fix/login       # start it\n")
	b.WriteString("multiversectl status                      # what is installed and running\n")
	b.WriteString("multiversectl universe urls fix/login     # the addresses it answers on\n")
	b.WriteString("multiversectl universe rm fix/login       # remove it: containers, volumes and worktrees\n")
	b.WriteString("multiversectl doctor                      # check the machine and the multiverse\n")
	b.WriteString("multiversectl catalog sync                # after this repository changes\n")
	b.WriteString("```\n\n`multiversectl --help` lists everything; a command missing an argument asks for it.\n\n")

	b.WriteString("## Conventions\n\n")
	b.WriteString("- Change the multiverse here, in `multiverse.yaml`, then commit and push; run `multiversectl catalog sync` on each machine. Do not edit an installed copy.\n")
	b.WriteString("- Keep machine-specific values out: no absolute `spec.root`, no secrets, no local paths. Where things live on a machine is chosen when installing.\n")
	b.WriteString("- A universe is isolated: work for one change goes in its own universe, and `universe rm` discards uncommitted work in its worktrees, so commit first.\n")
	b.WriteString("- Name a universe after the work it is for (`universe new fix/login`): every repository is then on that branch, and the next person sees what it is without asking. A bare number (`universe new`) is for a try that gets thrown away; set `spec.branch` to have numbered universes follow a pattern of your own.\n")
	b.WriteString("- To add a repository, add it to `spec.repos`, push, and run `catalog sync`.\n")
	return []byte(b.String()), nil
}
