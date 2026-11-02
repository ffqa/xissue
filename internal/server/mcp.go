package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/knowledge"
	"github.com/xpier/xissue/internal/runner"
	"github.com/xpier/xissue/internal/store"
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

// handleMCP implements the MCP Streamable HTTP transport. A single xIssue
// process can therefore serve every IDE/agent session; clients that support
// HTTP do not need to spawn one `xissue mcp` stdio bridge per session.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		// Stateless server: there is no per-client session to tear down.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accept := strings.ToLower(r.Header.Get("Accept"))
	acceptJSON := accept == "" || strings.Contains(accept, "application/json") || strings.Contains(accept, "*/*")
	acceptSSE := strings.Contains(accept, "text/event-stream")
	if !acceptJSON && !acceptSSE {
		http.Error(w, "not acceptable: use application/json or text/event-stream", http.StatusNotAcceptable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		http.Error(w, "empty request body", http.StatusBadRequest)
		return
	}

	isBatch := body[0] == '['
	var reqs []mcpRequest
	if isBatch {
		if err := json.Unmarshal(body, &reqs); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	} else {
		var req mcpRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		reqs = []mcpRequest{req}
	}

	var resps []*mcpResponse
	for _, req := range reqs {
		if resp := s.ProcessMCPRequest(r.Context(), req); resp != nil {
			resps = append(resps, resp)
		}
	}
	if len(resps) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Prefer a plain JSON response when the client accepts it; fall back to a
	// one-shot SSE event for clients that only advertise text/event-stream.
	if acceptJSON {
		if isBatch {
			writeJSON(w, http.StatusOK, resps)
			return
		}
		writeJSON(w, http.StatusOK, resps[0])
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	payload, _ := json.Marshal(resps)
	if !isBatch {
		payload, _ = json.Marshal(resps[0])
	}
	_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func negotiateMCPProtocolVersion(raw json.RawMessage) string {
	const latest = "2025-06-18"
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return latest
	}
	switch p.ProtocolVersion {
	case "2024-11-05", "2025-03-26", "2025-06-18":
		return p.ProtocolVersion
	default:
		return latest
	}
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
			"protocolVersion": negotiateMCPProtocolVersion(req.Params),
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
		{
			"name":        "xissue_list_project_todos",
			"description": "List a project's persistent TODO checklist (survives archived/deleted issues). Supports search and status filter.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"projectId": map[string]any{"type": "number", "description": "Project ID (default: first project)"},
					"q":         map[string]any{"type": "string", "description": "Search title/detail/source key"},
					"status":    map[string]any{"type": "string", "enum": []string{"open", "done"}, "description": "Filter by status (omitted = all)"},
				},
			},
		},
		{
			"name":        "xissue_add_todo",
			"description": "Add an item to a project's TODO checklist (e.g. one feature decomposed from a spec).",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"title"},
				"properties": map[string]any{
					"projectId":     map[string]any{"type": "number", "description": "Project ID (default: first project)"},
					"title":         map[string]any{"type": "string", "description": "TODO title"},
					"detail":        map[string]any{"type": "string", "description": "Optional acceptance notes"},
					"sourceIssueId": map[string]any{"type": "number", "description": "Issue this item came from (optional)"},
					"sourceKey":     map[string]any{"type": "string", "description": "Issue key snapshot, e.g. ffqa/h5#12 (optional)"},
				},
			},
		},
		{
			"name":        "xissue_add_todos",
			"description": "Bulk-create TODO checklist items for a project (e.g. decompose a spec in one call). Existing items are untouched.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"items"},
				"properties": map[string]any{
					"projectId": map[string]any{"type": "number", "description": "Project ID (default: first project)"},
					"items": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":     "object",
							"required": []string{"title"},
							"properties": map[string]any{
								"title":         map[string]any{"type": "string"},
								"detail":        map[string]any{"type": "string"},
								"sourceIssueId": map[string]any{"type": "number"},
								"sourceKey":     map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
		{
			"name":        "xissue_update_todo",
			"description": "Update a TODO item by id: mark it done/open, rename it, or edit its detail.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"todoId"},
				"properties": map[string]any{
					"todoId": map[string]any{"type": "number", "description": "TODO id"},
					"status": map[string]any{"type": "string", "enum": []string{"open", "done"}},
					"title":  map[string]any{"type": "string"},
					"detail": map[string]any{"type": "string"},
				},
			},
		},
		{
			"name":        "xissue_delete_todo",
			"description": "Delete a TODO item by id.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"todoId"},
				"properties": map[string]any{
					"todoId": map[string]any{"type": "number", "description": "TODO id"},
				},
			},
		},
		{
			"name":        "xissue_add_test_plans",
			"description": "Bulk-create test plans for a project. Repeats of the same title are numbered x1, x2, … automatically.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"items"},
				"properties": map[string]any{
					"projectId": map[string]any{"type": "number", "description": "Project ID (default: first project)"},
					"items": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":     "object",
							"required": []string{"title"},
							"properties": map[string]any{
								"title":         map[string]any{"type": "string"},
								"detail":        map[string]any{"type": "string"},
								"sourceIssueId": map[string]any{"type": "number"},
								"sourceKey":     map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
		{
			"name":        "xissue_update_test_plan",
			"description": "Update a test plan by id: mark it done/open or edit its detail.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"planId"},
				"properties": map[string]any{
					"planId": map[string]any{"type": "number", "description": "Test plan id"},
					"status": map[string]any{"type": "string", "enum": []string{"open", "done"}},
					"detail": map[string]any{"type": "string"},
				},
			},
		},
		{
			"name":        "xissue_delete_test_plan",
			"description": "Delete a test plan by id.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"planId"},
				"properties": map[string]any{
					"planId": map[string]any{"type": "number", "description": "Test plan id"},
				},
			},
		},
		{
			"name":        "xissue_list_project_test_plans",
			"description": "List a project's persistent test plans. Repeated plans of the same kind are numbered x1, x2, … by the server.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"projectId": map[string]any{"type": "number", "description": "Project ID (default: first project)"},
					"q":         map[string]any{"type": "string", "description": "Search title/base title/detail/source key"},
					"status":    map[string]any{"type": "string", "enum": []string{"open", "done"}, "description": "Filter by status (omitted = all)"},
				},
			},
		},
		{
			"name":        "xissue_add_test_plan",
			"description": "Add a test plan for a project. Adding the same kind again renames the earlier one to x1 and stores the new one as x2 (then x3…).",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"title"},
				"properties": map[string]any{
					"projectId":     map[string]any{"type": "number", "description": "Project ID (default: first project)"},
					"title":         map[string]any{"type": "string", "description": "Test plan title, e.g. 回归登录"},
					"detail":        map[string]any{"type": "string", "description": "Optional steps / expected result"},
					"sourceIssueId": map[string]any{"type": "number", "description": "Issue this plan covers (optional)"},
					"sourceKey":     map[string]any{"type": "string", "description": "Issue key snapshot (optional)"},
				},
			},
		},
		{
			"name":        "xissue_get_run_changes",
			"description": "Get a run's changed files grouped per repository, including each worktreePath and the unified patch so the caller can run verification commands and feed an independent reviewer.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"runId"},
				"properties": map[string]any{
					"runId":     map[string]any{"type": "number", "description": "Run ID"},
					"withPatch": map[string]any{"type": "boolean", "description": "Include the unified diff (default: true)"},
				},
			},
		},
		{
			"name":        "xissue_send_message",
			"description": "Send a follow-up message to a run, resuming its warm agent session so the agent can keep editing the worktree.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"runId", "text"},
				"properties": map[string]any{
					"runId": map[string]any{"type": "number", "description": "Run ID"},
					"text":  map[string]any{"type": "string", "description": "Message / instructions for the agent"},
				},
			},
		},
		{
			"name":        "xissue_submit_run",
			"description": "Submit a run: pr (open a pull request) or push (push the branch only). merge_local is intentionally unavailable over MCP so unattended agents cannot merge into the target branch; use the web UI for that. The run's worktree is removed afterwards.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"runId", "mode"},
				"properties": map[string]any{
					"runId":         map[string]any{"type": "number", "description": "Run ID"},
					"mode":          map[string]any{"type": "string", "enum": []string{"pr", "push"}, "description": "Submit mode (merge_local is only available from the web UI)"},
					"targetBranch":  map[string]any{"type": "string", "description": "Target branch (default: project/repo default branch)"},
					"commitMessage": map[string]any{"type": "string", "description": "Commit message (default: issue title + key)"},
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
				agent = s.runner.AvailableAgentForRole(runner.AgentRoleIssueDevelop)
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
			agent = s.runner.AvailableAgentForRole(runner.AgentRoleIssueDevelop)
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

	case "xissue_list_project_todos", "xissue_list_project_test_plans":
		pid := numArg("projectId")
		if pid <= 0 {
			if projs, _ := s.st.ListProjects(ctx); len(projs) > 0 {
				pid = projs[0].ID
			}
		}
		status := strArg("status")
		if name == "xissue_list_project_todos" {
			items, err := s.st.ListProjectTodos(ctx, pid, strArg("q"), status)
			if err != nil {
				return "error: " + err.Error(), true
			}
			b, _ := json.MarshalIndent(items, "", "  ")
			return string(b), false
		}
		items, err := s.st.ListProjectTestPlans(ctx, pid, strArg("q"), status)
		if err != nil {
			return "error: " + err.Error(), true
		}
		b, _ := json.MarshalIndent(items, "", "  ")
		return string(b), false

	case "xissue_add_todo", "xissue_add_test_plan":
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
		var srcID *int64
		if v := numArg("sourceIssueId"); v > 0 {
			srcID = &v
		}
		var item any
		var err error
		if name == "xissue_add_todo" {
			item, err = s.st.CreateProjectTodo(ctx, pid, title, strArg("detail"), srcID, strArg("sourceKey"))
		} else {
			item, err = s.st.CreateProjectTestPlan(ctx, pid, title, strArg("detail"), srcID, strArg("sourceKey"))
		}
		if err != nil {
			return "error: " + err.Error(), true
		}
		b, _ := json.MarshalIndent(item, "", "  ")
		return string(b), false

	case "xissue_add_todos", "xissue_add_test_plans":
		pid := numArg("projectId")
		if pid <= 0 {
			if projs, _ := s.st.ListProjects(ctx); len(projs) > 0 {
				pid = projs[0].ID
			} else {
				return "error: no project found in xIssue", true
			}
		}
		rawItems, ok := args["items"].([]any)
		if !ok || len(rawItems) == 0 {
			return "error: items is required", true
		}
		created := make([]any, 0, len(rawItems))
		createdIDs := make([]int64, 0, len(rawItems))
		for i, raw := range rawItems {
			m, ok := raw.(map[string]any)
			if !ok {
				return fmt.Sprintf("error: item %d is not an object", i), true
			}
			itemTitle := ""
			if v, ok := m["title"].(string); ok {
				itemTitle = strings.TrimSpace(v)
			}
			detail := ""
			if v, ok := m["detail"].(string); ok {
				detail = strings.TrimSpace(v)
			}
			sourceKey := ""
			if v, ok := m["sourceKey"].(string); ok {
				sourceKey = strings.TrimSpace(v)
			}
			var srcID *int64
			if v, ok := m["sourceIssueId"].(float64); ok && int64(v) > 0 {
				id := int64(v)
				srcID = &id
			}
			if itemTitle == "" {
				return fmt.Sprintf("error: item %d: title is required", i), true
			}
			var item any
			var err error
			if name == "xissue_add_todos" {
				item, err = s.st.CreateProjectTodo(ctx, pid, itemTitle, detail, srcID, sourceKey)
			} else {
				item, err = s.st.CreateProjectTestPlan(ctx, pid, itemTitle, detail, srcID, sourceKey)
			}
			if err != nil {
				return fmt.Sprintf("error: item %d (%s): %v (created %d before it)", i, itemTitle, err, len(created)), true
			}
			created = append(created, item)
			switch v := item.(type) {
			case *domain.ProjectTodo:
				createdIDs = append(createdIDs, v.ID)
			case *domain.ProjectTestPlan:
				createdIDs = append(createdIDs, v.ID)
			}
		}
		// Re-read so bulk test plans reflect renumbering done by later inserts.
		created = created[:0]
		for _, id := range createdIDs {
			if name == "xissue_add_todos" {
				item, err := s.st.GetProjectTodoByID(ctx, id)
				if err != nil {
					return "error: " + err.Error(), true
				}
				created = append(created, item)
			} else {
				item, err := s.st.GetProjectTestPlanByID(ctx, id)
				if err != nil {
					return "error: " + err.Error(), true
				}
				created = append(created, item)
			}
		}
		b, _ := json.MarshalIndent(created, "", "  ")
		return string(b), false

	case "xissue_update_todo":
		tid := numArg("todoId")
		if tid <= 0 {
			return "error: todoId is required", true
		}
		patch := store.ProjectTodoPatch{}
		if v, ok := args["status"].(string); ok && strings.TrimSpace(v) != "" {
			patch.Status = &v
		}
		if v, ok := args["title"].(string); ok {
			patch.Title = &v
		}
		if v, ok := args["detail"].(string); ok {
			patch.Detail = &v
		}
		item, err := s.st.UpdateProjectTodoByID(ctx, tid, patch)
		if err != nil {
			return "error: " + err.Error(), true
		}
		b, _ := json.MarshalIndent(item, "", "  ")
		return string(b), false

	case "xissue_delete_todo":
		tid := numArg("todoId")
		if tid <= 0 {
			return "error: todoId is required", true
		}
		if err := s.st.DeleteProjectTodoByID(ctx, tid); err != nil {
			return "error: " + err.Error(), true
		}
		return `{"ok": true}`, false

	case "xissue_update_test_plan":
		planID := numArg("planId")
		if planID <= 0 {
			return "error: planId is required", true
		}
		patch := store.ProjectTestPlanPatch{}
		if v, ok := args["status"].(string); ok && strings.TrimSpace(v) != "" {
			patch.Status = &v
		}
		if v, ok := args["detail"].(string); ok {
			patch.Detail = &v
		}
		item, err := s.st.UpdateProjectTestPlanByID(ctx, planID, patch)
		if err != nil {
			return "error: " + err.Error(), true
		}
		b, _ := json.MarshalIndent(item, "", "  ")
		return string(b), false

	case "xissue_delete_test_plan":
		planID := numArg("planId")
		if planID <= 0 {
			return "error: planId is required", true
		}
		if err := s.st.DeleteProjectTestPlanByID(ctx, planID); err != nil {
			return "error: " + err.Error(), true
		}
		return `{"ok": true}`, false

	case "xissue_get_run_changes":
		rid := numArg("runId")
		if rid <= 0 {
			return "error: runId is required", true
		}
		withPatch := true
		if _, ok := args["withPatch"].(bool); ok {
			withPatch = boolArg("withPatch")
		}
		changes, err := s.runner.Changes(ctx, rid, withPatch)
		if err != nil {
			return "error: " + err.Error(), true
		}
		b, _ := json.MarshalIndent(changes, "", "  ")
		return string(b), false

	case "xissue_send_message":
		rid := numArg("runId")
		text := strArg("text")
		if rid <= 0 || text == "" {
			return "error: runId and text are required", true
		}
		if err := s.runner.SendMessage(ctx, rid, text); err != nil {
			return "error: " + err.Error(), true
		}
		b, _ := json.MarshalIndent(map[string]any{"ok": true, "runId": rid}, "", "  ")
		return string(b), false

	case "xissue_submit_run":
		rid := numArg("runId")
		mode := strArg("mode")
		if rid <= 0 {
			return "error: runId is required", true
		}
		switch mode {
		case "pr", "push":
		default:
			return "error: mode must be one of pr, push (merge_local is only available from the web UI)", true
		}
		if run, gerr := s.st.GetRun(ctx, rid); gerr != nil {
			return "error: " + gerr.Error(), true
		} else if run.AutoPR {
			// Auto-develop runs are submitted by the harness itself after a
			// successful turn; an agent must not be able to submit them early.
			return "error: this run is managed by auto-develop and cannot be submitted manually", true
		}
		results, err := s.runner.Submit(ctx, rid, runner.SubmitRequest{
			Mode:          mode,
			TargetBranch:  strArg("targetBranch"),
			CommitMessage: strArg("commitMessage"),
		})
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
	Transport  string `json:"transport,omitempty"` // "stdio" or "http"
}

func xissueBinaryPath() string {
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe
	}
	return "xissue"
}

