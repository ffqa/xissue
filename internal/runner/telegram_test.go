package runner

import (
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestFormatTelegramTitle(t *testing.T) {
	got := formatTelegramTitle("⚡ [admin] 分支推送自动化")
	want := "⚡ 【admin】 分支推送自动化"
	if got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
	if strings.Contains(got, `\[`) {
		t.Fatalf("title still contains escaped bracket: %q", got)
	}
}

func TestFormatTelegramBody(t *testing.T) {
	in := "" +
		"**重要**\n" +
		"### 标题 ###\n" +
		"---\n" +
		"- `a_b` 与 [报告](https://example.com/a_b)\n" +
		"[`reports/latest.md`](https://example.com/reports)\n" +
		"```go\n" +
		"fmt.Println(\"*x*\")\n" +
		"```\n" +
		"未匹配 [admin] 与 a_b"
	want := "" +
		"*重要*\n" +
		"*标题*\n" +
		"• `a_b` 与 [报告](https://example.com/a_b)\n" +
		"[reports/latest.md](https://example.com/reports)\n" +
		"```\n" +
		"fmt.Println(\"*x*\")\n" +
		"```\n" +
		"未匹配 【admin】 与 a\\_b"
	got := formatTelegramBody(in)
	if got != want {
		t.Fatalf("body mismatch\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, `\[`) {
		t.Fatalf("body still contains escaped bracket: %q", got)
	}
}

func TestFormatTelegramBodyHandlesEmpty(t *testing.T) {
	if got := formatTelegramBody("  \n"); got != "" {
		t.Fatalf("empty body = %q", got)
	}
}

func TestTelegramPayloadAvoidsEscapedBracketsAndFormatsMarkdown(t *testing.T) {
	payload := telegramPayload("123", "⚡ [admin] 分支推送自动化", "**重要**\n### 标题\n---", "https://example.com")
	if payload["parse_mode"] != "Markdown" {
		t.Fatalf("parse_mode = %v", payload["parse_mode"])
	}
	msg, _ := payload["text"].(string)
	if strings.Contains(msg, `\[`) {
		t.Fatalf("telegram message contains escaped bracket: %q", msg)
	}
	if !strings.Contains(msg, "【admin】") || !strings.Contains(msg, "*重要*") || strings.Contains(msg, "###") || strings.Contains(msg, "---") {
		t.Fatalf("unexpected telegram message: %q", msg)
	}
}

func TestWebhookPayloadDeclaresMarkdown(t *testing.T) {
	payload := webhookPayload("branch_push", "⚡ [admin] 分支推送自动化", "**重要**", "https://example.com")
	if payload["format"] != "markdown" || payload["msg_type"] != "markdown" {
		t.Fatalf("webhook format = %v/%v", payload["format"], payload["msg_type"])
	}
	if payload["event"] != "branch_push" || payload["title"] != "⚡ [admin] 分支推送自动化" {
		t.Fatalf("webhook payload = %#v", payload)
	}
}

func TestFormatTelegramBodyPreservesURLUnderscores(t *testing.T) {
	in := "见 https://example.com/a_b?x=1_2 与 file_name，以及 [链接](https://example.com/a_b)"
	got := formatTelegramBody(in)
	if !strings.Contains(got, "https://example.com/a_b?x=1_2") {
		t.Fatalf("bare URL was escaped: %q", got)
	}
	if !strings.Contains(got, "(https://example.com/a_b)") {
		t.Fatalf("link target was escaped: %q", got)
	}
	if !strings.Contains(got, `file\_name`) {
		t.Fatalf("plain underscore should be escaped: %q", got)
	}
}

func TestWorkspaceAGENTSWarnsAgainstDescriptionPath(t *testing.T) {
	repoID := int64(1)
	p := &domain.Project{Name: "ide", Description: "/Users/example/project", Repos: []domain.Repo{{ID: repoID, Name: "repo"}}}
	issue := &domain.Issue{Key: "acme/repo#1", Title: "Feature", RepoID: &repoID}
	got := buildWorkspaceAGENTS(p, issue)
	if !strings.Contains(got, "not a filesystem path") {
		t.Fatalf("workspace prompt missing path warning: %s", got)
	}
	if !strings.Contains(got, "`./repo/`") {
		t.Fatalf("workspace prompt missing relative repo path: %s", got)
	}
	if strings.Contains(got, "/Users/example/project") {
		t.Fatalf("workspace prompt leaked absolute description path: %s", got)
	}
}
