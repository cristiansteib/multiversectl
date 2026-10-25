package prompt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/cristiansteib/multiversectl/internal/app"
)

var _ app.Progress = (*Console)(nil)

var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Do runs fn under a spinner that says what is being waited for, and how long it
// has been once it is not instant. The line is erased when fn returns, so a
// quick call leaves nothing and a slow one leaves no trace of the wait; what
// the caller reports afterwards is the only record. fn must not print.
func (c *Console) Do(label string, fn func(ctx context.Context) error) error {
	if !c.tty || c.accessible {
		return fn(c.ctx)
	}
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		fmt.Fprint(os.Stderr, "\x1b[?25l") // hide the cursor
		tick := time.NewTicker(80 * time.Millisecond)
		defer tick.Stop()
		start := time.Now()
		for i := 0; ; i++ {
			line := accent.Render(frames[i%len(frames)]) + " " + label
			if d := time.Since(start); d >= 3*time.Second {
				line += dim.Render(fmt.Sprintf("  %ds", int(d.Seconds())))
			}
			fmt.Fprint(os.Stderr, "\r\x1b[2K"+line)
			select {
			case <-done:
				fmt.Fprint(os.Stderr, "\r\x1b[2K\x1b[?25h")
				return
			case <-tick.C:
			}
		}
	}()
	err := fn(c.ctx)
	close(done)
	<-finished
	return err
}

// Parallel draws one line per job under a heading with the count, and redraws
// the block in place as jobs start, finish or fail. Past maxLines jobs only the
// ones running (and failed) get a line, so the block never outgrows the screen.
// Without a terminal it just runs them.
func (c *Console) Parallel(label string, limit int, jobs []app.Job) []error {
	if !c.tty || c.accessible {
		return app.RunJobs(c.ctx, limit, jobs, nil)
	}
	const maxLines = 10
	type state struct {
		phase      int // 0 waiting, 1 running, 2 done, 3 failed
		start, end time.Time
	}
	var mu sync.Mutex
	states := make([]state, len(jobs))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	bad := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	drawn := 0
	render := func(frame int) {
		mu.Lock()
		defer mu.Unlock()
		finished := 0
		for _, st := range states {
			if st.phase >= 2 {
				finished++
			}
		}
		lines := []string{accent.Render(frames[frame%len(frames)]) + " " + label + dim.Render(fmt.Sprintf("  %d/%d", finished, len(jobs)))}
		for i, j := range jobs {
			st := states[i]
			if len(jobs) > maxLines && st.phase != 1 && st.phase != 3 {
				continue
			}
			switch st.phase {
			case 0:
				lines = append(lines, dim.Render("  · "+j.Name))
			case 1:
				lines = append(lines, "  "+accent.Render(frames[(frame+i)%len(frames)])+" "+j.Name+dim.Render(fmt.Sprintf("  %.1fs", time.Since(st.start).Seconds())))
			case 2:
				lines = append(lines, "  "+ok.Render("✓")+" "+j.Name+dim.Render(fmt.Sprintf("  %.1fs", st.end.Sub(st.start).Seconds())))
			case 3:
				lines = append(lines, "  "+bad.Render("✗")+" "+j.Name)
			}
		}
		var b strings.Builder
		if drawn > 0 {
			fmt.Fprintf(&b, "\x1b[%dA", drawn)
		}
		for _, l := range lines {
			b.WriteString("\r\x1b[2K" + l + "\n")
		}
		// A shorter block than the last one leaves lines behind: clear them.
		for i := len(lines); i < drawn; i++ {
			b.WriteString("\r\x1b[2K\n")
		}
		if len(lines) < drawn {
			fmt.Fprintf(&b, "\x1b[%dA", drawn-len(lines))
		}
		drawn = len(lines)
		fmt.Fprint(os.Stderr, b.String())
	}

	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		fmt.Fprint(os.Stderr, "\x1b[?25l")
		tick := time.NewTicker(80 * time.Millisecond)
		defer tick.Stop()
		for i := 0; ; i++ {
			render(i)
			select {
			case <-done:
				// Erase the block: what the caller reports next is the record.
				mu.Lock()
				if drawn > 0 {
					fmt.Fprintf(os.Stderr, "\x1b[%dA", drawn)
				}
				fmt.Fprint(os.Stderr, "\r\x1b[J\x1b[?25h")
				mu.Unlock()
				return
			case <-tick.C:
			}
		}
	}()
	errs := app.RunJobs(c.ctx, limit, jobs, func(i int, end bool, err error) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case !end:
			states[i] = state{phase: 1, start: time.Now()}
		case err != nil:
			states[i].phase, states[i].end = 3, time.Now()
		default:
			states[i].phase, states[i].end = 2, time.Now()
		}
	})
	close(done)
	<-finished
	return errs
}
