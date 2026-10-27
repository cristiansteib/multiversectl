package cli

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// skillText is the skill an agent reads, kept next to the commands it describes.
//
//go:embed skill/SKILL.md
var skillText string

func (r runner) skills() *app.Skills { return r.f.Skills(r.out) }

// refreshSkills keeps an installed skill in step with this binary, like the
// completions. It skips the skill commands, which are about exactly that.
func (r runner) refreshSkills(cmd *cobra.Command) {
	if r.f.Skills == nil {
		return
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "skill" {
			return
		}
	}
	r.skills().Refresh(skillText)
}

// agentsCmd groups what multiversectl offers coding agents. The skill is the first;
// more go beside it.
func (r runner) agentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Tools for coding agents",
		Long: "What multiversectl offers a coding agent, so it can work in a universe of its own.\n" +
			"For reading the tool itself, every command takes --json (and --help --json).",
		Example: "  multiversectl agents skill install\n  multiversectl agents skill status",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(r.skillCmd())
	return cmd
}

func (r runner) skillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the agent skill that teaches a coding agent to work in universes",
		Long: "A skill is a short document a coding agent loads when the task fits it. This one\n" +
			"says how to find its way (--json), to work in a universe of its own and what\n" +
			"never to do without asking. It records the version that wrote it, and any later\n" +
			"run of multiversectl rewrites an installed copy written by another version, so an\n" +
			"upgrade does not leave an agent reading old advice.",
		Example: "  multiversectl agents skill install              # for you, in every folder\n" +
			"  multiversectl agents skill install --here    # only in this folder's repository\n" +
			"  multiversectl agents skill status\n  multiversectl agents skill show",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	var here bool
	var dir string
	where := func() (string, error) {
		switch {
		case dir != "":
			return filepath.Abs(dir)
		case here:
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			return filepath.Join(cwd, ".claude", "skills"), nil
		}
		return r.skills().GlobalDir()
	}
	flags := func(c *cobra.Command) *cobra.Command {
		c.Flags().BoolVar(&here, "here", false, "the .claude/skills of the current folder, not your home")
		c.Flags().StringVar(&dir, "dir", "", "a folder of skills to use instead (its multiversectl/SKILL.md is written)")
		return c
	}
	cmd.AddCommand(
		flags(&cobra.Command{
			Use:   "install",
			Short: "Write the skill for Claude Code, in your home or in this repository",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				d, err := where()
				if err != nil {
					return err
				}
				return r.skills().Install(d, skillText)
			},
		}),
		flags(&cobra.Command{
			Use:   "uninstall",
			Short: "Remove the skill this tool wrote",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				d, err := where()
				if err != nil {
					return err
				}
				return r.skills().Uninstall(d)
			},
		}),
		&cobra.Command{
			Use:   "status",
			Short: "Say where the skill is installed and whether it is current",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				list := r.skills().Status()
				if len(list) == 0 {
					r.out.Info("not installed: `multiversectl agents skill install`")
					return nil
				}
				for _, i := range list {
					switch {
					case !i.Ours:
						r.out.Warn("%s: not written by multiversectl; left alone", shortPath(i.File))
					case i.Current(r.f.Version):
						r.out.OK("%s: %s, current", shortPath(i.File), i.Version)
					default:
						r.out.Warn("%s: %s, this is %s; any command of this version refreshes it", shortPath(i.File), i.Version, r.f.Version)
					}
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "show",
			Short: "Print the skill",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				_, err := fmt.Fprint(cmd.OutOrStdout(), strings.ReplaceAll(skillText, "{{version}}", r.f.Version))
				return err
			},
		},
	)
	return cmd
}
