package runner

import (
	"strings"

	"github.com/xpier/xissue/internal/domain"
)

// IssueKind is the actionable classification used by automatic issue triage.
type IssueKind string

const (
	IssueKindBug        IssueKind = "bug"
	IssueKindFeature    IssueKind = "feature"
	IssueKindSuggestion IssueKind = "suggestion"
	IssueKindUnknown    IssueKind = "unknown"
)

// TriageResult describes why a synced issue was classified a certain way.
type TriageResult struct {
	Kind       IssueKind `json:"kind"`
	Label      string    `json:"label"`
	Reason     string    `json:"reason"`
	Matched    []string  `json:"matched,omitempty"`
	Actionable bool      `json:"actionable"`
}

var triageLabelOrder = map[IssueKind]int{
	IssueKindBug:        0,
	IssueKindFeature:    1,
	IssueKindSuggestion: 2,
}

var bugLabels = []string{"bug", "fix", "error", "crash", "regression", "defect", "缺陷", "报错", "崩溃", "错误"}
var featureLabels = []string{"enhancement", "feature", "request", "需求", "功能", "新功能"}
var suggestionLabels = []string{"suggestion", "idea", "proposal", "讨论", "建议"}

var bugKeywords = []string{
	"bug", "error", "errors", "crash", "crashes", "crashing", "broken", "failure", "failures", "failed", "exception", "exceptions", "regression",
	"报错", "崩溃", "异常", "失败", "无法", "不工作", "不生效", "错误", "修复",
}
var featureKeywords = []string{
	"feature", "enhancement", "support", "add support", "improve",
	"需求", "功能", "支持", "增加", "新增", "希望", "优化", "改进",
}
var suggestionKeywords = []string{
	"suggestion", "proposal", "idea", "建议", "提议", "讨论", "咨询",
}

func normalizeLabels(labels []string) map[string]bool {
	out := make(map[string]bool, len(labels))
	for _, label := range labels {
		out[strings.ToLower(strings.TrimSpace(label))] = true
	}
	return out
}

func firstLabelMatch(labels map[string]bool, candidates []string) string {
	for _, candidate := range candidates {
		if labels[strings.ToLower(candidate)] {
			return candidate
		}
	}
	return ""
}

func firstKeywordMatch(text string, candidates []string) string {
	bestTerm, bestPos := "", -1
	for _, candidate := range candidates {
		pos := keywordPosition(text, candidate)
		if pos >= 0 && (bestPos < 0 || pos < bestPos) {
			bestTerm, bestPos = candidate, pos
		}
	}
	return bestTerm
}

func keywordPosition(text, keyword string) int {
	text = strings.ToLower(text)
	keyword = strings.ToLower(keyword)
	if keyword == "" {
		return -1
	}
	if !isASCII(keyword) {
		return strings.Index(text, keyword)
	}
	for offset := 0; ; {
		idx := strings.Index(text[offset:], keyword)
		if idx < 0 {
			return -1
		}
		start := offset + idx
		end := start + len(keyword)
		if (start == 0 || !isWordByte(text[start-1])) && (end == len(text) || !isWordByte(text[end])) {
			return start
		}
		offset = start + 1
	}
}

// ClassifyIssue uses explicit labels first, then conservative title/body
// keywords. Suggestions are intentionally non-actionable: the harness should
// wait for the author or a maintainer instead of spending an agent run.
func ClassifyIssue(issue domain.Issue) TriageResult {
	labels := normalizeLabels(issue.Labels)
	text := strings.TrimSpace(issue.Title + "\n" + issue.Body)

	type match struct {
		kind  IssueKind
		label string
		term  string
		src   string
	}
	var matches []match

	if label := firstLabelMatch(labels, bugLabels); label != "" {
		matches = append(matches, match{IssueKindBug, label, label, "label"})
	}
	if label := firstLabelMatch(labels, featureLabels); label != "" {
		matches = append(matches, match{IssueKindFeature, label, label, "label"})
	}
	if label := firstLabelMatch(labels, suggestionLabels); label != "" {
		matches = append(matches, match{IssueKindSuggestion, label, label, "label"})
	}

	// Explicit labels are authoritative; on conflicts prefer bug > feature >
	// suggestion.
	if len(matches) > 0 {
		best := matches[0]
		for _, m := range matches[1:] {
			if triageLabelOrder[m.kind] < triageLabelOrder[best.kind] {
				best = m
			}
		}
		return makeTriageResult(best.kind, canonicalTriageLabel(best.kind), "label:"+best.term)
	}

	// For free text, use the earliest strong signal. This keeps
	// "Add support for error handling" a feature while "Error when adding
	// support" stays a bug, and "建议增加..." a suggestion.
	type kwMatch struct {
		kind IssueKind
		term string
		pos  int
	}
	var kw []kwMatch
	add := func(kind IssueKind, terms []string) {
		if term := firstKeywordMatch(text, terms); term != "" {
			kw = append(kw, kwMatch{kind: kind, term: term, pos: keywordPosition(text, term)})
		}
	}
	add(IssueKindBug, bugKeywords)
	add(IssueKindSuggestion, suggestionKeywords)
	add(IssueKindFeature, featureKeywords)
	if len(kw) == 0 {
		return TriageResult{Kind: IssueKindUnknown, Reason: "no bug/feature/suggestion signal"}
	}
	best := kw[0]
	for _, m := range kw[1:] {
		if m.pos < best.pos || (m.pos == best.pos && triageLabelOrder[m.kind] < triageLabelOrder[best.kind]) {
			best = m
		}
	}
	return makeTriageResult(best.kind, canonicalTriageLabel(best.kind), "keyword:"+best.term)
}

func canonicalTriageLabel(kind IssueKind) string {
	switch kind {
	case IssueKindBug:
		return "bug"
	case IssueKindFeature:
		return "enhancement"
	case IssueKindSuggestion:
		return "suggestion"
	default:
		return ""
	}
}

func makeTriageResult(kind IssueKind, label, reason string) TriageResult {
	term := reason
	if idx := strings.IndexByte(reason, ':'); idx >= 0 {
		term = reason[idx+1:]
	}
	return TriageResult{
		Kind:       kind,
		Label:      label,
		Reason:     reason,
		Matched:    []string{term},
		Actionable: kind == IssueKindBug || kind == IssueKindFeature,
	}
}
