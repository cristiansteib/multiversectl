package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// manageEnv is what the screen asks of the outside: it reads and changes
// nothing itself, so it can be driven in a test by a fake.
type manageEnv interface {
	// Load reads the tree and which multiverses are installed (by catalog path,
	// the name each is installed as).
	Load() (domain.Node, map[string]string, error)
	// Apply makes the staged changes, with the terminal handed back: what it does
	// prints as the commands do, and the screen returns after.
	Apply(plan managePlan) tea.ExecCommand
	// RemoveEntry takes an entry out of the catalog (an edit of a shared repository),
	// with the terminal handed back.
	RemoveEntry(path string) tea.ExecCommand
	// Interactive runs one of the catalog's own guided commands ("add", "update")
	// with the terminal handed back.
	Interactive(which string) tea.ExecCommand
}

// changeKind is one thing staged on one node.
type changeKind int

const (
	install changeKind = iota + 1
	uninstall
	ignore
	enable
)

func (k changeKind) String() string {
	return [...]string{"", "install", "uninstall", "ignore", "enable"}[k]
}

// managePlan is what the person staged, in the order it is made: what was left
// out comes back first so it can be installed, and what is left out last, so an
// install is not made of something about to be ignored.
type managePlan struct {
	Enable, Install, Uninstall, Ignore []planItem
}

type planItem struct {
	Path string // in the catalog
	Name string // what it is called, and for an uninstall, what it is installed as
}

func (p managePlan) empty() bool {
	return len(p.Enable)+len(p.Install)+len(p.Uninstall)+len(p.Ignore) == 0
}

func (p managePlan) count() int {
	return len(p.Enable) + len(p.Install) + len(p.Uninstall) + len(p.Ignore)
}

// mode is what the keys mean right now.
type mode int

const (
	modeTree mode = iota
	modeFilter
	modeReview
	modeRemove
	modeQuit
	modeHelp
)

// Messages the screen handles besides keys.
type (
	loadedMsg struct {
		tree      domain.Node
		installed map[string]string
		err       error
	}
	doneMsg    struct{ err error } // after a command that had the terminal
	removedMsg struct{ err error }
)

// viewRow is one line of the tree as it is shown.
type viewRow struct {
	node      domain.Node
	prefix    string
	depth     int
	expand    bool // a catalog with something under it
	collapsed bool
}

// manageModel is the screen: the tree to move through, what has been staged on
// it, and the mode it is in.
type manageModel struct {
	env       manageEnv
	tree      domain.Node
	installed map[string]string
	loaded    bool

	collapsed map[string]bool
	cursor    int
	offset    int
	filter    textinput.Model
	mode      mode
	// staged is what the person has changed and not yet made, by the path of the
	// node: an install or uninstall on a multiverse, an ignore or enable on any.
	staged map[string]changeKind
	status string
	errMsg string

	width, height int
	removing      string // the path asked about in modeRemove
}

func newManageModel(env manageEnv) manageModel {
	f := textinput.New()
	f.Prompt = "/ "
	f.Placeholder = "filter by name, path or repository"
	return manageModel{env: env, collapsed: map[string]bool{}, staged: map[string]changeKind{}, filter: f, width: 100, height: 30}
}

func (m manageModel) Init() tea.Cmd { return m.load() }

func (m manageModel) load() tea.Cmd {
	return func() tea.Msg {
		tree, installed, err := m.env.Load()
		return loadedMsg{tree, installed, err}
	}
}

// ── what a node is, with what is staged on it ──────────────────────────

// isInstalled is whether the multiverse is installed, as it would be once what is staged is made.
func (m manageModel) isInstalled(n domain.Node) bool {
	_, was := m.installed[n.Path]
	switch m.staged[n.Path] {
	case install:
		return true
	case uninstall:
		return false
	}
	return was
}

// isIgnored is whether the node is left out, as it would be once what is staged is made.
func (m manageModel) isIgnored(n domain.Node) bool {
	switch m.staged[n.Path] {
	case ignore:
		return true
	case enable:
		return false
	}
	return n.Ignored
}

// leaves are the multiverses under a node that can be installed or uninstalled:
// not left out, and readable.
func (m manageModel) leaves(n domain.Node) []domain.Node {
	var out []domain.Node
	for _, l := range n.Leaves() {
		if !m.isIgnored(l) && l.Unavailable == "" {
			out = append(out, l)
		}
	}
	return out
}

// installState is "all", "some" or "none" of what is under a node installed.
func (m manageModel) installState(n domain.Node) string {
	ls := m.leaves(n)
	have := 0
	for _, l := range ls {
		if m.isInstalled(l) {
			have++
		}
	}
	switch {
	case len(ls) == 0 || have == 0:
		return "none"
	case have == len(ls):
		return "all"
	}
	return "some"
}