// MCPHTTPURL returns the local Streamable HTTP endpoint advertised to clients.
func MCPHTTPURL(listen string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		host, port = strings.TrimSpace(listen), "7789"
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/api/mcp"
}

func (s *Server) mcpHeaders() map[string]string {
	if strings.TrimSpace(s.cfg.Token) != "" {
		return map[string]string{"Authorization": "Bearer " + strings.TrimSpace(s.cfg.Token)}
	}
	return map[string]string{"X-XIssue": "1"}
}

func (s *Server) handleMCPStatus(w http.ResponseWriter, r *http.Request) {
	bin := xissueBinaryPath()
	writeJSON(w, http.StatusOK, map[string]any{
		"binaryPath": bin,
		"httpUrl":    MCPHTTPURL(s.cfg.Listen),
		"targets":    inspectMCPTargets(bin),
	})
}

func (s *Server) handleMCPInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Targets   []string `json:"targets"`   // optional; if empty, installs to all detected/supported JSON targets
		Transport string   `json:"transport"` // "stdio" (default) or "http"
	}
	_ = readJSON(r, &body)
	bin := xissueBinaryPath()
	transport := strings.ToLower(strings.TrimSpace(body.Transport))
	if transport == "" {
		transport = "stdio"
	}
	installed, err := InstallMCPTargets(bin, body.Targets, transport, MCPHTTPURL(s.cfg.Listen), s.mcpHeaders())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installed":  installed,
		"binaryPath": bin,
		"httpUrl":    MCPHTTPURL(s.cfg.Listen),
		"transport":  transport,
		"targets":    inspectMCPTargets(bin),
	})
}

