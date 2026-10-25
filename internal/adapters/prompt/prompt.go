// Package prompt asks questions on the terminal.
package prompt

import (
	"bufio"
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
}

var _ app.Prompter = (*Console)(nil)

func New() *Console {
	return &Console{
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
	text, err := c.in.ReadString('\n')
	if err != nil && text == "" {
		return "", fmt.Errorf("no answer: %w", err)
	}
	return strings.TrimSpace(text), nil
}

func (c *Console) Select(label string, options []string, def int) (int, error) {
	if !c.tty {
		return 0, app.ErrNotInteractive
	}
	fmt.Fprintln(c.out, label)
	for i, o := range options {
		fmt.Fprintf(c.out, "  %d) %s\n", i+1, o)
	}
	for {
		text, err := c.line(fmt.Sprintf("choose [%d]: ", def+1))
		if err != nil {
			return 0, err
		}
		if text == "" {
			return def, nil
		}
		if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		fmt.Fprintf(c.out, "enter a number from 1 to %d\n", len(options))
	}
}

func (c *Console) MultiSelect(label string, options []string) ([]int, error) {
	if !c.tty {
		return nil, app.ErrNotInteractive
	}
	fmt.Fprintln(c.out, label)
	for i, o := range options {
		fmt.Fprintf(c.out, "  %d) %s\n", i+1, o)
	}
	for {
		text, err := c.line("numbers separated by commas, or all: ")
		if err != nil {
			return nil, err
		}
		if picked, ok := parseNumbers(text, len(options)); ok {
			return picked, nil
		}
		fmt.Fprintf(c.out, "enter numbers from 1 to %d, or all\n", len(options))
	}
}

func parseNumbers(text string, max int) ([]int, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	var out []int
	if text == "all" {
		for i := 0; i < max; i++ {
			out = append(out, i)
		}
		return out, true
	}
	seen := map[int]bool{}
	for _, part := range strings.Split(text, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 || n > max {
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
