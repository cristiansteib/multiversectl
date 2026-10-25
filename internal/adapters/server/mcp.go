package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The web server also speaks MCP, the protocol coding agents use to call tools,
// at /mcp. It is the same server and the same operations the page uses, so an
// agent and a person see each other's work live, and several agents can each
// work in a universe of their own at once. It answers over plain HTTP (one JSON
// reply to each POST, no stream), and what it can do is exactly what the page
// can: read-only servers offer the reading tools and nothing else.

const (
	mcpPath        = "/mcp"
	mcpMaxBody     = 1 << 20
	mcpDefaultWait = 45 * time.Second
	mcpMaxWait     = 110 * time.Second
	// mcpLogTail is how much of an operation's output a reply carries.
	mcpLogTail = 25
)

// mcpVersions are the protocol revisions understood, newest first. The client's
// is used when it is one of them.
var mcpVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

const mcpInstructions = `multiversectl runs several complete copies ("universes") of a multi-repository project side by side on this machine. Each universe has its own git worktree of every repository, its own branches, containers and hostnames, so several can run at once without colliding.

To work on something in isolation: call universe_open (name it after the branch or leave it empty for the next number). It creates the universe and starts it, and returns the directories to edit in (checkouts[].path) and the addresses (apps[].urls). Edit only inside those worktrees, never in the project's shared clones. Use a different universe for each task that should run at the same time; they do not wait for each other. When the work is done and committed, call universe_rm.

Orient yourself with project_context (where the project lives and what it is made of) and universe_list. A start can take minutes: if a tool reply says the operation is still running, call operation_status with its id. Secrets such as .env are copied into each universe from the project's clone.`

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (r rpcRequest) isNotification() bool { return len(r.ID) == 0 }

// mcpEndpoint answers one POST: a request, a notification or a batch of them.
func (s *Server) mcpEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// There is no server-initiated stream to offer.
		w.Header().Set("Allow", http.MethodPost)
		fail(w, http.StatusMethodNotAllowed, "this endpoint takes POST")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mcpMaxBody))
	if err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "the request is too large")
		return
	}
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		var batch []rpcRequest
		if err := json.Unmarshal(body, &batch); err != nil || len(batch) == 0 {
			replyWith(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}})
			return
		}
		var out []rpcResponse
		for _, req := range batch {
			if resp, ok := s.mcpHandle(r.Context(), req); ok {
				out = append(out, resp)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		reply(w, out)
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		replyWith(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}})
		return
	}
	resp, ok := s.mcpHandle(r.Context(), req)
	if !ok {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	reply(w, resp)
}

// mcpHandle answers one message; ok is false for a notification, which has no reply.
func (s *Server) mcpHandle(ctx context.Context, req rpcRequest) (rpcResponse, bool) {
	if req.isNotification() {
		return rpcResponse{}, false
	}
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := mcpVersions[0]
		for _, v := range mcpVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		resp.Result = map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "multiversectl", "version": s.o.Version},
			"instructions":    mcpInstructions,
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.mcpToolList()}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid parameters"}
			break
		}
		tool, ok := s.mcpFind(p.Name)
		if !ok {
			resp.Error = &rpcError{Code: -32602, Message: fmt.Sprintf("unknown tool %q", p.Name)}
			break
		}
		resp.Result = s.mcpCall(ctx, tool, mcpArgs(p.Arguments))
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp, true
}

// mcpCall runs a tool and shapes what it returns. A failure of the tool itself
// is a result the agent can read and act on, not a protocol error.
func (s *Server) mcpCall(ctx context.Context, t mcpTool, a mcpArgs) map[string]any {
	text := func(v any, isErr bool) map[string]any {
		var body string
		switch x := v.(type) {
		case string:
			body = x
		default:
			data, _ := json.MarshalIndent(x, "", "  ")
			body = string(data)
		}
		out := map[string]any{"content": []map[string]any{{"type": "text", "text": body}}}
		if isErr {
			out["isError"] = true
		} else if _, isString := v.(string); !isString {
			out["structuredContent"] = v
		}
		return out
	}
	if t.write && s.o.ReadOnly {
		return text("this server was started read-only, so it cannot change anything", true)
	}
	v, err := t.run(s, ctx, a)
	if err != nil {
		return text(err.Error(), true)
	}
	return text(v, false)
}