func detectQoderConfig(home string) (configPath string, name string, detected bool) {
	// 1. 优先检查国际版 ~/.qoder 目录或 qoder/qodercli 命令
	intlDir := filepath.Join(home, ".qoder")
	intlConfig := filepath.Join(intlDir, "mcp.json")
	if st, err := os.Stat(intlDir); err == nil && st.IsDir() {
		return intlConfig, "Qoder (~/.qoder/mcp.json)", true
	}
	if _, err := exec.LookPath("qoder"); err == nil {
		return intlConfig, "Qoder (~/.qoder/mcp.json)", true
	}
	if _, err := exec.LookPath("qodercli"); err == nil {
		return intlConfig, "Qoder (~/.qoder/mcp.json)", true
	}

	// 2. 检查国内版 ~/.qoder-cn 目录或 qoderclicn 命令
	cnDir := filepath.Join(home, ".qoder-cn")
	cnConfig := filepath.Join(cnDir, "mcp.json")
	if st, err := os.Stat(cnDir); err == nil && st.IsDir() {
		return cnConfig, "Qoder-CN (~/.qoder-cn/mcp.json)", true
	}
	if _, err := exec.LookPath("qoderclicn"); err == nil {
		return cnConfig, "Qoder-CN (~/.qoder-cn/mcp.json)", true
	}

	// 3. 机器未安装时，默认标为国际版标准路径 ~/.qoder/mcp.json，detected = false
	return intlConfig, "Qoder (~/.qoder/mcp.json)", false
}