// ── the rows ───────────────────────────────────────────────────────────

func (m manageModel) rows() []viewRow {
	var out []viewRow
	f := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	matches := func(n domain.Node) bool {
		return f == "" || strings.Contains(strings.ToLower(n.Name+" "+n.Path+" "+n.Source.Repo), f)
	}
	// With a filter, what matches is shown with the way down to it and everything under it.
	var keep func(n domain.Node, inherited bool) bool
	keepSet := map[string]bool{}
	keep = func(n domain.Node, inherited bool) bool {
		mine := matches(n)
		any := f == "" || inherited || mine
		for _, c := range n.Children {
			if keep(c, f != "" && (inherited || mine)) {
				any = true
			}
		}
		if any {
			keepSet[n.Path] = true
		}
		return any
	}
	keep(m.tree, false)
	var walk func(n domain.Node, indent string, depth int, last bool)
	walk = func(n domain.Node, indent string, depth int, last bool) {
		if !keepSet[n.Path] {
			return
		}
		prefix := ""
		if depth > 0 {
			conn := "├─ "
			if last {
				conn = "└─ "
			}
			prefix = indent + conn
		}
		hasKids := len(n.Children) > 0
		out = append(out, viewRow{node: n, prefix: prefix, depth: depth, expand: hasKids, collapsed: m.collapsed[n.Path] && f == ""})
		if m.collapsed[n.Path] && f == "" {
			return
		}
		next := indent
		if depth > 0 {
			next += "│  "
			if last {
				next = indent + "   "
			}
		}
		// Only what is kept is counted for "last", so the guides close properly.
		var kept []domain.Node
		for _, c := range n.Children {
			if keepSet[c.Path] {
				kept = append(kept, c)
			}
		}
		for i, c := range kept {
			walk(c, next, depth+1, i == len(kept)-1)
		}
	}
	walk(m.tree, "", 0, true)
	return out
}

// current is the row the cursor is on, if there is one.
func (m manageModel) current() (viewRow, bool) {
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return viewRow{}, false
	}
	return rows[m.cursor], true
}

// ── staging ────────────────────────────────────────────────────────────

// stage sets what is to be done to a node; making it what it already is takes
// the staging off, so pressing the same key twice is no change.
func (m *manageModel) stage(n domain.Node, k changeKind) {
	switch {
	case k == install && m.installedAlready(n):
		delete(m.staged, n.Path)
	case k == uninstall && !m.installedAlready(n):
		delete(m.staged, n.Path)
	case k == ignore && n.Ignored, k == enable && !n.Ignored:
		delete(m.staged, n.Path)
	default:
		m.staged[n.Path] = k
	}
}

func (m manageModel) installedAlready(n domain.Node) bool { _, ok := m.installed[n.Path]; return ok }

// toggleInstall installs what is not installed, or uninstalls what is, for a
// multiverse, or for everything under a catalog: installs when any is missing.
func (m *manageModel) toggleInstall(n domain.Node) {
	if m.isIgnored(n) {
		m.status = fmt.Sprintf("%s is ignored: enable it first (e)", n.Name)
		return
	}
	if n.Unavailable != "" {
		m.status = fmt.Sprintf("%s cannot be read: %s", n.Name, n.Unavailable)
		return
	}
	ls := m.leaves(n)
	if len(ls) == 0 {
		m.status = "nothing here to install"
		return
	}
	missing := false
	for _, l := range ls {
		if !m.isInstalled(l) {
			missing = true
		}
	}
	for _, l := range ls {
		if missing && !m.isInstalled(l) {
			m.stage(l, install)
		} else if !missing {
			m.stage(l, uninstall)
		}
	}
	m.status = ""
}

// toggleEnabled leaves a node out, or brings it back.
func (m *manageModel) toggleEnabled(n domain.Node) {
	if n.Path == "" {
		m.status = "the root catalog cannot be left out"
		return
	}
	if m.isIgnored(n) {
		m.stage(n, enable)
		return
	}
	// What was staged under it no longer matters.
	for _, l := range n.Leaves() {
		if l.Path != n.Path {
			delete(m.staged, l.Path)
		}
	}
	m.stage(n, ignore)
	m.status = ""
}

