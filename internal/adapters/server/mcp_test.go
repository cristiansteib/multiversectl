package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
)

var agent = map[string]string{"Cookie": "", "Authorization": "Bearer " + testToken, "Content-Type": "application/json"}

type rpcReply struct {
	Result map[string]any `json:"result"`
	Error  *rpcError      `json:"error"`
}

// rpc is what an agent's client does: one POST, one reply, signed with the header.
func rpc(t *testing.T, h http.Handler, method, params string) rpcReply {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `"`
	if params != "" {
		body += `,"params":` + params
	}
	rec := do(t, h, "POST", "/mcp", body+`}`, agent)
	if rec.Code != 200 {
		t.Fatalf("%s: %d %s", method, rec.Code, rec.Body)
	}
	var r rpcReply
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("%s: %v: %s", method, err, rec.Body)
	}
	return r
}

// call runs a tool and returns its structured result, its text and whether it failed.
func call(t *testing.T, h http.Handler, tool, args string) (map[string]any, string, bool) {
	t.Helper()
	r := rpc(t, h, "tools/call", `{"name":"`+tool+`","arguments":`+args+`}`)
	if r.Error != nil {
		t.Fatalf("%s: protocol error %+v", tool, r.Error)
	}
	content := r.Result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	structured, _ := r.Result["structuredContent"].(map[string]any)
	failed, _ := r.Result["isError"].(bool)
	return structured, text, failed
}

func TestMCPNeedsTheTokenAsAHeader(t *testing.T) {
	_, h := serve(t, &fake{}, Options{})
	for name, headers := range map[string]map[string]string{
		"nothing":      {"Cookie": ""},
		"wrong token":  {"Cookie": "", "Authorization": "Bearer nope"},
		"not a bearer": {"Cookie": "", "Authorization": testToken},
	} {
		rec := do(t, h, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, headers)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("%s: %d %v", name, rec.Code, rec.Header())
		}
	}
	// The page's cookie is not how a program signs in, and does not open it.
	rec := do(t, h, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{csrfHeader: csrfValue})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("the cookie alone: %d", rec.Code)
	}
	// It is still only for this machine.
	rec = do(t, h, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Cookie": "", "Authorization": "Bearer " + testToken, "Host": "evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("another host name: %d", rec.Code)
	}
	if rec := rpc(t, h, "ping", ""); rec.Error != nil {
		t.Errorf("ping: %+v", rec.Error)
	}
}

func TestMCPInitializeNegotiatesAndExplainsTheWorkflow(t *testing.T) {
	_, h := serve(t, &fake{}, Options{Version: "v9"})
	r := rpc(t, h, "initialize", `{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}`)
	if r.Result["protocolVersion"] != "2025-03-26" {
		t.Errorf("a version both speak is used: %v", r.Result["protocolVersion"])
	}
	if r = rpc(t, h, "initialize", `{"protocolVersion":"1999-01-01"}`); r.Result["protocolVersion"] != mcpVersions[0] {
		t.Errorf("an unknown version gets the newest: %v", r.Result["protocolVersion"])
	}
	instr, _ := r.Result["instructions"].(string)
	for _, want := range []string{"universe_open", "checkouts[].path", "operation_status", "never in the project's shared clones"} {
		if !strings.Contains(instr, want) {
			t.Errorf("instructions lack %q", want)
		}
	}
	if info := r.Result["serverInfo"].(map[string]any); info["version"] != "v9" {
		t.Errorf("serverInfo = %v", info)
	}

	// A notification has no reply; an unknown method is an error, not silence.
	rec := do(t, h, "POST", "/mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, agent)
	if rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
		t.Errorf("notification: %d %q", rec.Code, rec.Body)
	}
	if r := rpc(t, h, "resources/subscribe", ""); r.Error == nil || r.Error.Code != -32601 {
		t.Errorf("unknown method: %+v", r.Error)
	}
	// A batch is answered in one array.
	rec = do(t, h, "POST", "/mcp", `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`, agent)
	var batch []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil || len(batch) != 2 {
		t.Errorf("batch: %v %s", err, rec.Body)
	}
}