type mcpArgs map[string]any

func (a mcpArgs) str(key string) string {
	v, _ := a[key].(string)
	return strings.TrimSpace(v)
}

func (a mcpArgs) flag(key string) bool {
	v, _ := a[key].(bool)
	return v
}

// wait is how long a tool may block before it hands back a running operation:
// agents' clients time out a call that takes minutes.
func (a mcpArgs) wait() time.Duration {
	n, ok := a["wait_seconds"].(float64)
	if !ok {
		return mcpDefaultWait
	}
	d := time.Duration(n * float64(time.Second))
	if d < 0 {
		return 0
	}
	if d > mcpMaxWait {
		return mcpMaxWait
	}
	return d
}

type mcpTool struct {
	name, desc string
	schema     string // the JSON schema of the arguments
	write      bool   // changes something
	destroys   bool   // and what it changes cannot be undone
	run        func(s *Server, ctx context.Context, a mcpArgs) (any, error)
}

func (s *Server) mcpFind(name string) (mcpTool, bool) {
	for _, t := range mcpTools {
		if t.name == name {
			return t, true
		}
	}
	return mcpTool{}, false
}

func (s *Server) mcpToolList() []map[string]any {
	out := []map[string]any{}
	for _, t := range mcpTools {
		if t.write && s.o.ReadOnly {
			continue
		}
		var schema any
		_ = json.Unmarshal([]byte(t.schema), &schema)
		out = append(out, map[string]any{
			"name": t.name, "description": t.desc, "inputSchema": schema,
			"annotations": map[string]any{"readOnlyHint": !t.write, "destructiveHint": t.destroys, "openWorldHint": false},
		})
	}
	return out
}

const (
	projectProp = `"project": {"type": "string", "description": "The installed project's name. Omit for the current project."}`
	waitProp    = `"wait_seconds": {"type": "number", "description": "How long to wait for the operation before returning it still running (default 45, at most 110); then use operation_status."}`
)

