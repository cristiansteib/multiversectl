package server

import (
	"encoding/json"
	"net/http"
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