func TestMCPListsTheToolsAndHidesTheOnesThatChangeThingsWhenReadOnly(t *testing.T) {
	names := func(h http.Handler) map[string]bool {
		out := map[string]bool{}
		for _, tool := range rpc(t, h, "tools/list", "").Result["tools"].([]any) {
			m := tool.(map[string]any)
			out[m["name"].(string)] = true
			if m["inputSchema"].(map[string]any)["type"] != "object" || m["description"] == "" {
				t.Errorf("%v has no schema or description", m["name"])
			}
		}
		return out
	}
	_, h := serve(t, &fake{}, Options{})
	all := names(h)
	for _, want := range []string{"project_context", "universe_list", "universe_open", "universe_up", "universe_down", "universe_rm", "operation_status", "doctor", "branches"} {
		if !all[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	_, ro := serve(t, &fake{}, Options{ReadOnly: true})
	only := names(ro)
	if only["universe_open"] || only["universe_rm"] || !only["universe_list"] {
		t.Errorf("read-only offers %v", only)
	}
	// And a call to one anyway is refused, not run.
	f := &fake{}
	_, ro2 := serve(t, f, Options{ReadOnly: true})
	if _, text, failed := call(t, ro2, "universe_up", `{"project":"acme","universe":"1"}`); !failed || !strings.Contains(text, "read-only") || len(f.did) != 0 {
		t.Errorf("read-only call: failed=%v %q ran=%d", failed, text, len(f.did))
	}
}

func TestMCPReadsTheProjectsAndTheirUniverses(t *testing.T) {
	f := &fake{
		status: app.StatusReport{Workspaces: []app.WorkspaceStatus{{Name: "acme", Current: true}}},
		ws:     workspaceView{Name: "acme", Root: "/home/me/acme", Manifest: "/home/me/acme/.multiverse/workspace/multiverse.yaml", UniversesDir: "/home/me/acme/universes"},
		fleet:  fleetView{Workspaces: []fleetWorkspace{{Name: "acme", Universes: []universeView{{Name: "3", Checkouts: []checkoutView{{Repo: "api", Path: "/home/me/acme/universes/3/api"}}}}}}},
	}
	_, h := serve(t, f, Options{})
	got, _, failed := call(t, h, "project_context", `{}`)
	if failed || !strings.Contains(asJSON(got), `"manifest":"/home/me/acme/.multiverse/workspace/multiverse.yaml"`) || !strings.Contains(asJSON(got), `"universesDir":"/home/me/acme/universes"`) {
		t.Errorf("project_context: %v", got)
	}
	got, _, _ = call(t, h, "universe_list", `{"project":"acme"}`)
	if !strings.Contains(asJSON(got), `"path":"/home/me/acme/universes/3/api"`) {
		t.Errorf("universe_list: %v", got)
	}
	if !strings.Contains(f.asks(), "fleet:acme") {
		t.Errorf("asked %q", f.asks())
	}
}

func asJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestUniverseOpenCreatesAndStartsAndReturnsWhereToWork(t *testing.T) {
	f := &fake{
		status: app.StatusReport{Workspaces: []app.WorkspaceStatus{{Name: "acme", Current: true}}},
		fleet: fleetView{Workspaces: []fleetWorkspace{{Name: "acme", Universes: []universeView{{Name: "7",
			Checkouts: []checkoutView{{Repo: "api", Path: "/w/universes/7/api"}},
			Apps:      []appView{{Key: "api", URLs: []urlView{{Host: "api", URL: "http://api-u7.example.test"}}}}}}}}},
	}
	_, h := serve(t, f, Options{})
	got, text, failed := call(t, h, "universe_open", `{"branch":"fix/login","note":"login bug"}`)
	if failed {
		t.Fatal(text)
	}
	if len(f.did) != 2 || f.did[0].Action != "new" || f.did[0].Branch != "fix/login" || f.did[0].Workspace != "acme" ||
		f.did[1].Action != "up" || f.did[1].Universe != "7" {
		t.Fatalf("it must create, then start the universe it created: %+v", f.did)
	}
	js := asJSON(got)
	for _, want := range []string{`"done":true`, `"path":"/w/universes/7/api"`, `"url":"http://api-u7.example.test"`, `"created"`} {
		if !strings.Contains(js, want) {
			t.Errorf("result lacks %s: %s", want, js)
		}
	}
}

func TestSeveralAgentsOpenTheirOwnUniversesAtOnce(t *testing.T) {
	f := &fake{status: app.StatusReport{Workspaces: []app.WorkspaceStatus{{Name: "acme", Current: true}}}}
	_, h := serve(t, f, Options{})
	var wg sync.WaitGroup
	results := make([]string, 4)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, text, failed := call(t, h, "universe_open", `{}`)
			if failed {
				results[i] = "FAILED: " + text
			}
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if r != "" {
			t.Errorf("agent %d: %s", i, r)
		}
	}
	creates, starts := 0, 0
	for _, r := range f.did {
		switch r.Action {
		case "new":
			creates++
		case "up":
			starts++
		}
	}
	if creates != 4 || starts != 4 {
		t.Errorf("each agent creates and starts its own: %d new, %d up", creates, starts)
	}
}

func TestALongStartHandsBackTheOperationToFollow(t *testing.T) {
	f := &fake{hold: make(chan struct{})}
	_, h := serve(t, f, Options{})
	got, _, failed := call(t, h, "universe_up", `{"project":"acme","universe":"1","wait_seconds":0.3}`)
	if failed || got["done"] != false || !strings.Contains(asJSON(got), "operation_status") {
		t.Fatalf("a start that outlasts the wait is returned running with how to follow it: %v", got)
	}
	id := got["operation"].(map[string]any)["id"].(string)

	close(f.hold)
	got, _, failed = call(t, h, "operation_status", `{"id":"`+id+`","wait_seconds":5}`)
	if failed || got["done"] != true || got["operation"].(map[string]any)["state"] != "done" {
		t.Errorf("after it finished: %v", got)
	}
	if !strings.Contains(asJSON(got["log"]), "done up") {
		t.Errorf("the output comes back too: %v", got["log"])
	}
	if _, text, failed := call(t, h, "operation_status", `{"id":"op999"}`); !failed || !strings.Contains(text, "no operation") {
		t.Errorf("unknown id: %q", text)
	}
}

func TestRemovingNeedsTheNameAndRefusesUncommittedWork(t *testing.T) {
	f := &fake{dirty: []string{"api"}}
	_, h := serve(t, f, Options{})
	if _, text, failed := call(t, h, "universe_rm", `{"project":"acme","universe":"3","confirm":"4"}`); !failed || !strings.Contains(text, "confirm") {
		t.Errorf("a wrong confirmation: %v %q", failed, text)
	}
	if _, text, failed := call(t, h, "universe_rm", `{"project":"acme","universe":"3","confirm":"3"}`); !failed || !strings.Contains(text, "uncommitted work in api") {
		t.Errorf("uncommitted work: %v %q", failed, text)
	}
	if len(f.did) != 0 {
		t.Error("nothing may run before it is safe")
	}
	if _, text, failed := call(t, h, "universe_rm", `{"project":"acme","universe":"3","confirm":"3","force":true}`); failed {
		t.Errorf("forced: %q", text)
	}
}

func TestAProjectIsNeededWhenThereIsNoCurrentOne(t *testing.T) {
	f := &fake{status: app.StatusReport{Workspaces: []app.WorkspaceStatus{{Name: "a"}, {Name: "b"}}}}
	_, h := serve(t, f, Options{})
	if _, text, failed := call(t, h, "universe_next_number", `{}`); !failed || !strings.Contains(text, "pass project: one of a, b") {
		t.Errorf("%v %q", failed, text)
	}
	_ = httptest.NewRecorder
}
