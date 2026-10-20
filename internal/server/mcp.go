package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/xpier/xissue/internal/knowledge"
)

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	var req mcpRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	resp := s.ProcessMCPRequest(r.Context(), req)
	if resp == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ProcessMCPRequest processes a single MCP JSON-RPC 2.0 request against the Server.
func (s *Server) ProcessMCPRequest(ctx context.Context, req mcpRequest) *mcpResponse {
	// Notifications have no ID and expect no response.
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return nil
	}
	reply := func(res any) *mcpResponse {
		return &mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: res}
	}
	fail := func(code int, msg string) *mcpResponse {
		return &mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: &mcpError{Code: code, Message: msg}}
	}

	switch req.Method {
	case "initialize":
		return reply(map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "xissue-mcp",
				"version": Version,
			},
		})
	case "ping":
		return reply(map[string]any{})
	case "tools/list":
		return reply(map[string]any{
			"tools": mcpToolDefinitions(),
		})
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fail(-32602, "invalid params: "+err.Error())
		}
		text, isErr := s.callMCPTool(ctx, params.Name, params.Arguments)
		return reply(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
			"isError": isErr,
		})
	default:
		return fail(-32601, "method not found: "+req.Method)
	}
}

func mcpToolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "xissue_list_projects",
			"description": "List all xIssue projects and their configured git repositories.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "xissue_list_issues",
			"description": "List issues in an xIssue project, including status, PR URL, and PR state.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"projectId": map[string]any{"type": "number", "description": "Project ID (default: 1 if omitted)"},
				},
			},
		},
		{
			"name":        "xissue_create_issue",
			"description": "Create a new task/issue in xIssue Todo pool, and optionally auto-start an AI Agent run immediately.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"title"},
				"properties": map[string]any{
					"projectId": map[string]any{"type": "number", "description": "Project ID (if omitted, uses first project)"},
					"title":     map[string]any{"type": "string", "description": "Issue title"},
					"body":      map[string]any{"type": "string", "description": "Markdown description / acceptance criteria"},
					"autoRun":   map[string]any{"type": "boolean", "description": "If true, immediately start an Agent run on this issue"},
					"agent":     map[string]any{"type": "string", "description": "Optional agent name (e.g. opencode, omp, claude, codex)"},
					"model":     map[string]any{"type": "string", "description": "Optional model ID (e.g. openai/gpt-5.2-codex)"},
				},
			},
		},
		{
			"name":        "xissue_run_issue",
			"description": "Start an AI Agent run for an existing issue in xIssue.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"issueId"},
				"properties": map[string]any{
					"issueId": map[string]any{"type": "number", "description": "Issue ID"},
					"agent":   map[string]any{"type": "string", "description": "Agent name (optional, defaults to first available agent)"},
					"model":   map[string]any{"type": "string", "description": "Optional model ID"},
					"prompt":  map[string]any{"type": "string", "description": "Optional extra instructions"},
				},
			},
		},
		{
			"name":        "xissue_get_issue",
			"description": "Get full details of an issue, including its runs, PR URL/state, and change record.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"issueId"},
				"properties": map[string]any{
					"issueId": map[string]any{"type": "number", "description": "Issue ID"},
				},
			},
		},
		{
			"name":        "xissue_list_docs",
			"description": "List all documentation in the project knowledge base, including GitHub Wiki pages and local markdown docs (README, architecture, specs, docs/).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"projectId": map[string]any{"type": "number", "description": "Project ID (optional, defaults to first project)"},
				},
			},
		},
		{
			"name":        "xissue_get_doc",
			"description": "Get full markdown content of a document from the project knowledge base by its docId.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"docId"},
				"properties": map[string]any{
					"docId":     map[string]any{"type": "string", "description": "Document ID from xissue_list_docs or xissue_search_docs (e.g. 'wiki:1:Home.md' or 'local:1:docs/design.md')"},
					"projectId": map[string]any{"type": "number", "description": "Project ID (optional, defaults to first project)"},
				},
			},
		},
		{
			"name":        "xissue_search_docs",
			"description": "Full-text search in the project knowledge base (GitHub Wiki + local docs) to find architecture, API design, workflows, and specifications.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query":     map[string]any{"type": "string", "description": "Keywords or question terms to search for"},
					"projectId": map[string]any{"type": "number", "description": "Project ID (optional, defaults to first project)"},
					"limit":     map[string]any{"type": "number", "description": "Max number of results to return (default: 10)"},
				},
			},
		},
	}
}