var mcpTools = []mcpTool{
	{
		name:   "project_context",
		desc:   "Where every installed project lives and what it is made of: its root, the definition file (multiverse.yaml), where it was installed from, the shared clones of its repositories (which runtime each has), its apps and addresses. Start here to learn the layout.",
		schema: `{"type":"object","properties":{` + projectProp + `}}`,
		run:    (*Server).toolProjectContext,
	},
	{
		name:   "universe_list",
		desc:   "The universes of a project: each one's directory, branches, whether it is running, the worktree of every repository (checkouts[].path, where code is edited) and every address (apps[].urls).",
		schema: `{"type":"object","properties":{` + projectProp + `}}`,
		run: func(s *Server, ctx context.Context, a mcpArgs) (any, error) {
			return s.b.Fleet(ctx, a.str("project"))
		},
	},
	{
		name:   "universe_next_number",
		desc:   "The number a new universe would get, and the numbers passed over because a branch of that name is left behind.",
		schema: `{"type":"object","properties":{` + projectProp + `}}`,
		run: func(s *Server, ctx context.Context, a mcpArgs) (any, error) {
			p, err := s.mcpProject(ctx, a)
			if err != nil {
				return nil, err
			}
			return s.b.NextFree(ctx, p)
		},
	},
	{
		name:   "branches",
		desc:   "The branches across a project's repositories, and which repositories have each. Use it to pick a branch for universe_open.",
		schema: `{"type":"object","properties":{` + projectProp + `}}`,
		run: func(s *Server, ctx context.Context, a mcpArgs) (any, error) {
			p, err := s.mcpProject(ctx, a)
			if err != nil {
				return nil, err
			}
			return s.b.Branches(ctx, p)
		},
	},
	{
		name:   "doctor",
		desc:   "Check the machine, the project and, if named, one universe: runtime, proxy, secrets, hostnames, containers. Say what is wrong when something does not start.",
		schema: `{"type":"object","properties":{` + projectProp + `,"universe":{"type":"string","description":"One universe to check; omit to check the project."}}}`,
		run: func(s *Server, ctx context.Context, a mcpArgs) (any, error) {
			p, err := s.mcpProject(ctx, a)
			if err != nil {
				return nil, err
			}
			return s.b.Doctor(ctx, p, a.str("universe"))
		},
	},
	{
		name:   "catalog",
		desc:   "The catalog tree: the projects that exist and which are installed here.",
		schema: `{"type":"object","properties":{"fetch":{"type":"boolean","description":"Fetch the remotes first."}}}`,
		run: func(s *Server, ctx context.Context, a mcpArgs) (any, error) {
			return s.b.Catalog(ctx, a.flag("fetch"))
		},
	},
	{
		name:   "universe_open",
		desc:   "Create a universe and start it, in one call: the way to get an isolated, running copy of the project to work in. Give name to name it (a number, or the branch it is named after) and branch to create that branch in every repository, or give neither for the next free number. Returns the universe: the worktrees to edit in and the addresses. Safe to call from several agents at once, each gets its own universe.",
		write:  true,
		schema: `{"type":"object","properties":{` + projectProp + `,"name":{"type":"string","description":"The universe's number or name; empty for the next free number."},"branch":{"type":"string","description":"The new branch to create in every repository (it must not exist yet); defaults from the name."},"base":{"type":"string","description":"Where the new branches start, e.g. origin/main."},"note":{"type":"string","description":"Why the universe exists, shown in listings."},` + waitProp + `}}`,
		run:    (*Server).toolOpen,
	},
	{
		name:   "universe_new",
		desc:   "Create a universe on new branches, without starting it. Use universe_open to create and start.",
		write:  true,
		schema: `{"type":"object","properties":{` + projectProp + `,"name":{"type":"string"},"branch":{"type":"string"},"base":{"type":"string"},"note":{"type":"string"},` + waitProp + `}}`,
		run: func(s *Server, ctx context.Context, a mcpArgs) (any, error) {
			return s.mcpAction(ctx, a, opRequest{Action: "new", Name: a.str("name"), Branch: a.str("branch"), Base: a.str("base"), Note: a.str("note")})
		},
	},
	{
		name:   "universe_checkout",
		desc:   "Create a universe that works on an existing branch (a pull request's, say) instead of new ones, without starting it.",
		write:  true,
		schema: `{"type":"object","required":["branch"],"properties":{` + projectProp + `,"branch":{"type":"string","description":"The existing branch to check out."},"name":{"type":"string"},"only_existing":{"type":"boolean","description":"Leave repositories without the branch on their base."},` + waitProp + `}}`,
		run: func(s *Server, ctx context.Context, a mcpArgs) (any, error) {
			return s.mcpAction(ctx, a, opRequest{Action: "checkout", Name: a.str("name"), Branch: a.str("branch"), OnlyExisting: a.flag("only_existing")})
		},
	},
	{
		name:   "universe_up",
		desc:   "Start a universe's containers. Other universes keep running; they do not wait for this one.",
		write:  true,
		schema: `{"type":"object","required":["universe"],"properties":{` + projectProp + `,"universe":{"type":"string"},` + waitProp + `}}`,
		run: func(s *Server, ctx context.Context, a mcpArgs) (any, error) {
			return s.mcpAction(ctx, a, opRequest{Action: "up", Universe: a.str("universe")})
		},
	},
	{
		name:   "universe_down",
		desc:   "Stop a universe's containers and keep everything else: its worktrees, branches and data.",
		write:  true,
		schema: `{"type":"object","required":["universe"],"properties":{` + projectProp + `,"universe":{"type":"string"},` + waitProp + `}}`,
		run: func(s *Server, ctx context.Context, a mcpArgs) (any, error) {
			return s.mcpAction(ctx, a, opRequest{Action: "down", Universe: a.str("universe")})
		},
	},
	{
		name:     "universe_rm",
		desc:     "Remove a universe: its containers, volumes and worktrees. This cannot be undone. It refuses when a worktree has uncommitted work unless force is set, so commit or push first. confirm must be the universe's own name.",
		write:    true,
		destroys: true,
		schema:   `{"type":"object","required":["universe","confirm"],"properties":{` + projectProp + `,"universe":{"type":"string"},"confirm":{"type":"string","description":"The universe's name again, to confirm."},"force":{"type":"boolean","description":"Remove it even with uncommitted work, which is lost."},` + waitProp + `}}`,
		run: func(s *Server, ctx context.Context, a mcpArgs) (any, error) {
			return s.mcpAction(ctx, a, opRequest{Action: "rm", Universe: a.str("universe"), Confirm: a.str("confirm"), Force: a.flag("force")})
		},
	},
	{
		name:   "project_update",
		desc:   "Bring a project's manifest up to date with where it was installed from, and clone repositories it newly lists. Waits for everything running in the project to finish, and refuses a manifest with local edits.",
		write:  true,
		schema: `{"type":"object","properties":{` + projectProp + `,` + waitProp + `}}`,
		run: func(s *Server, ctx context.Context, a mcpArgs) (any, error) {
			return s.mcpAction(ctx, a, opRequest{Action: "update"})
		},
	},
	{
		name:   "project_use",
		desc:   "Make a project the current one, the one commands act on when no project is named.",
		write:  true,
		schema: `{"type":"object","required":["project"],"properties":{"project":{"type":"string"},` + waitProp + `}}`,
		run: func(s *Server, ctx context.Context, a mcpArgs) (any, error) {
			return s.mcpAction(ctx, a, opRequest{Action: "use"})
		},
	},
	{
		name:   "operation_status",
		desc:   "Where an operation that was still running stands. Waits up to wait_seconds for it to end, and returns its state and the end of its output.",
		schema: `{"type":"object","required":["id"],"properties":{"id":{"type":"string","description":"The operation id a tool returned."},"lines":{"type":"number","description":"How many lines of output to return (default 25)."},` + waitProp + `}}`,
		run: func(s *Server, ctx context.Context, a mcpArgs) (any, error) {
			o := s.ops.byID(a.str("id"))
			if o == nil {
				return nil, fmt.Errorf("no operation %q: it is either wrong or older than the last %d", a.str("id"), keepOps)
			}
			n := mcpLogTail
			if v, ok := a["lines"].(float64); ok && v > 0 {
				n = int(v)
			}
			s.ops.await(ctx, o, a.wait())
			return s.mcpResult(ctx, o, n, ""), nil
		},
	},
}

