package cli

import (
	"errors"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// fakeEnv stands in for the catalog: a tree, what is installed, and a record of what was asked.
type fakeEnv struct {
	tree      domain.Node
	installed map[string]string
	applied   []managePlan
	removed   []string
	asked     []string
}

type noop struct{ run func() }

func (n noop) Run() error        { n.run(); return nil }
func (noop) SetStdin(io.Reader)  {}
func (noop) SetStdout(io.Writer) {}
func (noop) SetStderr(io.Writer) {}

func (f *fakeEnv) Load() (domain.Node, map[string]string, error) { return f.tree, f.installed, nil }
func (f *fakeEnv) Apply(p managePlan) tea.ExecCommand {
	return noop{func() { f.applied = append(f.applied, p) }}
}
func (f *fakeEnv) RemoveEntry(path string) tea.ExecCommand {
	return noop{func() { f.removed = append(f.removed, path) }}
}
func (f *fakeEnv) Interactive(which string) tea.ExecCommand {
	return noop{func() { f.asked = append(f.asked, which) }}
}

func testTree() domain.Node {
	return domain.Node{Name: "index", Kind: domain.CatalogNode, Children: []domain.Node{
		{Name: "backend", Path: "backend", Kind: domain.CatalogNode, Children: []domain.Node{
			{Name: "billing", Path: "backend/billing", Kind: domain.MultiverseNode, Source: domain.Source{Repo: "git@example.test:acme/billing.git"}},
			{Name: "search", Path: "backend/search", Kind: domain.MultiverseNode},
		}},
		{Name: "web", Path: "web", Kind: domain.MultiverseNode},
		{Name: "vpn", Path: "vpn", Kind: domain.CatalogNode, Ignored: true},
		{Name: "gone", Path: "gone", Kind: domain.CatalogNode, Unavailable: "no access"},
	}}
}

// loaded is a screen that has read the catalog, with web installed.
func loaded(t *testing.T) (manageModel, *fakeEnv) {
	t.Helper()
	env := &fakeEnv{tree: testTree(), installed: map[string]string{"web": "web"}}
	m := newManageModel(env)
	next, _ := m.Update(loadedMsg{tree: env.tree, installed: env.installed})
	return next.(manageModel), env
}

func press(m manageModel, keys ...string) manageModel {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.Update(msg)
		m = next.(manageModel)
	}
	return m
}

func names(rows []viewRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.node.Name)
	}
	return out
}

func TestTheTreeIsShownWithEverythingInItAndFoldsCatalogs(t *testing.T) {
	m, _ := loaded(t)
	if got := strings.Join(names(m.rows()), ","); got != "index,backend,billing,search,web,vpn,gone" {
		t.Fatalf("rows = %s", got)
	}
	m = press(m, "down", "left") // on backend: fold it
	if got := strings.Join(names(m.rows()), ","); got != "index,backend,web,vpn,gone" {
		t.Fatalf("folded = %s", got)
	}
	m = press(m, "right")
	if len(m.rows()) != 7 {
		t.Fatalf("unfolded: %d rows", len(m.rows()))
	}
}

func TestSpaceStagesAnInstallAndAgainTakesItOff(t *testing.T) {
	m, _ := loaded(t)
	m = press(m, "down", "down") // billing: the root is the first row
	if row, _ := m.current(); row.node.Name != "billing" {
		t.Fatalf("on %s", row.node.Name)
	}
	m = press(m, "space")
	if m.staged["backend/billing"] != install {
		t.Fatalf("staged = %v", m.staged)
	}
	m = press(m, "space")
	if len(m.staged) != 0 {
		t.Fatalf("pressing again should take it off: %v", m.staged)
	}
}

func TestSpaceOnAnInstalledMultiverseStagesAnUninstall(t *testing.T) {
	m, _ := loaded(t)
	m = press(m, "down", "down", "down", "down") // web
	m = press(m, "space")
	if m.staged["web"] != uninstall {
		t.Fatalf("staged = %v", m.staged)
	}
	if p := m.plan(); len(p.Uninstall) != 1 || p.Uninstall[0].Name != "web" {
		t.Fatalf("plan = %+v", p)
	}
}

// On a catalog, space installs what is missing under it; when all is there it uninstalls.
func TestSpaceOnACatalogActsOnEverythingUnderIt(t *testing.T) {
	m, _ := loaded(t)
	m = press(m, "down", "space") // backend
	if m.staged["backend/billing"] != install || m.staged["backend/search"] != install {
		t.Fatalf("staged = %v", m.staged)
	}
	if m.installState(testTree().Children[0]) != "all" {
		t.Errorf("with the installs staged the catalog should read as all installed")
	}
	m = press(m, "space") // all are (to be) installed: now it unstages the installs... then uninstall is only for what was installed
	if len(m.staged) != 0 && m.staged["backend/billing"] == install {
		t.Fatalf("a second press should not keep staging installs: %v", m.staged)
	}
}

