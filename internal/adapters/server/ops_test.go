package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"
)

// An update has no multiverse, ends as done and then restarts the server once.
func TestSelfUpdateEndsDoneAndRestartsTheServer(t *testing.T) {
	restarted := make(chan struct{}, 2)
	_, h := serve(t, &fake{}, Options{Restart: func() { restarted <- struct{}{} }})
	rec := do(t, h, "POST", "/api/ops", `{"action":"selfupdate"}`, map[string]string{"X-Mvctl": "1"})
	if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Fatalf("starting it: %d %s", rec.Code, rec.Body)
	}
	select {
	case <-restarted:
	case <-time.After(3 * time.Second):
		t.Fatal("the server was not restarted")
	}
	var seen opView
	for range 50 {
		var body struct{ Ops []opView }
		_ = json.Unmarshal(do(t, h, "GET", "/api/ops", "", nil).Body.Bytes(), &body)
		if len(body.Ops) > 0 {
			seen = body.Ops[len(body.Ops)-1]
		}
		if seen.State != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if seen.State != "done" || seen.Title != "Update multiversectl" {
		t.Fatalf("the operation: %+v", seen)
	}
}

func TestRestartArgsKeepTheAddressItIsOnAndDropOpen(t *testing.T) {
	got := restartArgs([]string{"--addr", "127.0.0.1:0", "--open", "--read-only", "-addr=1.2.3.4:1", "--expose"}, "127.0.0.1:7791")
	want := []string{"--read-only", "--expose", "--addr", "127.0.0.1:7791"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The update replaces the program, so it does not start while anything else
// runs, nothing starts while it does, and nothing starts once it has finished.
func TestSelfUpdateWaitsForOtherOperationsAndNothingStartsAfterIt(t *testing.T) {
	hold := make(chan struct{})
	f := &fake{hold: hold}
	_, h := serve(t, f, Options{Restart: func() {}})
	hdr := map[string]string{"X-Mvctl": "1"}
	if rec := do(t, h, "POST", "/api/ops", `{"action":"up","multiverse":"acme","universe":"1"}`, hdr); rec.Code != http.StatusAccepted {
		t.Fatalf("up: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "POST", "/api/ops", `{"action":"selfupdate"}`, hdr); rec.Code != http.StatusConflict {
		t.Fatalf("an update while something runs: %d %s", rec.Code, rec.Body)
	}
	close(hold)
	for range 100 {
		if rec := do(t, h, "POST", "/api/ops", `{"action":"selfupdate"}`, hdr); rec.Code == http.StatusAccepted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if rec := do(t, h, "POST", "/api/ops", `{"action":"up","multiverse":"acme","universe":"2"}`, hdr); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("after the update: %d %s", rec.Code, rec.Body)
	}
}

// Creating a universe is listed with the universe it makes, once it has a name.
func TestACreatedUniverseIsNamedOnItsOperation(t *testing.T) {
	_, h := serve(t, &fake{}, Options{})
	hdr := map[string]string{"X-Mvctl": "1"}
	rec := do(t, h, "POST", "/api/ops", `{"action":"new","multiverse":"acme","name":" 7 "}`, hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("new: %d %s", rec.Code, rec.Body)
	}
	var v opView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || v.Universe != "7" {
		t.Fatalf("the operation is about %q (%v), want 7", v.Universe, err)
	}
}

// A universe made without a name is listed under the number the server picked.
func TestAnOperationLearnsTheNumberTheServerPicked(t *testing.T) {
	_, h := serve(t, &fake{}, Options{})
	if rec := do(t, h, "POST", "/api/ops", `{"action":"new","multiverse":"acme"}`, map[string]string{"X-Mvctl": "1"}); rec.Code != http.StatusAccepted {
		t.Fatalf("new: %d %s", rec.Code, rec.Body)
	}
	var last opView
	for range 100 {
		var body struct{ Ops []opView }
		_ = json.Unmarshal(do(t, h, "GET", "/api/ops", "", nil).Body.Bytes(), &body)
		if len(body.Ops) > 0 {
			last = body.Ops[len(body.Ops)-1]
		}
		if last.State == "done" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if last.State != "done" || last.Universe != "7" {
		t.Fatalf("the operation: %+v", last)
	}
}
