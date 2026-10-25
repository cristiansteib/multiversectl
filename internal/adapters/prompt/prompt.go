// Package prompt asks questions on the terminal.
package prompt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/app"
)

type Console struct {
	in  *bufio.Reader
	out io.Writer
	tty bool
	// ctx ends a question that is waiting for an answer: a read on a terminal
	// does not return when Ctrl+C is caught rather than left to kill the
	// process, so the wait has to be raced against the context.
	ctx context.Context
}

var _ app.Prompter = (*Console)(nil)

// New asks on the terminal. A cancelled ctx abandons the question that is
// waiting, with ctx's error.
func New(ctx context.Context) *Console {
	return &Console{
		ctx: ctx,
		in:  bufio.NewReader(os.Stdin),
		out: os.Stderr, // stdout stays clean for output a script might read
		tty: isTerminal(os.Stdin),
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

func (c *Console) line(label string) (string, error) {
	if !c.tty {
		return "", app.ErrNotInteractive
	}
	fmt.Fprint(c.out, label)
	type answer struct {
		text string
		err  error
	}
	got := make(chan answer, 1)
	go func() {
		text, err := c.in.ReadString('\n')
		got <- answer{text, err}
	}()
	select {
	case <-c.ctx.Done():
		fmt.Fprintln(c.out)
		return "", c.ctx.Err()
	case a := <-got:
		if a.err != nil && a.text == "" {
			return "", fmt.Errorf("no answer: %w", a.err)
		}
		return strings.TrimSpace(a.text), nil
	}
}

// show prints a menu. Every line is numbered by its place so the numbers do not
// shift; one that cannot be chosen gets a dash and its note, so it is still
// seen and its absence not a mystery.
func (c *Console) show(label string, options []app.Option) {
	fmt.Fprintln(c.out, label)
	width := 0
	for _, o := range options {
		if w := len([]rune(o.Label)); w > width {
			width = w
		}
	}
	for i, o := range options {
		num := fmt.Sprintf("%2d", i+1)
		if o.Disabled {
			num = " -"
		}
		line := fmt.Sprintf("  %s  %s", num, o.Label)
		if o.Note != "" {
			line += strings.Repeat(" ", width-len([]rune(o.Label))) + "  " + o.Note
		}
		fmt.Fprintln(c.out, strings.TrimRight(line, " "))
	}
}

func chooseable(options []app.Option) int {
	n := 0
	for _, o := range options {
		if !o.Disabled {
			n++
		}
	}
	return n
}

func (c *Console) Select(label string, options []app.Option, def int) (int, error) {
	if !c.tty {
		return 0, app.ErrNotInteractive
	}
	if chooseable(options) == 0 {
		return 0, errors.New("nothing here can be chosen")
	}
	c.show(label, options)
	for {
		text, err := c.line(fmt.Sprintf("choose [%d]: ", def+1))
		if err != nil {
			return 0, err
		}
		if text == "" {
			return def, nil
		}
		if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= len(options) && !options[n-1].Disabled {
			return n - 1, nil
		}
		fmt.Fprintf(c.out, "enter the number of a line that has one\n")
	}
}

// MultiSelect returns nothing on an empty answer: Enter is how a person backs
// out, and a menu that can only be left with a valid number is a trap.
func (c *Console) MultiSelect(label string, options []app.Option) ([]int, error) {
	if !c.tty {
		return nil, app.ErrNotInteractive
	}
	if chooseable(options) == 0 {
		return nil, errors.New("nothing here can be chosen")
	}
	c.show(label, options)
	for {
		text, err := c.line("numbers separated by commas, all, or Enter for none: ")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(text) == "" {
			return nil, nil
		}
		if picked, ok := parseNumbers(text, options); ok {
			return picked, nil
		}
		fmt.Fprintf(c.out, "enter numbers of lines that have one, or all\n")
	}
}

func parseNumbers(text string, options []app.Option) ([]int, bool) {
	text = strings.TrimSpace(text)
	var out []int
	if text == "all" {
		for i, o := range options {
			if !o.Disabled {
				out = append(out, i)
			}
		}
		return out, true
	}
	seen := map[int]bool{}
	for _, part := range strings.Split(text, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 || n > len(options) || options[n-1].Disabled {
			return nil, false
		}
		if !seen[n-1] {
			seen[n-1] = true
			out = append(out, n-1)
		}
	}
	return out, true
}

func (c *Console) Confirm(label string, def bool) (bool, error) {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	for {
		text, err := c.line(fmt.Sprintf("%s %s ", label, hint))
		if err != nil {
			return false, err
		}
		switch strings.ToLower(text) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
	}
}

func (c *Console) Input(label, def string) (string, error) {
	prompt := label + ": "
	if def != "" {
		prompt = fmt.Sprintf("%s [%s]: ", label, def)
	}
	text, err := c.line(prompt)
	if err != nil {
		return "", err
	}
	if text == "" {
		return def, nil
	}
	return text, nil
}
