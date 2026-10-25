package cli

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	red    = "\x1b[31m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	blue   = "\x1b[34m"
	cyan   = "\x1b[36m"
)

// ansi matches the escape sequences paint writes, so a painted string's
// printed width can still be measured.
var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func visibleLen(s string) int { return utf8.RuneCountInString(ansi.ReplaceAllString(s, "")) }

// Output renders use-case messages to a terminal or a pipe.
type Output struct {
	w     io.Writer
	color bool
}

func NewOutput(w io.Writer, color bool) *Output { return &Output{w: w, color: color} }

// ColorEnabled is false under NO_COLOR, --no-color, or when f is not a terminal.
func ColorEnabled(f *os.File, noColorFlag bool) bool {
	if noColorFlag || os.Getenv("NO_COLOR") != "" {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (o *Output) paint(code, s string) string {
	if !o.color || code == "" {
		return s
	}
	return code + s + reset
}

func (o *Output) line(code, glyph, format string, a ...any) {
	fmt.Fprintf(o.w, "%s %s\n", o.paint(code, glyph), fmt.Sprintf(format, a...))
}

func (o *Output) Info(format string, a ...any) { o.line(blue, "›", format, a...) }
func (o *Output) OK(format string, a ...any)   { o.line(green, "✓", format, a...) }
func (o *Output) Warn(format string, a ...any) { o.line(yellow, "!", format, a...) }
func (o *Output) Fail(format string, a ...any) { o.line(red, "✗", format, a...) }
func (o *Output) Plain(format string, a ...any) {
	fmt.Fprintf(o.w, format+"\n", a...)
}

// Bold and the colours below return s styled for the output's colour setting.
func (o *Output) Bold(s string) string   { return o.paint(bold, s) }
func (o *Output) Red(s string) string    { return o.paint(red, s) }
func (o *Output) Dim(s string) string    { return o.paint(dim, s) }
func (o *Output) Green(s string) string  { return o.paint(green, s) }
func (o *Output) Yellow(s string) string { return o.paint(yellow, s) }
func (o *Output) Accent(s string) string { return o.paint(cyan, s) }

// Cell is one table cell and the colour it is painted in.
type Cell struct {
	Text  string
	Style string
}

// Plain cells are a row of unstyled text.
func Plain(texts ...string) []Cell {
	row := make([]Cell, len(texts))
	for i, t := range texts {
		row[i] = Cell{Text: t}
	}
	return row
}

// Table is a column-aligned table of unstyled rows.
func (o *Output) Table(headers []string, rows [][]string) {
	styled := make([][]Cell, len(rows))
	for i, row := range rows {
		styled[i] = Plain(row...)
	}
	for _, line := range o.tableLines(headers, styled) {
		fmt.Fprintln(o.w, line)
	}
}

// Block is a titled table under a rule as wide as its widest line, so two
// blocks printed one after the other never read as one table.
func (o *Output) Block(title string, headers []string, rows [][]Cell) {
	lines := o.tableLines(headers, rows)
	width := visibleLen(title)
	for _, line := range lines {
		width = max(width, visibleLen(line))
	}
	fmt.Fprintln(o.w, title)
	fmt.Fprintln(o.w, o.paint(dim, strings.Repeat("─", width)))
	for _, line := range lines {
		fmt.Fprintln(o.w, line)
	}
}

// tableLines renders the header and rows into column-aligned lines, each cell
// painted in its own colour.
func (o *Output) tableLines(headers []string, rows [][]Cell) []string {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				widths[i] = max(widths[i], utf8.RuneCountInString(cell.Text))
			}
		}
	}
	render := func(cells []Cell) string {
		var b strings.Builder
		for i, cell := range cells {
			b.WriteString(o.paint(cell.Style, cell.Text))
			if i == len(cells)-1 {
				break
			}
			b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell.Text)+2))
		}
		return strings.TrimRight(b.String(), " ")
	}
	head := make([]Cell, len(headers))
	for i, h := range headers {
		head[i] = Cell{Text: h, Style: bold + dim}
	}
	lines := make([]string, 0, len(rows)+1)
	lines = append(lines, render(head))
	for _, row := range rows {
		lines = append(lines, render(row))
	}
	return lines
}
