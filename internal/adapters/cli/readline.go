package cli

import (
	"bufio"
	"context"
	"os"
)

// readLine reads one line from the terminal and gives up when ctx ends. A
// plain read does not return on Ctrl+C once the signal is caught instead of
// killing the process, which left a confirmation prompt deaf to it.
func readLine(ctx context.Context) (string, error) {
	type answer struct {
		text string
		err  error
	}
	got := make(chan answer, 1)
	go func() {
		text, err := bufio.NewReader(os.Stdin).ReadString('\n')
		got <- answer{text, err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case a := <-got:
		return a.text, a.err
	}
}
