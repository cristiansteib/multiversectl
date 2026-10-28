package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Colours that read on a light or a dark terminal. Meaning is never in colour alone:
// every state also has its own mark.
var (
	cAccent = lipgloss.AdaptiveColor{Light: "#087a66", Dark: "#4ed8b8"}
	cOK     = lipgloss.AdaptiveColor{Light: "#17794a", Dark: "#55d28f"}
	cWarn   = lipgloss.AdaptiveColor{Light: "#955600", Dark: "#f1b04e"}
	cBad    = lipgloss.AdaptiveColor{Light: "#b3302a", Dark: "#ff7d74"}
	cMute   = lipgloss.AdaptiveColor{Light: "#6b7785", Dark: "#8e9aa9"}

	sMute   = lipgloss.NewStyle().Foreground(cMute)
	sOK     = lipgloss.NewStyle().Foreground(cOK)
	sWarn   = lipgloss.NewStyle().Foreground(cWarn)
	sBad    = lipgloss.NewStyle().Foreground(cBad)
	sAccent = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sBold   = lipgloss.NewStyle().Bold(true)
	sRow    = lipgloss.NewStyle().Reverse(true) // the row the cursor is on
)

func (m manageModel) View() string {
	if m.width < 50 || m.height < 10 {
		return "The window is too small for this: make it at least 50 by 10.\n"
	}
	var b strings.Builder
	b.WriteString(m.header() + "\n")
	b.WriteString(sMute.Render(strings.Repeat("─", m.width)) + "\n")

	switch {
	case !m.loaded && m.errMsg == "":
		b.WriteString(m.pad("Reading the catalog…", m.listHeight()))
	case m.mode == modeReview:
		b.WriteString(m.pad(m.reviewBox(), m.listHeight()))
	case m.mode == modeRemove:
		b.WriteString(m.pad(m.box("Remove from the catalog?", []string{
			sBold.Render(m.removing),
			"",
			"This edits the catalog (a shared repository): the entry is taken out, committed and pushed.",
			"What it points at is untouched, and a multiverse installed from it stays installed here.",
		}, "y remove · esc back"), m.listHeight()))
	case m.mode == modeQuit:
		b.WriteString(m.pad(m.box("Quit?", []string{
			fmt.Sprintf("%d staged change(s) have not been made and will be lost.", m.plan().count()),
		}, "y quit · esc back"), m.listHeight()))
	case m.mode == modeHelp:
		b.WriteString(m.pad(m.helpBox(), m.listHeight()))
	default:
		b.WriteString(m.list())
	}

	b.WriteString(sMute.Render(strings.Repeat("─", m.width)) + "\n")
	b.WriteString(m.statusLine() + "\n")
	b.WriteString(m.keys())
	return b.String()
}

