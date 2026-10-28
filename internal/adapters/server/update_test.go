package server

import (
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// A page can ask for the newest release, and the server does not ask GitHub for
// every page that does.
func TestAPageCanAskForTheNewestReleaseButNotMoreThanOnceAMinute(t *testing.T) {
	f := &fake{update: updateView{Notice: &app.UpdateNotice{Current: "0.0.57", Latest: "0.0.58", URL: "https://example.test/r"}}}
	_, h := serve(t, f, Options{})
	rec := do(t, h, "GET", "/api/update", "", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"latest":"0.0.58"`) {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	if !f.looked {
		t.Fatal("the first request should have looked")
	}
	f.looked = false
	do(t, h, "GET", "/api/update", "", nil)
	if f.looked {
		t.Fatal("a second request within a minute looked again")
	}
}
