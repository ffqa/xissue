package runner

import (
	"testing"

	"github.com/xpier/xissue/internal/provider"
)

func TestDedupePRsByURL(t *testing.T) {
	prs := []provider.RemotePR{
		{Number: 7, URL: "https://github.com/acme/h5/pull/7", RepoID: 1, RepoName: "h5"},
		{Number: 7, URL: "https://github.com/acme/h5/pull/7/", RepoID: 2, RepoName: "h5-v2"},
		{Number: 8, URL: "https://github.com/acme/h5/pull/8", RepoID: 1, RepoName: "h5"},
	}
	got := dedupePRs(prs)
	if len(got) != 2 || got[0].Number != 7 || got[1].Number != 8 {
		t.Fatalf("dedupePRs = %+v", got)
	}
	if got[0].RepoName != "h5" {
		t.Fatalf("first duplicate should be kept, got %q", got[0].RepoName)
	}
}

func TestDedupePRsFallsBackToRepoAndNumber(t *testing.T) {
	prs := []provider.RemotePR{
		{Number: 3, RepoID: 9},
		{Number: 3, RepoID: 9},
		{Number: 3, RepoID: 10},
	}
	if got := dedupePRs(prs); len(got) != 2 {
		t.Fatalf("dedupePRs without URL = %+v", got)
	}
}
