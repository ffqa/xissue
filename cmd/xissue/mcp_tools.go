package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// printMCPToolList renders the MCP tool schemas in a readable table. The CLI
// compiles the same definitions the server serves, so it works without a
// running instance.
func printMCPToolList(defs []map[string]any, asJSON bool) {
	if asJSON {
		b, _ := json.MarshalIndent(defs, "", "  ")
		fmt.Println(string(b))
		return
	}
	fmt.Printf("xIssue MCP tools (%d):\n\n", len(defs))
	for _, def := range defs {
		name, _ := def["name"].(string)
		desc, _ := def["description"].(string)
		fmt.Printf("  • %s\n", name)
		if desc != "" {
			fmt.Printf("      %s\n", desc)
		}
		if args := describeMCPArgs(def); args != "" {
			fmt.Printf("      args: %s\n", args)
		}
	}
	fmt.Println()
	fmt.Println("调用方式:")
	fmt.Println("  - stdio: 在 MCP 客户端配置 {\"command\":\"xissue\",\"args\":[\"mcp\"]}")
	fmt.Println("  - HTTP:  POST <server>/api/mcp  (xissue mcp http 可查看地址)")
	fmt.Println("  - 请求:  {\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"xissue_list_projects\",\"arguments\":{}}}")
	fmt.Println("  - 原始 schema: xissue mcp list --json")
}

func describeMCPArgs(def map[string]any) string {
	schema, _ := def["inputSchema"].(map[string]any)
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return ""
	}
	required := map[string]bool{}
	switch list := schema["required"].(type) {
	case []any:
		for _, v := range list {
			if s, ok := v.(string); ok {
				required[s] = true
			}
		}
	case []string:
		for _, s := range list {
			required[s] = true
		}
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		spec, _ := props[name].(map[string]any)
		typ, _ := spec["type"].(string)
		label := name
		if typ != "" {
			label += " (" + typ
		} else {
			label += " ("
		}
		if required[name] {
			label += ", required"
		}
		if enum, ok := spec["enum"].([]string); ok && len(enum) > 0 {
			label += ": " + strings.Join(enum, "|")
		}
		label += ")"
		parts = append(parts, label)
	}
	return strings.Join(parts, ", ")
}