func (s *Server) callMCPTool(ctx context.Context, name string, args map[string]any) (string, bool) {
	numArg := func(k string) int64 {
		switch v := args[k].(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		case int:
			return int64(v)
		}
		return 0
	}
	strArg := func(k string) string {
		if v, ok := args[k].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	boolArg := func(k string) bool {
		if v, ok := args[k].(bool); ok {
			return v
		}
		return false
	}

	switch name {
	case "xissue_list_projects":
		projs, err := s.st.ListProjects(ctx)
		if err != nil {
			return "error: " + err.Error(), true
		}
		b, _ := json.MarshalIndent(projs, "", "  ")
		return string(b), false

	case "xissue_list_issues":
		pid := numArg("projectId")
		if pid <= 0 {
			if projs, _ := s.st.ListProjects(ctx); len(projs) > 0 {
				pid = projs[0].ID
			}
		}
		issues, err := s.st.ListIssues(ctx, pid)
		if err != nil {
			return "error: " + err.Error(), true
		}
		b, _ := json.MarshalIndent(issues, "", "  ")
		return string(b), false

	case "xissue_create_issue":
		pid := numArg("projectId")
		if pid <= 0 {
			if projs, _ := s.st.ListProjects(ctx); len(projs) > 0 {
				pid = projs[0].ID
			} else {
				return "error: no project found in xIssue", true
			}
		}
		title := strArg("title")
		if title == "" {
			return "error: title is required", true
		}
		body := strArg("body")
		issue, err := s.st.CreateLocalIssue(ctx, pid, title, body, []string{"mcp"})
		if err != nil {
			return "error: " + err.Error(), true
		}
		s.runner.ExportIssueMarkdown(ctx, issue.ID)
		s.hub.BroadcastIssueUpdated(issue)

		out := map[string]any{"issue": issue}
		if boolArg("autoRun") {
			agent := strArg("agent")
			if agent == "" {
				for _, a := range s.runner.ListAgents() {
					if a.Available {
						agent = a.Name
						break
					}
				}
			}
			if agent != "" {
				run, rerr := s.runner.StartRun(ctx, issue.ID, agent, strArg("model"), "")
				if rerr != nil {
					out["runError"] = rerr.Error()
				} else {
					out["run"] = run
				}
			}
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return string(b), false

	case "xissue_run_issue":
		iid := numArg("issueId")
		if iid <= 0 {
			return "error: issueId is required", true
		}
		agent := strArg("agent")
		if agent == "" {
			for _, a := range s.runner.ListAgents() {
				if a.Available {
					agent = a.Name
					break
				}
			}
		}
		run, err := s.runner.StartRun(ctx, iid, agent, strArg("model"), strArg("prompt"))
		if err != nil {
			return "error: " + err.Error(), true
		}
		b, _ := json.MarshalIndent(run, "", "  ")
		return string(b), false

	case "xissue_get_issue":
		iid := numArg("issueId")
		issue, err := s.st.GetIssue(ctx, iid)
		if err != nil {
			return "error: " + err.Error(), true
		}
		runs, _ := s.st.ListRuns(ctx, iid)
		b, _ := json.MarshalIndent(map[string]any{
			"issue": issue,
			"runs":  runs,
		}, "", "  ")
		return string(b), false

	case "xissue_list_docs":
		pid := numArg("projectId")
		if pid <= 0 {
			if projs, _ := s.st.ListProjects(ctx); len(projs) > 0 {
				pid = projs[0].ID
			}
		}
		p, err := s.st.GetProject(ctx, pid)
		if err != nil {
			return "error: project not found: " + err.Error(), true
		}
		docs, err := knowledge.ScanProjectDocs(ctx, s.cfg.DataDir, p)
		if err != nil {
			return "error: " + err.Error(), true
		}
		b, _ := json.MarshalIndent(docs, "", "  ")
		return string(b), false

	case "xissue_get_doc":
		docID := strArg("docId")
		if docID == "" {
			return "error: docId is required", true
		}
		pid := numArg("projectId")
		if pid <= 0 {
			if projs, _ := s.st.ListProjects(ctx); len(projs) > 0 {
				pid = projs[0].ID
			}
		}
		p, err := s.st.GetProject(ctx, pid)
		if err != nil {
			return "error: project not found: " + err.Error(), true
		}
		detail, err := knowledge.GetDocDetail(ctx, s.cfg.DataDir, p, docID)
		if err != nil {
			return "error: " + err.Error(), true
		}
		b, _ := json.MarshalIndent(detail, "", "  ")
		return string(b), false

	case "xissue_search_docs":
		query := strArg("query")
		if query == "" {
			return "error: query is required", true
		}
		pid := numArg("projectId")
		if pid <= 0 {
			if projs, _ := s.st.ListProjects(ctx); len(projs) > 0 {
				pid = projs[0].ID
			}
		}
		p, err := s.st.GetProject(ctx, pid)
		if err != nil {
			return "error: project not found: " + err.Error(), true
		}
		limit := int(numArg("limit"))
		if limit <= 0 {
			limit = 10
		}
		results, err := knowledge.SearchDocs(ctx, s.cfg.DataDir, p, query, limit)
		if err != nil {
			return "error: " + err.Error(), true
		}
		b, _ := json.MarshalIndent(results, "", "  ")
		return string(b), false

	default:
		return fmt.Sprintf("unknown tool %q", name), true
	}
}

// ---------- One-Click MCP Installer ----------

type MCPTargetStatus struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ConfigPath string `json:"configPath"`
	Detected   bool   `json:"detected"`
	Installed  bool   `json:"installed"`
}

func xissueBinaryPath() string {
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe
	}
	return "xissue"
}

