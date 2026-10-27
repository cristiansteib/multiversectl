package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

func TestThePanelReadsContainersAndStats(t *testing.T) {
	f := &fake{
		containers: []containerView{{App: "api", Service: "web", Name: "acme-u3-api-web-1", Status: "Up 2 hours", Running: true}},
		stats:      []statView{{Name: "acme-u3-api-web-1", CPU: 1.5, Memory: 20, MemUsage: "50MiB / 2GiB"}},
	}
	_, h := serve(t, f, Options{})

	rec := do(t, h, "GET", "/api/universe/containers?multiverse=acme&universe=3", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"acme-u3-api-web-1"`) {
		t.Fatalf("containers: %d %s", rec.Code, rec.Body)
	}
	rec = do(t, h, "GET", "/api/universe/stats?multiverse=acme&universe=3", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"memUsage":"50MiB / 2GiB"`) {
		t.Fatalf("stats: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "GET", "/api/universe/containers?multiverse=acme", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a missing universe = %d, want 400", rec.Code)
	}
}

// The panel only reads, so --read-only keeps it; restarting is an action, and
// that stays refused there.
func TestThePanelWorksReadOnlyButRestartDoesNot(t *testing.T) {
	f := &fake{containers: []containerView{{App: "api", Service: "web", Name: "n", Running: true}}}
	_, h := serve(t, f, Options{ReadOnly: true})
	if rec := do(t, h, "GET", "/api/universe/containers?multiverse=acme&universe=3", "", nil); rec.Code != http.StatusOK {
		t.Errorf("read-only containers = %d", rec.Code)
	}
	body := `{"action":"restart","multiverse":"acme","universe":"3","app":"api","service":"web"}`
	if rec := do(t, h, "POST", "/api/ops", body, map[string]string{"X-Mvctl": "1"}); rec.Code == http.StatusAccepted {
		t.Errorf("a restart was accepted in read-only mode")
	}
}

func TestLogsAreStreamedAsText(t *testing.T) {
	f := &fake{logText: "line one\nline two\n"}
	_, h := serve(t, f, Options{})
	rec := do(t, h, "GET", "/api/universe/logs?multiverse=acme&universe=3&app=api&service=web&tail=50", "", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "line one\nline two\n" {
		t.Fatalf("logs: %d %q", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content type %q", ct)
	}
	if got := strings.Join(f.asked, ","); !strings.Contains(got, "logs:acme:3:api/web:50:false") {
		t.Errorf("asked %s", got)
	}
}

func TestLogsOfAServiceThatDoesNotExistAreNotFound(t *testing.T) {
	f := &fake{err: fmt.Errorf("universe 3 has no container for api/nope: %w", domain.ErrNotFound)}
	_, h := serve(t, f, Options{})
	rec := do(t, h, "GET", "/api/universe/logs?multiverse=acme&universe=3&app=api&service=nope", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("= %d, want 404", rec.Code)
	}
}

// Followed logs hold a docker process each; past the cap a page is told to
// close one rather than the machine being left to run them all.
func TestTooManyOpenLogsAreRefused(t *testing.T) {
	f := &fake{logText: "x\n", logHold: true}
	s, h := serve(t, f, Options{})
	url := "/api/universe/logs?multiverse=acme&universe=3&app=api&service=web&follow=1"

	ctx, cancel := contextWithCancel()
	defer cancel()
	done := make(chan struct{}, maxLogStreams)
	for i := 0; i < maxLogStreams; i++ {
		go func() {
			req := httptest.NewRequest("GET", url, nil).WithContext(ctx)
			req.Host = "localhost:7777"
			req.AddCookie(&http.Cookie{Name: cookieName, Value: testToken})
			h.ServeHTTP(httptest.NewRecorder(), req)
			done <- struct{}{}
		}()
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(s.logSlots) < maxLogStreams && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if rec := do(t, h, "GET", url, "", nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("past the cap = %d, want 429", rec.Code)
	}
	cancel()
	for i := 0; i < maxLogStreams; i++ {
		<-done
	}
}

// A runtime that is down is not a universe that is missing: the page tells a
// typo from docker being unreachable by the status it gets back.
func TestAReadThatFailsForAnotherReasonIsNotAFourOhFour(t *testing.T) {
	_, h := serve(t, &fake{err: errors.New("docker daemon is unreachable")}, Options{})
	if rec := do(t, h, "GET", "/api/universe/stats?multiverse=acme&universe=3", "", nil); rec.Code != http.StatusInternalServerError {
		t.Errorf("= %d, want 500", rec.Code)
	}
}

func TestTheStatsViewReadsEveryProjectAtOnce(t *testing.T) {
	f := &fake{fleetStats: fleetStatsView{At: "2026-01-01T00:00:00Z", Rows: []fleetStatRow{
		{Multiverse: "acme", Universe: "3", App: "api", Service: "web", statView: statView{Name: "acme-u3-api-web-1", CPU: 4, Mem: 1 << 20}},
	}, Sources: []fleetStatError{{Multiverse: "other", Error: "no manifest"}}}}
	_, h := serve(t, f, Options{ReadOnly: true})
	rec := do(t, h, "GET", "/api/stats", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("= %d %s", rec.Code, rec.Body)
	}
	for _, want := range []string{`"multiverse":"acme"`, `"universe":"3"`, `"mem":1048576`, `"sources":[{"multiverse":"other"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("missing %s in %s", want, rec.Body)
		}
	}
	if got := strings.Join(f.asked, ","); !strings.Contains(got, "fleetstats:") {
		t.Errorf("asked %s", got)
	}
}

func TestRestartNeedsItsAppAndService(t *testing.T) {
	_, h := serve(t, &fake{}, Options{})
	rec := do(t, h, "POST", "/api/ops", `{"action":"restart","multiverse":"acme","universe":"3"}`, map[string]string{"X-Mvctl": "1"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("= %d, want 400", rec.Code)
	}
}

func contextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

func TestTheCodeOfAUniverseIsItsOwnRequest(t *testing.T) {
	f := &fake{code: codeView{Rows: []codeRow{
		{Repo: "api", Branch: "u3", Follows: "main", Behind: 2,
			Work:   &diffView{Files: 4, Added: 120, Removed: 30},
			Review: &reviewView{Number: 12, Mark: "#", Title: "Keep the tax column", URL: "https://example.test/pull/12"}},
	}}}
	_, h := serve(t, f, Options{ReadOnly: true})
	rec := do(t, h, "GET", "/api/universe/code?multiverse=acme&universe=3", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("= %d %s", rec.Code, rec.Body)
	}
	for _, want := range []string{`"repo":"api"`, `"branch":"u3"`, `"follows":"main"`, `"added":120`, `"mark":"#"`, `"number":12`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("missing %s in %s", want, rec.Body)
		}
	}
	if got := strings.Join(f.asked, ","); !strings.Contains(got, "code:acme:3") {
		t.Errorf("asked %s", got)
	}
}

func TestTheCodeOfNoUniverseIsNotARequest(t *testing.T) {
	_, h := serve(t, &fake{}, Options{})
	if rec := do(t, h, "GET", "/api/universe/code?multiverse=acme", "", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("= %d %s", rec.Code, rec.Body)
	}
}
