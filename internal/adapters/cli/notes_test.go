package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// A note is prose: one that runs over two lines must not break the column its
// first line sits in, or the journal stops reading as one.
func TestAJournalKeepsItsColumnsUnderAMultiLineNote(t *testing.T) {
	at := time.Date(2026, 10, 30, 18, 50, 0, 0, time.UTC).Local()
	out := journalLines([]domain.Note{
		{ID: "n9", At: at, By: "claude", Text: "api redirect fixed"},
		{ID: "n10", At: at, Text: "a longer note\nover two lines"},
	})
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("want three lines:\n%s", out)
	}
	text := strings.Index(lines[1], "a longer note")
	if text < 0 || !strings.HasPrefix(lines[2], strings.Repeat(" ", text)+"over two lines") {
		t.Errorf("the second line should start under the first:\n%s", out)
	}
	// The ids line up, and a journal nobody signed has no column for who.
	if !strings.HasPrefix(lines[0], "n9 ") || !strings.HasPrefix(lines[1], "n10") {
		t.Errorf("ids are not lined up:\n%s", out)
	}
	if unsigned := journalLines([]domain.Note{{ID: "n1", At: at, Text: "alone"}}); !strings.HasSuffix(unsigned, "  alone") || strings.Contains(unsigned, "   alone") {
		t.Errorf("an unsigned journal should keep no room for a name: %q", unsigned)
	}
}
