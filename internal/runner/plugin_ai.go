package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xpier/xissue/internal/agent/acp"
	"github.com/xpier/xissue/internal/config"
)

// PluginAIInfo describes the default agent a plugin can use for AI calls.
type PluginAIInfo struct {
	Agent     string   `json:"agent"`
	Model     string   `json:"model,omitempty"`
	Models    []string `json:"models,omitempty"`
	Available bool     `json:"available"`
}

// PluginAIResult is the result of a one-shot plugin AI prompt.
type PluginAIResult struct {
	Agent      string     `json:"agent"`
	Model      string     `json:"model,omitempty"`
	Text       string     `json:"text"`
	StopReason string     `json:"stopReason,omitempty"`
	Usage      *acp.Usage `json:"usage,omitempty"`
}

// pluginAgent picks the configured default agent, preferring one whose binary is
// actually installed. An explicit agentName is still validated.
func (r *Runner) pluginAgent(agentName string) (string, config.AgentConfig, bool, error) {
	statuses := r.ListAgents()
	statusByName := make(map[string]AgentStatus, len(statuses))
	fallback := ""
	fallbackCfg := config.AgentConfig{}
	for _, st := range statuses {
		statusByName[st.Name] = st
		if !st.Available {
			continue
		}
		if fallback == "" {
			fallback = st.Name
			fallbackCfg, _ = r.cfg.Agent(st.Name)
		}
	}

	explicit := strings.TrimSpace(agentName) != ""
	name := strings.TrimSpace(agentName)
	if name == "" {
		name = r.DefaultAgentName()
	}
	if name == "" || (!explicit && fallback != "") {
		if !explicit && name != "" {
			if st, ok := statusByName[name]; ok && st.Available {
				ac, _ := r.cfg.Agent(name)
				return name, ac, true, nil
			}
		}
		if fallback == "" {
			return "", config.AgentConfig{}, false, errors.New("no agent configured")
		}
		return fallback, fallbackCfg, true, nil
	}
	ac, ok := r.cfg.Agent(name)
	if !ok {
		return "", config.AgentConfig{}, false, fmt.Errorf("unknown agent %q", name)
	}
	st, ok := statusByName[name]
	return name, ac, ok && st.Available, nil
}

// PluginAIDefault returns the default agent metadata for plugins.
func (r *Runner) PluginAIDefault() PluginAIInfo {
	name, ac, available, err := r.pluginAgent("")
	if err != nil {
		return PluginAIInfo{Available: false}
	}
	models := append([]string(nil), ac.Models...)
	return PluginAIInfo{Agent: name, Model: ac.DefaultModel, Models: models, Available: available}
}

// PluginAI runs a single prompt through the default ACP agent in a throwaway
// workspace. Tools are rejected: this API is for text generation, not access to
// the user's files or shell.
func (r *Runner) PluginAI(ctx context.Context, prompt, agentName, modelName string) (*PluginAIResult, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, errors.New("prompt is required")
	}
	if len(prompt) > 32*1024 {
		return nil, errors.New("prompt is too long (max 32KB)")
	}
	name, ac, available, err := r.pluginAgent(agentName)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, fmt.Errorf("agent %q is not available", name)
	}
	binPath, err := LookupAgentBinary(ac.Command)
	if err != nil {
		return nil, fmt.Errorf("agent binary %q not found", ac.Command)
	}
	baseDir := filepath.Join(r.cfg.DataDir, "plugin-ai")
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(baseDir, "request-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	var text strings.Builder
	client, err := acp.Start(ctx, acp.Options{
		Command: binPath,
		Args:    ac.Args,
		Env:     ac.Env,
		Dir:     dir,
	}, acp.Handlers{
		OnEvent: func(ev acp.Event) {
			if ev.Type == "message" {
				if t, ok := ev.Data["text"].(string); ok {
					text.WriteString(t)
				}
			}
		},
		OnPermission: func(_ context.Context, req acp.PermissionRequest) string {
			for _, opt := range req.Options {
				if strings.HasPrefix(opt.Kind, "reject") {
					return opt.OptionID
				}
			}
			return ""
		},
	})
	if err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}
	defer client.Close()
	if err := client.NewSession(ctx, dir); err != nil {
		return nil, fmt.Errorf("agent session: %w", err)
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		modelName = ac.DefaultModel
	}
	if modelName != "" {
		if err := client.SetModel(ctx, modelName); err != nil {
			return nil, fmt.Errorf("set model: %w", err)
		}
	}
	res, err := client.Prompt(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("agent prompt: %w", err)
	}
	return &PluginAIResult{
		Agent:      name,
		Model:      modelName,
		Text:       text.String(),
		StopReason: res.StopReason,
		Usage:      res.Usage,
	}, nil
}
