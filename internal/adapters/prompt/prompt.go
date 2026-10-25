// Package prompt asks questions on the terminal: arrow keys to move, Space to
// tick, / to filter, Enter to accept. It is the one place the interface is
// drawn, so every question in the tool looks and behaves the same.
package prompt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// Console asks on the terminal. It draws on stderr, so stdout stays clean for
// output a script might read.
type Console struct {
	ctx        context.Context
	tty        bool
	accessible bool
	theme      *huh.Theme
}

var _ app.Prompter = (*Console)(nil)

// New asks on the terminal. A cancelled ctx abandons the question that is
// waiting, and so does Ctrl+C, both ending in context.Canceled.
func New(ctx context.Context) *Console {
	return &Console{
		ctx: ctx,
		tty: isTerminal(os.Stdin) && isTerminal(os.Stderr),
		// A terminal that cannot draw gets plain line questions instead.
		accessible: os.Getenv("TERM") == "dumb" || os.Getenv("ACCESSIBLE") != "",
		theme:      theme(),
	}
}

// isTerminal is a character-device check with /dev/null excluded, which is
// one too: scripts redirect to it to say "no input", and prompting there would
// answer every question with the default.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(info, null)
}

// theme is quieter than the library's default: one accent colour, dim help,
// and glyphs that read at a glance.
func theme() *huh.Theme {
	t := huh.ThemeBase()
	accent := lipgloss.Color("6")
	green := lipgloss.Color("2")
	dim := lipgloss.Color("8")

	f := &t.Focused
	f.Base = f.Base.BorderForeground(accent)
	f.Title = lipgloss.NewStyle().Foreground(accent).Bold(true)
	f.Description = lipgloss.NewStyle().Foreground(dim)
	f.ErrorIndicator = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).SetString(" ✗")
	f.ErrorMessage = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	f.SelectSelector = lipgloss.NewStyle().Foreground(accent).SetString("❯ ")
	f.Option = lipgloss.NewStyle()
	f.NextIndicator = lipgloss.NewStyle().Foreground(dim).SetString(" ↓")
	f.PrevIndicator = lipgloss.NewStyle().Foreground(dim).SetString(" ↑")
	f.MultiSelectSelector = lipgloss.NewStyle().Foreground(accent).SetString("❯ ")
	f.SelectedOption = lipgloss.NewStyle().Foreground(green)
	f.SelectedPrefix = lipgloss.NewStyle().Foreground(green).SetString("◉ ")
	f.UnselectedOption = lipgloss.NewStyle()
	f.UnselectedPrefix = lipgloss.NewStyle().Foreground(dim).SetString("○ ")
	f.TextInput.Cursor = lipgloss.NewStyle().Foreground(accent)
	f.TextInput.Prompt = lipgloss.NewStyle().Foreground(accent)
	f.TextInput.Placeholder = lipgloss.NewStyle().Foreground(dim)
	f.FocusedButton = lipgloss.NewStyle().Padding(0, 2).Bold(true).Foreground(lipgloss.Color("0")).Background(accent)
	f.BlurredButton = lipgloss.NewStyle().Padding(0, 2).Foreground(dim)
	t.Blurred = *f
	t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Title = lipgloss.NewStyle().Foreground(dim)
	t.Blurred.SelectSelector = lipgloss.NewStyle().SetString("  ")
	t.Blurred.MultiSelectSelector = lipgloss.NewStyle().SetString("  ")

	t.Help.ShortKey = lipgloss.NewStyle().Foreground(dim)
	t.Help.ShortDesc = lipgloss.NewStyle().Foreground(dim).Faint(true)
	t.Help.ShortSeparator = lipgloss.NewStyle().Foreground(dim).Faint(true)
	t.Help.FullKey, t.Help.FullDesc, t.Help.FullSeparator = t.Help.ShortKey, t.Help.ShortDesc, t.Help.ShortSeparator
	return t
}