func inspectMCPTargets(bin string) []MCPTargetStatus {
	home, _ := os.UserHomeDir()
	targets := []struct {
		id      string
		name    string
		relPath string
		topKey  string
		binName string
	}{
		{"opencode", "OpenCode (~/.config/opencode/opencode.json)", ".config/opencode/opencode.json", "mcp", "opencode"},
		{"claude", "Claude Code (~/.claude.json)", ".claude.json", "mcpServers", "claude"},
		{"cursor", "Cursor IDE (~/.cursor/mcp.json)", ".cursor/mcp.json", "mcpServers", "cursor"},
		{"windsurf", "Windsurf IDE (~/.codeium/windsurf/mcp_config.json)", ".codeium/windsurf/mcp_config.json", "mcpServers", "windsurf"},
	}

	var out []MCPTargetStatus
	for _, t := range targets {
		p := filepath.Join(home, t.relPath)
		_, statErr := os.Stat(p)
		_, dirErr := os.Stat(filepath.Dir(p))
		_, binErr := exec.LookPath(t.binName)
		detected := statErr == nil || dirErr == nil || binErr == nil
		installed, transport := detectMCPEntry(p, t.topKey)
		out = append(out, MCPTargetStatus{
			ID:         t.id,
			Name:       t.name,
			ConfigPath: p,
			Detected:   detected,
			Installed:  installed,
			Transport:  transport,
		})
	}

	// Qoder 动态探测（国际版 ~/.qoder vs 国内版 ~/.qoder-cn）
	qoderPath, qoderName, qoderDetected := detectQoderConfig(home)
	qoderInstalled, qoderTransport := detectMCPEntry(qoderPath, "mcpServers")
	out = append(out, MCPTargetStatus{
		ID:         "qoder",
		Name:       qoderName,
		ConfigPath: qoderPath,
		Detected:   qoderDetected,
		Installed:  qoderInstalled,
		Transport:  qoderTransport,
	})

	return out
}

