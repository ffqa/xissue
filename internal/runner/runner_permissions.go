package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/xpier/xissue/internal/agent/acp"
	"github.com/xpier/xissue/internal/domain"
)

func (r *Runner) handlePermission(ctx context.Context, act *activeRun, req acp.PermissionRequest) string {
	title, _ := req.ToolCall["title"].(string)
	kind, _ := req.ToolCall["kind"].(string)
	if title == "" {
		title = "Tool execution permission"
	}
	reqID := fmt.Sprintf("perm-%d", act.reqSeq.Add(1))

	// Check policy for auto-approval.
	if opt := r.autoApproveOption(act.runID, kind, title, req.ToolCall, req.Options); opt != "" {
		r.emitEvent(act.runID, act.issueID, "permission_request", map[string]any{
			"requestId": reqID, "title": title, "toolCall": req.ToolCall, "options": req.Options,
		})
		r.emitEvent(act.runID, act.issueID, "permission_resolved", map[string]any{
			"requestId": reqID, "optionId": opt, "auto": true,
		})
		return opt
	}

	// Auto pre-development runs must never occupy a concurrency slot waiting for
	// an administrator. Deny non-allowlisted tools and let the agent wrap up.
	if run, rerr := r.st.GetRun(ctx, act.runID); rerr == nil && run.AutoPR {
		chosen := ""
		for _, o := range req.Options {
			if strings.Contains(strings.ToLower(o.Kind), "reject") || strings.Contains(strings.ToLower(o.OptionID), "reject") {
				chosen = o.OptionID
				break
			}
		}
		r.LogMonitor("WARN", "automation", fmt.Sprintf("自动预开发 Run #%d 请求权限 %q，已自动拒绝以避免占用槽位", act.runID, title))
		r.emitEvent(act.runID, act.issueID, "permission_request", map[string]any{
			"requestId": reqID, "title": title, "toolCall": req.ToolCall, "options": req.Options,
		})
		r.emitEvent(act.runID, act.issueID, "permission_resolved", map[string]any{
			"requestId": reqID, "optionId": chosen, "auto": true, "denied": true,
		})
		return chosen
	}

	ch := make(chan string, 1)
	act.permMu.Lock()
	act.perms[reqID] = ch
	act.permMu.Unlock()
	defer func() {
		act.permMu.Lock()
		delete(act.perms, reqID)
		act.permMu.Unlock()
	}()

	r.emitEvent(act.runID, act.issueID, "permission_request", map[string]any{
		"requestId": reqID, "title": title, "toolCall": req.ToolCall, "options": req.Options,
	})
	r.setRunStatus(act.runID, act.issueID, domain.RunAwaiting, false, false)
	r.pushNotification("permission", fmt.Sprintf("Run #%d 等待授权审批", act.runID), title, fmt.Sprintf("/issues/%d", act.issueID), "")

	var chosen string
	select {
	case <-ctx.Done():
		r.emitEvent(act.runID, act.issueID, "permission_resolved", map[string]any{
			"requestId": reqID, "optionId": "cancelled", "auto": true,
		})
		return ""
	case chosen = <-ch:
	}

	for _, o := range req.Options {
		if o.OptionID == chosen && o.Kind == "allow_always" {
			r.mu.Lock()
			if r.allowSet[act.runID] == nil {
				r.allowSet[act.runID] = map[string]bool{}
			}
			reqKind := ""
			if req.ToolCall != nil {
				if k, ok := req.ToolCall["kind"].(string); ok {
					reqKind = strings.ToLower(k)
				}
			}
			if reqKind == "" {
				reqKind = "read"
			}
			r.allowSet[act.runID][reqKind] = true
			r.mu.Unlock()
		}
	}
	r.emitEvent(act.runID, act.issueID, "permission_resolved", map[string]any{
		"requestId": reqID, "optionId": chosen, "auto": false,
	})
	r.setRunStatus(act.runID, act.issueID, domain.RunRunning, false, false)
	return chosen
}

func (r *Runner) autoApproveOption(runID int64, kind, title string, tc map[string]any, opts []acp.PermissionOption) string {
	rawText := fmt.Sprintf("%s %v", title, tc["rawInput"])
	perm := r.cfg.PermissionsSnapshot()
	alwaysAsk := perm.AlwaysAskPatterns
	autoAllow := perm.AutoAllowKinds
	allowedKinds := map[string]bool{}
	r.mu.Lock()
	for k, v := range r.allowSet[runID] {
		allowedKinds[k] = v
	}
	r.mu.Unlock()

	for _, pat := range alwaysAsk {
		if pat != "" && strings.Contains(rawText, pat) {
			return ""
		}
	}

	allowedKind := false
	if allowedKinds != nil && (allowedKinds[strings.ToLower(kind)] || allowedKinds["*"]) {
		allowedKind = true
	}
	if !allowedKind {
		for _, k := range autoAllow {
			if strings.EqualFold(k, kind) {
				allowedKind = true
				break
			}
		}
	}
	if !allowedKind {
		return ""
	}
	for _, o := range opts {
		if o.Kind == "allow_once" || o.Kind == "allow_always" {
			return o.OptionID
		}
	}
	return ""
}