// run shows one field. Ctrl+C reaches us as a key, not a signal, so it is
// turned into the same cancellation the rest of the tool treats as "stop".
func (c *Console) run(field huh.Field) error {
	if !c.tty {
		return app.ErrNotInteractive
	}
	err := huh.NewForm(huh.NewGroup(field)).
		WithTheme(c.theme).
		WithKeyMap(keys()).
		WithOutput(os.Stderr).
		WithAccessible(c.accessible).
		RunWithContext(c.ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return fmt.Errorf("%w", context.Canceled)
	}
	return err
}

// keys adds Space for ticking, which is what people reach for, and Esc for
// leaving, beside the library's x and Ctrl+C.
func keys() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.MultiSelect.Toggle = key.NewBinding(key.WithKeys(" ", "x"), key.WithHelp("space", "toggle"))
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"), key.WithHelp("esc", "cancel"))
	return km
}

// labels draws each option as its name and, in a column beside it, its note.
func labels(options []app.Option) []string {
	width := 0
	for _, o := range options {
		width = max(width, utf8.RuneCountInString(o.Label))
	}
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	out := make([]string, len(options))
	for i, o := range options {
		text := o.Label
		if o.Note != "" {
			text += strings.Repeat(" ", width-utf8.RuneCountInString(o.Label)) + "  " + dim.Render(o.Note)
		}
		if o.Disabled {
			text = dim.Render(o.Label + strings.Repeat(" ", width-utf8.RuneCountInString(o.Label)) + "  " + o.Note)
		}
		out[i] = text
	}
	return out
}

// listHeight keeps a long list scrolling inside the screen.
func listHeight(n int) int { return min(n, 12) + 1 }

func (c *Console) Select(label string, options []app.Option, def int) (int, error) {
	if !c.tty {
		return 0, app.ErrNotInteractive
	}
	free := 0
	for _, o := range options {
		if !o.Disabled {
			free++
		}
	}
	if free == 0 {
		return 0, errors.New("nothing here can be chosen")
	}
	texts := labels(options)
	chosen := def
	description := ""
	for {
		opts := make([]huh.Option[int], len(options))
		for i := range options {
			opts[i] = huh.NewOption(texts[i], i)
		}
		sel := huh.NewSelect[int]().Title(label).Description(description).Options(opts...).Value(&chosen)
		if len(options) > 12 {
			sel = sel.Height(listHeight(len(options)))
		}
		if err := c.run(sel); err != nil {
			return 0, err
		}
		if !options[chosen].Disabled {
			return chosen, nil
		}
		// A line that cannot be chosen is shown with its reason, so it is not a mystery.
		description = "not available: " + options[chosen].Note
	}
}

// MultiSelect returns nothing when the person ticks nothing and presses Enter:
// that is how they back out. Long lists can be filtered by typing /.
func (c *Console) MultiSelect(label string, options []app.Option) ([]int, error) {
	if !c.tty {
		return nil, app.ErrNotInteractive
	}
	var shown []int // indexes into options of the lines that can be ticked
	for i, o := range options {
		if !o.Disabled {
			shown = append(shown, i)
		}
	}
	if len(shown) == 0 {
		return nil, errors.New("nothing here can be chosen")
	}
	texts := labels(options)
	opts := make([]huh.Option[int], len(shown))
	for k, i := range shown {
		opts[k] = huh.NewOption(texts[i], i).Selected(options[i].Checked)
	}
	var picked []int
	field := huh.NewMultiSelect[int]().Title(label).Options(opts...).Value(&picked).Filterable(len(shown) > 8)
	if len(shown) > 12 {
		field = field.Height(listHeight(len(shown)))
	}
	if err := c.run(field); err != nil {
		return nil, err
	}
	return picked, nil
}

func (c *Console) Confirm(label string, def bool) (bool, error) {
	answer := def
	if err := c.run(huh.NewConfirm().Title(label).Affirmative("Yes").Negative("No").Value(&answer)); err != nil {
		return false, err
	}
	return answer, nil
}

// Input starts with def filled in, so Enter accepts it and a few keys edit it.
func (c *Console) Input(label, def string) (string, error) {
	answer := def
	if err := c.run(huh.NewInput().Title(label).Value(&answer)); err != nil {
		return "", err
	}
	return strings.TrimSpace(answer), nil
}