func (s *Server) handleMCPStatus(w http.ResponseWriter, r *http.Request) {
	bin := xissueBinaryPath()
	writeJSON(w, http.StatusOK, map[string]any{
		"binaryPath": bin,
		"httpUrl":    fmt.Sprintf("http://%s/api/mcp", s.cfg.Listen),
		"targets":    inspectMCPTargets(bin),
	})
}

func (s *Server) handleMCPInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Targets []string `json:"targets"` // optional; if empty, installs to all detected/supported JSON targets
	}
	_ = readJSON(r, &body)
	bin := xissueBinaryPath()
	installed, err := InstallMCPTargets(bin, body.Targets)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installed":  installed,
		"binaryPath": bin,
		"targets":    inspectMCPTargets(bin),
	})
}

func inspectMCPTargets(bin string) []MCPTargetStatus {
	home, _ := os.UserHomeDir()
	targets := []struct {
		id      string
		name    string
		relPath string
		binName string
	}{
		{"opencode", "OpenCode (~/.config/opencode/opencode.json)", ".config/opencode/opencode.json", "opencode"},
		{"claude", "Claude Code (~/.claude.json)", ".claude.json", "claude"},
		{"qoder", "Qoder CLI (~/.qoder-cn/mcp.json)", ".qoder-cn/mcp.json", "qoderclicn"},
		{"cursor", "Cursor IDE (~/.cursor/mcp.json)", ".cursor/mcp.json", "cursor"},
		{"windsurf", "Windsurf IDE (~/.codeium/windsurf/mcp_config.json)", ".codeium/windsurf/mcp_config.json", "windsurf"},
	}

	var out []MCPTargetStatus
	for _, t := range targets {
		p := filepath.Join(home, t.relPath)
		_, statErr := os.Stat(p)
		_, dirErr := os.Stat(filepath.Dir(p))
		_, binErr := exec.LookPath(t.binName)
		detected := statErr == nil || dirErr == nil || binErr == nil
		installed := false
		if b, err := os.ReadFile(p); err == nil {
			installed = strings.Contains(string(b), `"xissue"`)
		}
		out = append(out, MCPTargetStatus{
			ID:         t.id,
			Name:       t.name,
			ConfigPath: p,
			Detected:   detected,
			Installed:  installed,
		})
	}
	return out
}

