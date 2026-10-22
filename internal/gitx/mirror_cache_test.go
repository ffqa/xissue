package gitx

import (
	"context"
	"path/filepath"
	"testing"
)

func TestEnsureMirrorCachedSkipsFetch(t *testing.T) {
	src := t.TempDir()
	mirror := filepath.Join(t.TempDir(), "mirror.git")
	ctx := context.Background()
	if _, err := Run(ctx, src, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, src, "-c", "user.name=T", "-c", "user.email=t@e", "commit", "-q", "--allow-empty", "-m", "one"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMirrorCached(ctx, src, mirror); err != nil {
		t.Fatal(err)
	}
	before, err := ListCommits(ctx, mirror, "HEAD", 10)
	if err != nil || len(before) != 1 {
		t.Fatalf("before: %v %+v", err, before)
	}
	if _, err := Run(ctx, src, "-c", "user.name=T", "-c", "user.email=t@e", "commit", "-q", "--allow-empty", "-m", "two"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMirrorCached(ctx, src, mirror); err != nil {
		t.Fatal(err)
	}
	cached, err := ListCommits(ctx, mirror, "HEAD", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached) != 1 {
		t.Fatalf("cached mirror unexpectedly fetched: %+v", cached)
	}
	if err := EnsureMirror(ctx, src, mirror); err != nil {
		t.Fatal(err)
	}
	fetched, err := ListCommits(ctx, mirror, "HEAD", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched) != 2 {
		t.Fatalf("refresh did not fetch: %+v", fetched)
	}
}