// detectMCPEntry reports whether the xissue entry exists and which transport it uses.
func detectMCPEntry(path, topKey string) (installed bool, transport string) {
	b, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return false, ""
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(b, &root) != nil {
		// Tolerate comments/trailing commas in hand-edited configs for status display.
		return strings.Contains(string(b), `"xissue"`), ""
	}
	var entries map[string]json.RawMessage
	if json.Unmarshal(root[topKey], &entries) != nil {
		return false, ""
	}
	raw, ok := entries["xissue"]
	if !ok {
		return false, ""
	}
	var entry map[string]json.RawMessage
	if json.Unmarshal(raw, &entry) != nil {
		return true, ""
	}
	if _, ok := entry["url"]; ok {
		return true, "http"
	}
	if _, ok := entry["serverUrl"]; ok {
		return true, "http"
	}
	if _, ok := entry["command"]; ok {
		return true, "stdio"
	}
	return true, ""
}

func withMCPHeaders(entry map[string]any, headers map[string]string) map[string]any {
	if len(headers) > 0 {
		entry["headers"] = headers
	}
	return entry
}

// InstallMCPTargets injects the xissue MCP server into the specified (or all)
// local IDE/Agent config files. transport is "stdio" (default) or "http";
// httpURL is used only for HTTP-capable clients.
func InstallMCPTargets(bin string, targetIDs []string, transport, httpURL string, headers map[string]string) ([]string, error) {
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
	if strings.ToLower(strings.TrimSpace(transport)) == "http" {
		return installMCPHTTPTargets(home, should, httpURL, headers)
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

	// 3. Qoder CLI: 动态优先注入已探测到的版本（国际版 ~/.qoder/mcp.json 或国内版 ~/.qoder-cn/mcp.json）
	if should("qoder") {
		qoderPath, _, detected := detectQoderConfig(home)
		if detected || len(want) > 0 {
			if err := injectJSONMap(qoderPath, "mcpServers", "xissue", map[string]any{
				"command": bin,
				"args":    []string{"mcp"},
			}); err == nil {
				installed = append(installed, "qoder")
			}
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

// installMCPHTTPTargets rewrites supported clients to use the shared
// Streamable HTTP endpoint instead of spawning `xissue mcp` per session.
func installMCPHTTPTargets(home string, should func(string) bool, httpURL string, headers map[string]string) ([]string, error) {
	if strings.TrimSpace(httpURL) == "" {
		return nil, fmt.Errorf("empty MCP HTTP URL")
	}
	var installed []string

	if should("opencode") {
		p := filepath.Join(home, ".config/opencode/opencode.json")
		entry := withMCPHeaders(map[string]any{
			"type":    "remote",
			"url":     httpURL,
			"enabled": true,
		}, headers)
		if err := injectJSONMap(p, "mcp", "xissue", entry); err == nil {
			installed = append(installed, "opencode")
		}
	}
	if should("claude") {
		p := filepath.Join(home, ".claude.json")
		entry := withMCPHeaders(map[string]any{
			"type": "http",
			"url":  httpURL,
		}, headers)
		if err := injectJSONMap(p, "mcpServers", "xissue", entry); err == nil {
			installed = append(installed, "claude")
		}
	}
	if should("cursor") {
		p := filepath.Join(home, ".cursor/mcp.json")
		entry := withMCPHeaders(map[string]any{
			"url": httpURL,
		}, headers)
		if err := injectJSONMap(p, "mcpServers", "xissue", entry); err == nil {
			installed = append(installed, "cursor")
		}
	}
	if should("windsurf") {
		p := filepath.Join(home, ".codeium/windsurf/mcp_config.json")
		entry := withMCPHeaders(map[string]any{
			"serverUrl": httpURL,
		}, headers)
		if err := injectJSONMap(p, "mcpServers", "xissue", entry); err == nil {
			installed = append(installed, "windsurf")
		}
	}
	// Qoder's remote schema is not stable across releases; keep stdio for it.
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