// header names the catalog and counts what is in it.
func (m manageModel) header() string {
	title := sAccent.Render(" Catalog")
	if !m.loaded {
		return title
	}
	var multiverses, have, left int
	var walk func(n domain.Node)
	walk = func(n domain.Node) {
		if n.Ignored {
			left++
			return
		}
		if n.Kind == domain.MultiverseNode {
			multiverses++
			if m.installedAlready(n) {
				have++
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(m.tree)
	counts := sMute.Render(fmt.Sprintf("%d multiverses · %d installed · %d left out", multiverses, have, left))
	if m.mode == modeFilter || m.filter.Value() != "" {
		return title + "  " + m.filter.View()
	}
	gap := max(2, m.width-lipgloss.Width(title)-lipgloss.Width(counts)-1)
	return title + strings.Repeat(" ", gap) + counts
}

// list is the tree, scrolled so the cursor is in it.
func (m manageModel) list() string {
	rows := m.rows()
	h := m.listHeight()
	if len(rows) == 0 {
		msg := "The catalog is empty. Press a to add an entry."
		if m.filter.Value() != "" {
			msg = "Nothing matches the filter. Esc clears it."
		}
		return m.pad(sMute.Render("  "+msg), h)
	}
	end := min(len(rows), m.offset+h)
	var lines []string
	for i := m.offset; i < end; i++ {
		lines = append(lines, m.rowLine(rows[i], i == m.cursor))
	}
	return m.pad(strings.Join(lines, "\n"), h)
}

// rowLine is one node: the cursor, its checkbox, the tree, what it is and where it lives,
// and on the right what is staged on it.
func (m manageModel) rowLine(r viewRow, on bool) string {
	n := r.node
	name := n.Name
	kind := string(n.Kind)
	if n.Path == "" {
		kind = "root"
	}
	fold := "  "
	if r.expand {
		fold = "▾ "
		if r.collapsed {
			fold = "▸ "
		}
	}
	left := fmt.Sprintf("%s%s%s%s  %s", m.box3(n), " ", sMute.Render(r.prefix), fold+name, sMute.Render(kind))
	if loc := shortRepoName(n.Source.Repo); loc != "" {
		left += "  " + sMute.Render(loc)
	}
	tag := m.tag(n)
	pad := m.width - 2 - lipgloss.Width(left) - lipgloss.Width(tag)
	line := left + strings.Repeat(" ", max(1, pad)) + tag
	cursor := "  "
	if on {
		cursor = sAccent.Render("› ")
	}
	line = cursor + line
	line = xansi.Truncate(line, m.width, "…")
	if on {
		// The whole line, so the row is easy to follow with the eye.
		return sRow.Render(xansi.Strip(line) + strings.Repeat(" ", max(0, m.width-lipgloss.Width(xansi.Strip(line)))))
	}
	return line
}

// box3 is the checkbox: installed, not, or some of what is under a catalog, with what
// is staged shown in another mark and colour; left out and unreadable have their own.
func (m manageModel) box3(n domain.Node) string {
	switch {
	case n.Unavailable != "":
		return sBad.Render("[!]")
	case m.isIgnored(n):
		if m.staged[n.Path] == ignore {
			return sWarn.Render("[⊘]")
		}
		return sMute.Render("[⊘]")
	}
	if n.Kind == domain.MultiverseNode {
		switch m.staged[n.Path] {
		case install:
			return sWarn.Render("[+]")
		case uninstall:
			return sWarn.Render("[−]")
		}
		if m.installedAlready(n) {
			return sOK.Render("[✓]")
		}
		return "[ ]"
	}
	pending := false
	for _, l := range n.Leaves() {
		if k := m.staged[l.Path]; k == install || k == uninstall {
			pending = true
		}
	}
	mark := map[string]string{"all": "✓", "some": "~", "none": " "}[m.installState(n)]
	switch {
	case pending:
		return sWarn.Render("[" + mark + "]")
	case mark == "✓":
		return sOK.Render("[✓]")
	}
	return "[" + mark + "]"
}

// tag says, on the right, what is staged on a node or why it cannot be changed.
func (m manageModel) tag(n domain.Node) string {
	switch m.staged[n.Path] {
	case install:
		return sWarn.Render("+ install")
	case uninstall:
		return sWarn.Render("− uninstall")
	case ignore:
		return sWarn.Render("⊘ leave out")
	case enable:
		return sWarn.Render("● bring back")
	}
	switch {
	case n.Unavailable != "":
		return sBad.Render("✗ " + xansi.Truncate(n.Unavailable, 34, "…"))
	case n.Ignored:
		return sMute.Render("left out on this machine")
	case n.Stale != "":
		return sWarn.Render("⚠ not refreshed")
	}
	if n.Kind == domain.CatalogNode && n.Path != "" {
		ls := m.leaves(n)
		have := 0
		for _, l := range ls {
			if m.isInstalled(l) {
				have++
			}
		}
		if len(ls) > 0 {
			return sMute.Render(fmt.Sprintf("%d/%d installed", have, len(ls)))
		}
	}
	return ""
}

// statusLine is what is staged, or the last thing worth saying, or an error.
func (m manageModel) statusLine() string {
	switch {
	case m.errMsg != "":
		return " " + sBad.Render("✗ "+xansi.Truncate(m.errMsg, m.width-4, "…"))
	case !m.plan().empty():
		p := m.plan()
		var parts []string
		for _, x := range []struct {
			n    int
			word string
		}{{len(p.Install), "install"}, {len(p.Uninstall), "uninstall"}, {len(p.Ignore), "leave out"}, {len(p.Enable), "bring back"}} {
			if x.n > 0 {
				parts = append(parts, fmt.Sprintf("%s %d", x.word, x.n))
			}
		}
		return " " + sWarn.Render("Staged: "+strings.Join(parts, " · ")) + sMute.Render("   enter to review and make them, esc to discard")
	case m.status != "":
		return " " + sMute.Render(m.status)
	}
	return " " + sMute.Render(xansi.Truncate("Nothing staged: toggle with space and e; nothing changes until you review it.", m.width-2, "…"))
}

// keys is the line of what can be pressed: all of it where there is room, and what
// matters most where there is not.
func (m manageModel) keys() string {
	items := []string{"↑↓ move", "←→ fold", "space install/uninstall", "e leave out/bring back", "r remove entry", "/ filter", "a add", "u update", "enter review", "? help", "q quit"}
	if m.width < 125 {
		items = []string{"↑↓ move", "space install", "e leave out", "r remove", "/ filter", "enter review", "? help", "q quit"}
	}
	if m.width < 80 {
		items = []string{"↑↓ move", "space install", "e leave out", "enter review", "? help"}
	}
	if m.mode == modeFilter {
		items = []string{"type to filter", "enter keep", "esc clear"}
	}
	return sMute.Render(xansi.Truncate(" "+strings.Join(items, " · "), m.width, "…"))
}

// ── overlays ───────────────────────────────────────────────────────────

func (m manageModel) box(title string, body []string, keys string) string {
	inner := append([]string{sAccent.Render(title), ""}, body...)
	inner = append(inner, "", sMute.Render(keys))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(0, 2).Margin(1, 2).
		Width(min(m.width-6, 100)).Render(strings.Join(inner, "\n"))
}

// reviewBox lists what will be done and the command that does each, so a
// person sees exactly what is about to happen and can do it by hand next time.
func (m manageModel) reviewBox() string {
	p := m.plan()
	var lines []string
	add := func(sym string, st lipgloss.Style, word string, items []planItem, cmd func(planItem) string) {
		for _, it := range items {
			lines = append(lines, fmt.Sprintf("%s %-10s %-20s %s", st.Render(sym), word, xansi.Truncate(it.Name, 20, "…"), sMute.Render(cmd(it))))
		}
	}
	add("●", sOK, "bring back", p.Enable, func(i planItem) string { return "multiversectl catalog unignore " + i.Path })
	add("+", sOK, "install", p.Install, func(i planItem) string { return "multiversectl start " + i.Path + " --install-only" })
	add("−", sWarn, "uninstall", p.Uninstall, func(i planItem) string { return "multiversectl system forget " + i.Name })
	add("⊘", sWarn, "leave out", p.Ignore, func(i planItem) string { return "multiversectl catalog ignore " + i.Path })
	if limit := m.listHeight() - 8; len(lines) > limit && limit > 3 {
		lines = append(lines[:limit-1], sMute.Render(fmt.Sprintf("… and %d more", len(lines)-limit+1)))
	}
	if len(p.Uninstall) > 0 {
		lines = append(lines, "", sMute.Render("Uninstalling only stops tracking it here: its files and universes stay."))
	}
	return m.box(fmt.Sprintf("Make these %d change(s)?", p.count()), lines, "enter make them · esc back")
}

func (m manageModel) helpBox() string {
	return m.box("Keys", []string{
		"↑ ↓  j k      move              PgUp PgDn  g G   page, first, last",
		"←  h          fold a catalog, or go to the one that holds it     →  l   unfold",
		"space  x      install what is not installed; uninstall what is",
		"              (on a catalog: everything under it)",
		"e  tab        leave a node out on this machine, or bring it back",
		"r             remove the entry from the catalog (shared)",
		"/             filter by name, path or repository        esc  clear",
		"a  u          add an entry · update what is installed (guided, in the terminal)",
		"enter         review what is staged, then make it",
		"esc           discard what is staged         q  quit",
		"",
		"Installing, uninstalling and leaving out are staged: nothing is done until you review it.",
	}, "any key closes this")
}

// pad fills a block to a number of lines, so the footer stays where it is.
func (m manageModel) pad(s string, lines int) string {
	have := strings.Count(s, "\n") + 1
	if have < lines {
		s += strings.Repeat("\n", lines-have)
	}
	return s + "\n"
}
