package gitx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetCommitDetail(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	if _, err := Run(ctx, dir, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "first commit", "-m", "body line"); err != nil {
		t.Fatal(err)
	}
	first, err := Run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	firstDetail, err := GetCommitDetail(ctx, dir, first)
	if err != nil {
		t.Fatalf("first detail: %v", err)
	}
	if firstDetail.Subject != "first commit" || len(firstDetail.Files) != 1 || firstDetail.Files[0].Status != "A" {
		t.Fatalf("unexpected first detail: %+v", firstDetail)
	}
	if !strings.Contains(firstDetail.Patch, "+one") {
		t.Fatalf("patch missing content: %q", firstDetail.Patch)
	}

	if _, err := Run(ctx, dir, "mv", "a.txt", "b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "rename and edit"); err != nil {
		t.Fatal(err)
	}
	second, err := Run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := GetCommitDetail(ctx, dir, second[:10])
	if err != nil {
		t.Fatalf("second detail: %v", err)
	}
	if detail.SHA != second || detail.Subject != "rename and edit" || len(detail.Parents) != 1 {
		t.Fatalf("unexpected metadata: %+v", detail)
	}
	if len(detail.Files) != 1 || detail.Files[0].Path != "b.txt" || !strings.HasPrefix(detail.Files[0].Status, "R") {
		t.Fatalf("unexpected files: %+v", detail.Files)
	}
	if !strings.Contains(detail.Patch, "+two") {
		t.Fatalf("patch missing edit: %q", detail.Patch)
	}
}
