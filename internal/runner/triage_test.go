package runner

import (
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func domainIssue(title, body string, labels []string) domain.Issue {
	return domain.Issue{Title: title, Body: body, Labels: labels}
}

func TestClassifyIssue(t *testing.T) {
	cases := []struct {
		name       string
		title      string
		body       string
		labels     []string
		want       IssueKind
		actionable bool
	}{
		{"explicit bug label", "something", "", []string{"bug"}, IssueKindBug, true},
		{"explicit feature label", "something", "", []string{"enhancement"}, IssueKindFeature, true},
		{"explicit suggestion label", "something", "", []string{"suggestion"}, IssueKindSuggestion, false},
		{"bug keyword wins over suggestion", "App crashes when opening settings", "建议修复", nil, IssueKindBug, true},
		{"feature keyword", "请增加导出功能", "", nil, IssueKindFeature, true},
		{"feature phrase before bug keyword", "Add support for error handling", "", nil, IssueKindFeature, true},
		{"suggestion keyword", "建议增加深色模式", "", nil, IssueKindSuggestion, false},
		{"unknown", "hello", "", nil, IssueKindUnknown, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyIssue(domainIssue(tc.title, tc.body, tc.labels))
			if got.Kind != tc.want {
				t.Fatalf("kind=%s want %s (result=%+v)", got.Kind, tc.want, got)
			}
			if got.Actionable != tc.actionable {
				t.Fatalf("actionable=%v want %v", got.Actionable, tc.actionable)
			}
		})
	}
}