// plan is what is staged, in the order it is made.
func (m manageModel) plan() managePlan {
	var p managePlan
	byPath := map[string]domain.Node{}
	var index func(n domain.Node)
	index = func(n domain.Node) {
		byPath[n.Path] = n
		for _, c := range n.Children {
			index(c)
		}
	}
	index(m.tree)
	paths := make([]string, 0, len(m.staged))
	for path := range m.staged {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		n := byPath[path]
		item := planItem{Path: path, Name: n.Name}
		switch m.staged[path] {
		case enable:
			p.Enable = append(p.Enable, item)
		case install:
			p.Install = append(p.Install, item)
		case uninstall:
			item.Name = m.installed[path]
			p.Uninstall = append(p.Uninstall, item)
		case ignore:
			p.Ignore = append(p.Ignore, item)
		}
	}
	return p
}

// ── keys ───────────────────────────────────────────────────────────────

func (m manageModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.keepCursorInView()
	case loadedMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.tree, m.installed, m.loaded, m.errMsg = msg.tree, msg.installed, true, ""
		m.staged = map[string]changeKind{}
		if rows := m.rows(); m.cursor >= len(rows) {
			m.cursor = max(0, len(rows)-1)
		}
	case doneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		}
		m.mode = modeTree
		return m, m.load()
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m manageModel) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.errMsg = ""
	switch m.mode {
	case modeFilter:
		switch k.String() {
		case "enter":
			m.filter.Blur()
			m.mode = modeTree
		case "esc":
			m.filter.SetValue("")
			m.filter.Blur()
			m.mode = modeTree
		default:
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(k)
			m.cursor, m.offset = 0, 0
			return m, cmd
		}
		return m, nil
	case modeReview:
		switch k.String() {
		case "enter", "y":
			plan := m.plan()
			m.mode = modeTree
			return m, tea.Exec(m.env.Apply(plan), func(err error) tea.Msg { return doneMsg{err} })
		case "esc", "n", "q":
			m.mode = modeTree
		}
		return m, nil
	case modeRemove:
		switch k.String() {
		case "y", "enter":
			path := m.removing
			m.mode = modeTree
			return m, tea.Exec(m.env.RemoveEntry(path), func(err error) tea.Msg { return doneMsg{err} })
		case "esc", "n", "q":
			m.mode = modeTree
		}
		return m, nil
	case modeQuit:
		switch k.String() {
		case "y", "enter":
			return m, tea.Quit
		case "esc", "n":
			m.mode = modeTree
		}
		return m, nil
	case modeHelp:
		m.mode = modeTree
		return m, nil
	}

	// The tree.
	rows := m.rows()
	row, onRow := m.current()
	switch k.String() {
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(rows)-1, m.cursor+1)
	case "pgup":
		m.cursor = max(0, m.cursor-m.listHeight())
	case "pgdown":
		m.cursor = min(len(rows)-1, m.cursor+m.listHeight())
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = max(0, len(rows)-1)
	case "left", "h":
		switch {
		case onRow && row.expand && !row.collapsed:
			m.collapsed[row.node.Path] = true
		case onRow && row.depth > 0:
			// Closed already: go to what holds it.
			for i := m.cursor - 1; i >= 0; i-- {
				if rows[i].depth < row.depth {
					m.cursor = i
					break
				}
			}
		}
	case "right", "l":
		if onRow && row.expand && row.collapsed {
			delete(m.collapsed, row.node.Path)
		}
	case " ", "x":
		if onRow {
			m.toggleInstall(row.node)
		}
	case "e", "tab":
		if onRow {
			m.toggleEnabled(row.node)
		}
	case "r":
		if onRow && row.node.Path != "" {
			m.removing, m.mode = row.node.Path, modeRemove
		}
	case "/":
		m.mode = modeFilter
		m.filter.Focus()
		return m, textinput.Blink
	case "a":
		return m, tea.Exec(m.env.Interactive("add"), func(err error) tea.Msg { return doneMsg{err} })
	case "u":
		return m, tea.Exec(m.env.Interactive("update"), func(err error) tea.Msg { return doneMsg{err} })
	case "enter":
		if m.plan().empty() {
			m.status = "nothing is staged: space installs, e leaves out or brings back"
		} else {
			m.mode = modeReview
		}
	case "esc":
		switch {
		case m.filter.Value() != "":
			m.filter.SetValue("")
			m.cursor, m.offset = 0, 0
		case !m.plan().empty():
			m.staged = map[string]changeKind{}
			m.status = "staged changes discarded"
		}
	case "?":
		m.mode = modeHelp
	case "q", "ctrl+c":
		if m.plan().empty() {
			return m, tea.Quit
		}
		m.mode = modeQuit
	}
	m.keepCursorInView()
	return m, nil
}

// listHeight is how many tree rows fit between the header and the footer.
func (m manageModel) listHeight() int { return max(3, m.height-6) }

func (m *manageModel) keepCursorInView() {
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}
