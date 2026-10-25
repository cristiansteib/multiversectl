package prompt

import (
	"context"
	"fmt"
	"os"
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
