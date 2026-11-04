package main

import (
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/server"
)

func TestMCPToolDefinitionsAreComplete(t *testing.T) {
	defs := server.MCPToolDefinitions()
	if len(defs) < 10 {
		t.Fatalf("expected the full tool set, got %d", len(defs))
	}
	seen := map[string]bool{}
	for _, def := range defs {
		name, _ := def["name"].(string)
		desc, _ := def["description"].(string)
		schema, ok := def["inputSchema"].(map[string]any)
		if name == "" || desc == "" || !ok || schema["type"] != "object" {
			t.Fatalf("incomplete tool definition: %+v", def)
		}
		if seen[name] {
			t.Fatalf("duplicate tool name %q", name)
		}
		seen[name] = true
	}
	for _, want := range []string{"xissue_list_project_todos", "xissue_add_test_plans", "xissue_submit_run"} {
		if !seen[want] {
			t.Fatalf("missing tool %q", want)
		}
	}
}

func TestDescribeMCPArgs(t *testing.T) {
	def := map[string]any{
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []any{"title"},
			"properties": map[string]any{
				"title":  map[string]any{"type": "string"},
				"status": map[string]any{"type": "string", "enum": []string{"open", "done"}},
			},
		},
	}
	got := describeMCPArgs(def)
	if !strings.Contains(got, "status (string: open|done)") || !strings.Contains(got, "title (string, required)") {
		t.Fatalf("describeMCPArgs = %q", got)
	}
	if describeMCPArgs(map[string]any{"inputSchema": map[string]any{"properties": map[string]any{}}}) != "" {
		t.Fatal("empty schema should render no args")
	}
}
