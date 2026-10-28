package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// readUntil reads events until one of the type arrives; the first messages of a
// connection are the state of the machine.
func readUntil(t *testing.T, c *websocket.Conn, typ string) map[string]string {
	t.Helper()
	for range 40 {
		if e := read(t, c); e.Type == typ {
			var d map[string]string
			if err := json.Unmarshal(e.Data.(json.RawMessage), &d); err != nil {
				t.Fatal(err)
			}
			return d
		}
	}
	t.Fatalf("no %s event", typ)
	return nil
}

func send(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

// A log is read over the socket: its text arrives in pieces for the page that asked,
// then its end.
func TestALogIsReadOverTheWebsocket(t *testing.T) {
	f := &fake{logText: "first line\nsecond line\n"}
	s, _ := serve(t, f, Options{})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	c, _, err := dial(t, ts, signedIn())
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	send(t, c, map[string]any{"type": "logs", "id": "l1", "multiverse": "acme", "universe": "1", "app": "api", "service": "web", "tail": 100})
	got := readUntil(t, c, "log")
	if got["id"] != "l1" || !strings.Contains(got["text"], "first line") {
		t.Fatalf("log = %v", got)
	}
	end := readUntil(t, c, "logEnd")
	if end["id"] != "l1" || end["error"] != "" {
		t.Fatalf("end = %v", end)
	}
	if !strings.Contains(f.asks(), "logs:acme:1:api/web:100:false") {
		t.Errorf("backend was asked: %s", f.asks())
	}
}

// A followed log goes on until the page stops it, and stopping frees its place.
func TestAFollowedLogGoesOnUntilThePageStopsIt(t *testing.T) {
	f := &fake{logText: "hello\n", logHold: true}
	s, _ := serve(t, f, Options{})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	c, _, err := dial(t, ts, signedIn())
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	send(t, c, map[string]any{"type": "logs", "id": "f1", "multiverse": "acme", "universe": "1", "app": "api", "service": "web", "follow": true})
	readUntil(t, c, "log")
	if len(s.logSlots) != 1 {
		t.Fatalf("a followed log should hold a slot: %d", len(s.logSlots))
	}
	send(t, c, map[string]any{"type": "logs.stop", "id": "f1"})
	wait(t, "the slot to be freed", func() bool { return len(s.logSlots) == 0 })
}

// What cannot be read is said; a page cannot open more than a few at once.
func TestALogThatCannotBeStartedSaysWhy(t *testing.T) {
	s, _ := serve(t, &fake{logHold: true, logText: "x\n"}, Options{})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	c, _, err := dial(t, ts, signedIn())
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	send(t, c, map[string]any{"type": "logs", "id": "bad"}) // no multiverse, universe, app or service
	if end := readUntil(t, c, "logEnd"); end["id"] != "bad" || !strings.Contains(end["error"], "required") {
		t.Fatalf("end = %v", end)
	}
	for i := range maxLogsPerPage {
		send(t, c, map[string]any{"type": "logs", "id": "s" + string(rune('a'+i)), "multiverse": "acme", "universe": "1", "app": "api", "service": "web", "follow": true})
	}
	send(t, c, map[string]any{"type": "logs", "id": "one-too-many", "multiverse": "acme", "universe": "1", "app": "api", "service": "web", "follow": true})
	for range 40 {
		e := readUntil(t, c, "logEnd")
		if e["id"] == "one-too-many" {
			if !strings.Contains(e["error"], "several logs") {
				t.Fatalf("end = %v", e)
			}
			return
		}
	}
	t.Fatal("the fifth log was not refused")
}
