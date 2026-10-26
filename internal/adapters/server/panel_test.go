package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestThePanelReadsContainersAndStats(t *testing.T) {
	f := &fake{
		containers: []containerView{{App: "api", Service: "web", Name: "acme-u3-api-web-1", Status: "Up 2 hours", Running: true}},
		stats:      []statView{{Name: "acme-u3-api-web-1", CPU: 1.5, Memory: 20, MemUsage: "50MiB / 2GiB"}},
	}
	_, h := serve(t, f, Options{})

	rec := do(t, h, "GET", "/api/universe/containers?project=acme&universe=3", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"acme-u3-api-web-1"`) {
		t.Fatalf("containers: %d %s", rec.Code, rec.Body)
	}
	rec = do(t, h, "GET", "/api/universe/stats?project=acme&universe=3", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"memUsage":"50MiB / 2GiB"`) {
		t.Fatalf("stats: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "GET", "/api/universe/containers?project=acme", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a missing universe = %d, want 400", rec.Code)
	}
}

// The panel only reads, so --read-only keeps it; restarting is an action, and
// that stays refused there.
func TestThePanelWorksReadOnlyButRestartDoesNot(t *testing.T) {
	f := &fake{containers: []containerView{{App: "api", Service: "web", Name: "n", Running: true}}}
	_, h := serve(t, f, Options{ReadOnly: true})
	if rec := do(t, h, "GET", "/api/universe/containers?project=acme&universe=3", "", nil); rec.Code != http.StatusOK {
		t.Errorf("read-only containers = %d", rec.Code)
	}
	body := `{"action":"restart","project":"acme","universe":"3","app":"api","service":"web"}`
	if rec := do(t, h, "POST", "/api/ops", body, map[string]string{"X-Mvctl": "1"}); rec.Code == http.StatusAccepted {
		t.Errorf("a restart was accepted in read-only mode")
	}
}

func TestLogsAreStreamedAsText(t *testing.T) {
	f := &fake{logText: "line one\nline two\n"}
	_, h := serve(t, f, Options{})
	rec := do(t, h, "GET", "/api/universe/logs?project=acme&universe=3&app=api&service=web&tail=50", "", nil)
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
	f := &fake{err: errors.New("universe 3 has no container for api/nope")}
	_, h := serve(t, f, Options{})
	rec := do(t, h, "GET", "/api/universe/logs?project=acme&universe=3&app=api&service=nope", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("= %d, want 404", rec.Code)
	}
}

// Followed logs hold a docker process each; past the cap a page is told to
// close one rather than the machine being left to run them all.
func TestTooManyOpenLogsAreRefused(t *testing.T) {
	f := &fake{logText: "x\n", logHold: true}
	s, h := serve(t, f, Options{})
	url := "/api/universe/logs?project=acme&universe=3&app=api&service=web&follow=1"

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

func TestRestartNeedsItsAppAndService(t *testing.T) {
	_, h := serve(t, &fake{}, Options{})
	rec := do(t, h, "POST", "/api/ops", `{"action":"restart","project":"acme","universe":"3"}`, map[string]string{"X-Mvctl": "1"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("= %d, want 400", rec.Code)
	}
}

func contextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}