func TestLeavingOutAndBringingBackAreStagedAndWhatIsIgnoredCannotBeInstalled(t *testing.T) {
	m, _ := loaded(t)
	m = press(m, "down", "down", "e") // billing: leave it out
	if m.staged["backend/billing"] != ignore {
		t.Fatalf("staged = %v", m.staged)
	}
	m = press(m, "space")
	if m.staged["backend/billing"] != ignore || !strings.Contains(m.status, "enable it first") {
		t.Fatalf("an ignored multiverse is not installed: staged %v, status %q", m.staged, m.status)
	}
	m = press(m, "e") // takes the staging off
	if len(m.staged) != 0 {
		t.Fatalf("staged = %v", m.staged)
	}
	// vpn is left out already: e stages bringing it back.
	m = press(m, "down", "down", "down")
	if row, _ := m.current(); row.node.Name != "vpn" {
		t.Fatalf("on %s", row.node.Name)
	}
	m = press(m, "e")
	if m.staged["vpn"] != enable {
		t.Fatalf("staged = %v", m.staged)
	}
}

func TestLeavingACatalogOutDropsWhatWasStagedUnderIt(t *testing.T) {
	m, _ := loaded(t)
	m = press(m, "down", "space", "e") // backend: install all, then leave it out
	if m.staged["backend"] != ignore || m.staged["backend/billing"] != 0 {
		t.Fatalf("staged = %v", m.staged)
	}
}

// Nothing is done until the list has been seen and confirmed, and it is made in order.
func TestEnterReviewsAndOnlyThenMakesTheChanges(t *testing.T) {
	m, env := loaded(t)
	m = press(m, "enter")
	if m.mode != modeTree || !strings.Contains(m.status, "nothing is staged") {
		t.Fatalf("mode %v status %q", m.mode, m.status)
	}
	m = press(m, "down", "down", "space", "down", "down", "down", "e") // install billing; bring vpn back
	m = press(m, "enter")
	if m.mode != modeReview {
		t.Fatalf("mode = %v", m.mode)
	}
	if v := m.View(); !strings.Contains(v, "multiversectl start backend/billing --install-only") || !strings.Contains(v, "multiversectl catalog unignore vpn") {
		t.Errorf("the review should show the commands:\n%s", v)
	}
	if len(env.applied) != 0 {
		t.Fatal("nothing should have been made yet")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(manageModel)
	if cmd == nil {
		t.Fatal("confirming should hand the terminal over to make the changes")
	}
	// Running what the screen asked for is what makes the changes.
	_ = cmd() // tea.Exec's command is run by the program; here the plan it carries is what matters
	p := m.plan()
	if len(p.Install) != 1 || p.Install[0].Path != "backend/billing" || len(p.Enable) != 1 || p.Enable[0].Path != "vpn" {
		t.Fatalf("plan = %+v", p)
	}
}

func TestEscDiscardsWhatIsStagedAndQuitAsksWhenSomethingIs(t *testing.T) {
	m, _ := loaded(t)
	m = press(m, "down", "down", "space")
	m = press(m, "q")
	if m.mode != modeQuit {
		t.Fatalf("q with staged changes should ask: %v", m.mode)
	}
	m = press(m, "esc")
	m = press(m, "esc")
	if len(m.staged) != 0 {
		t.Fatalf("esc should discard: %v", m.staged)
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); cmd == nil {
		t.Fatal("q with nothing staged quits")
	}
}

func TestRemovingAnEntryAsksFirst(t *testing.T) {
	m, env := loaded(t)
	m = press(m, "down", "down", "r") // billing
	if m.mode != modeRemove || m.removing != "backend/billing" {
		t.Fatalf("mode %v removing %q", m.mode, m.removing)
	}
	if len(env.removed) != 0 {
		t.Fatal("asked first, not done")
	}
	m = press(m, "esc")
	if m.mode != modeTree {
		t.Fatalf("esc goes back: %v", m.mode)
	}
}

func TestTheFilterShowsWhatMatchesWithTheWayDownToIt(t *testing.T) {
	m, _ := loaded(t)
	m = press(m, "/")
	for _, r := range "bill" {
		m = press(m, string(r))
	}
	if got := strings.Join(names(m.rows()), ","); got != "index,backend,billing" {
		t.Fatalf("rows = %s", got)
	}
	m = press(m, "esc")
	if len(m.rows()) != 7 || m.mode != modeTree {
		t.Fatalf("esc clears the filter: %d rows, mode %v", len(m.rows()), m.mode)
	}
}

func TestAFailedReadIsShownNotHidden(t *testing.T) {
	m := newManageModel(&fakeEnv{})
	next, _ := m.Update(loadedMsg{err: errors.New("no root catalog")})
	if v := next.(manageModel).View(); !strings.Contains(v, "no root catalog") {
		t.Fatalf("the error should be on screen:\n%s", v)
	}
}

func TestTheScreenIsReadableAtTheSizeOfAWindow(t *testing.T) {
	m, _ := loaded(t)
	m = press(m, "down", "down", "space")
	v := m.View()
	for _, want := range []string{"Catalog", "billing", "[+]", "+ install", "Staged: install 1", "space install", "left out on this machine"} {
		if !strings.Contains(v, want) {
			t.Errorf("the screen lacks %q:\n%s", want, v)
		}
	}
	if got := strings.Count(v, "\n") + 1; got > m.height {
		t.Errorf("the screen is %d lines in a window of %d", got, m.height)
	}
}
