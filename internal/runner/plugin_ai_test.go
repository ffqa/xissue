package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/config"
)

func TestPluginAIUsesDefaultAgent(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-agent")
	body := `#!/usr/bin/env python3
import json, sys
for line in sys.stdin:
    msg = json.loads(line)
    if "id" not in msg:
        continue
    method = msg.get("method")
    if method == "initialize":
        result = {"protocolVersion": 1, "agentCapabilities": {}, "agentInfo": {}}
    elif method == "session/new":
        result = {"sessionId": "s1", "configOptions": []}
    elif method == "session/prompt":
        print(json.dumps({"jsonrpc": "2.0", "method": "session/update", "params": {"sessionId": "s1", "update": {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "hello from fake agent"}}}}), flush=True)
        result = {"stopReason": "end_turn", "usage": {"totalTokens": 3}}
    else:
        result = {}
    print(json.dumps({"jsonrpc": "2.0", "id": msg["id"], "result": result}), flush=True)
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.DefaultAgent = "fake"
	cfg.Agents = map[string]config.AgentConfig{"fake": {Command: script}}
	r := &Runner{cfg: cfg}

	info := r.PluginAIDefault()
	if !info.Available || info.Agent != "fake" {
		t.Fatalf("PluginAIDefault = %+v", info)
	}
	res, err := r.PluginAI(context.Background(), "say hi", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Agent != "fake" || res.Text != "hello from fake agent" || res.StopReason != "end_turn" {
		t.Fatalf("PluginAI = %+v", res)
	}
	if res.Usage == nil || res.Usage.TotalTokens != 3 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if _, err := r.PluginAI(context.Background(), "   ", "", ""); err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("empty prompt error = %v", err)
	}
}