// InstallMCPTargets injects the xissue MCP server into the specified (or all) local IDE/Agent config files.
func InstallMCPTargets(bin string, targetIDs []string) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range targetIDs {
		want[id] = true
	}
	should := func(id string) bool {
		return len(want) == 0 || want[id]
	}

	var installed []string

	// 1. OpenCode: ~/.config/opencode/opencode.json uses "mcp": { "xissue": { "type": "local", "command": [bin, "mcp"], "enabled": true } }
	if should("opencode") {
		p := filepath.Join(home, ".config/opencode/opencode.json")
		if err := injectJSONMap(p, "mcp", "xissue", map[string]any{
			"type":    "local",
			"command": []string{bin, "mcp"},
			"enabled": true,
		}); err == nil {
			installed = append(installed, "opencode")
		}
	}

	// 2. Claude Code: ~/.claude.json uses "mcpServers": { "xissue": { "type": "stdio", "command": bin, "args": ["mcp"] } }
	if should("claude") {
		p := filepath.Join(home, ".claude.json")
		if err := injectJSONMap(p, "mcpServers", "xissue", map[string]any{
			"type":    "stdio",
			"command": bin,
			"args":    []string{"mcp"},
		}); err == nil {
			installed = append(installed, "claude")
		}
	}

	// 3. Qoder CLI: ~/.qoder-cn/mcp.json uses "mcpServers": { "xissue": { "command": bin, "args": ["mcp"] } }
	if should("qoder") {
		p := filepath.Join(home, ".qoder-cn/mcp.json")
		if err := injectJSONMap(p, "mcpServers", "xissue", map[string]any{
			"command": bin,
			"args":    []string{"mcp"},
		}); err == nil {
			installed = append(installed, "qoder")
		}
	}

	// 4. Cursor: ~/.cursor/mcp.json uses "mcpServers": { "xissue": { "command": bin, "args": ["mcp"] } }
	if should("cursor") {
		p := filepath.Join(home, ".cursor/mcp.json")
		if err := injectJSONMap(p, "mcpServers", "xissue", map[string]any{
			"command": bin,
			"args":    []string{"mcp"},
		}); err == nil {
			installed = append(installed, "cursor")
		}
	}

	// 5. Windsurf: ~/.codeium/windsurf/mcp_config.json
	if should("windsurf") {
		p := filepath.Join(home, ".codeium/windsurf/mcp_config.json")
		if err := injectJSONMap(p, "mcpServers", "xissue", map[string]any{
			"command": bin,
			"args":    []string{"mcp"},
		}); err == nil {
			installed = append(installed, "windsurf")
		}
	}

	return installed, nil
}

func injectJSONMap(path, topKey, entryKey string, entryVal any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	root := map[string]any{}
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		if err := json.Unmarshal(b, &root); err != nil {
			return err
		}
	}
	sub, ok := root[topKey].(map[string]any)
	if !ok || sub == nil {
		sub = map[string]any{}
	}
	sub[entryKey] = entryVal
	root[topKey] = sub
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	var perm os.FileMode = 0o644
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