// mcpProject is the project a tool acts on: the one named, else the current one.
func (s *Server) mcpProject(ctx context.Context, a mcpArgs) (string, error) {
	if p := a.str("project"); p != "" {
		return p, nil
	}
	rep, err := s.b.Status(ctx, false)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(rep.Workspaces))
	for _, w := range rep.Workspaces {
		if w.Current {
			return w.Name, nil
		}
		names = append(names, w.Name)
	}
	switch len(names) {
	case 0:
		return "", errors.New("no project is installed on this machine: `multiversectl project install <owner/repo>`")
	case 1:
		return names[0], nil
	}
	sort.Strings(names)
	return "", fmt.Errorf("several projects are installed and none is current; pass project: one of %s", strings.Join(names, ", "))
}

func (s *Server) toolProjectContext(ctx context.Context, a mcpArgs) (any, error) {
	if p := a.str("project"); p != "" {
		return s.b.Workspace(ctx, p)
	}
	rep, err := s.b.Status(ctx, false)
	if err != nil {
		return nil, err
	}
	var out []workspaceView
	for _, w := range rep.Workspaces {
		v, err := s.b.Workspace(ctx, w.Name)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return map[string]any{"projects": out}, nil
}

// mcpAction starts an operation, queueing behind one that holds the same thing,
// and waits for it up to the time the caller allows.
func (s *Server) mcpAction(ctx context.Context, a mcpArgs, req opRequest) (any, error) {
	if req.Action == "use" {
		req.Workspace = a.str("project")
	} else {
		p, err := s.mcpProject(ctx, a)
		if err != nil {
			return nil, err
		}
		req.Workspace = p
	}
	deadline := time.Now().Add(a.wait())
	o, err := s.mcpStart(ctx, req, deadline)
	if err != nil {
		return nil, err
	}
	s.ops.await(ctx, o, time.Until(deadline))
	return s.mcpResult(ctx, o, mcpLogTail, req.Name), nil
}

// mcpStart begins an operation. When another holds what this one needs it waits
// for it, within the deadline, so agents that ask for things at the same moment
// take turns instead of failing.
func (s *Server) mcpStart(ctx context.Context, req opRequest, deadline time.Time) (*op, error) {
	for {
		v, oerr := s.ops.start(ctx, req)
		if oerr == nil {
			return s.ops.byID(v.ID), nil
		}
		busy := oerr.code == http.StatusConflict && oerr.body["dirty"] == nil
		if !busy || time.Now().After(deadline) {
			return nil, oerr
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// mcpResult is what an agent needs after an operation: where it stands, the end
// of its output, and, once a universe exists, the universe itself with its paths
// and addresses.
func (s *Server) mcpResult(ctx context.Context, o *op, lines int, named string) map[string]any {
	v := o.snapshot(true)
	tail := v.Lines
	if len(tail) > lines {
		tail = tail[len(tail)-lines:]
	}
	view := v
	view.Lines = nil
	out := map[string]any{"operation": view, "log": tail, "done": v.State != "running"}
	if v.State == "running" {
		out["next"] = "still running: call operation_status with id " + v.ID
		return out
	}
	if v.State != "done" {
		return out
	}
	name := v.Universe
	if name == "" {
		name = universeFromLog(v.Lines)
	}
	if name == "" {
		name = named
	}
	if name != "" && (v.Action == "new" || v.Action == "checkout" || v.Action == "up") {
		if u, ok := s.findUniverse(ctx, v.Workspace, name); ok {
			out["universe"] = u
		}
	}
	return out
}

var universeReady = regexp.MustCompile(`universe (\S+) ready`)

// universeFromLog is the name `new` reported, for when it picked a number.
func universeFromLog(lines []opLine) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if m := universeReady.FindStringSubmatch(lines[i].Text); m != nil {
			return m[1]
		}
	}
	return ""
}

func (s *Server) findUniverse(ctx context.Context, project, name string) (universeView, bool) {
	c, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	fv, err := s.b.Fleet(c, project)
	if err != nil {
		return universeView{}, false
	}
	for _, w := range fv.Workspaces {
		if w.Name != project {
			continue
		}
		for _, u := range w.Universes {
			if u.Name == name {
				return u, true
			}
		}
	}
	return universeView{}, false
}

// toolOpen creates a universe and starts it, within one budget of time.
func (s *Server) toolOpen(ctx context.Context, a mcpArgs) (any, error) {
	p, err := s.mcpProject(ctx, a)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(a.wait())
	create, err := s.mcpStart(ctx, opRequest{Action: "new", Workspace: p, Name: a.str("name"), Branch: a.str("branch"), Base: a.str("base"), Note: a.str("note")}, deadline)
	if err != nil {
		return nil, err
	}
	s.ops.await(ctx, create, time.Until(deadline))
	res := s.mcpResult(ctx, create, mcpLogTail, a.str("name"))
	if res["done"] != true || create.snapshot(false).State != "done" {
		return res, nil // still being created, or it failed: say so as it is
	}
	// What creating reported is the name to start: it is the number it picked
	// when none was given, and the name as the project spells it when one was.
	name := universeFromLog(create.snapshot(true).Lines)
	if name == "" {
		name = a.str("name")
	}
	if name == "" {
		return nil, errors.New("the universe was created but its name could not be read; call universe_list, then universe_up")
	}
	up, err := s.mcpStart(ctx, opRequest{Action: "up", Workspace: p, Universe: name}, deadline)
	if err != nil {
		return nil, err
	}
	s.ops.await(ctx, up, time.Until(deadline))
	out := s.mcpResult(ctx, up, mcpLogTail, name)
	out["created"] = create.snapshot(false)
	return out, nil
}
