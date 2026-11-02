package runner

import (
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestRepoCanListPRs(t *testing.T) {
	cases := []struct {
		name string
		repo domain.Repo
		want bool
	}{
		{"plain local", domain.Repo{Provider: domain.ProviderPlain, RemoteURL: "/tmp/x"}, false},
		{"github remote", domain.Repo{Provider: domain.ProviderGitHub, OwnerRepo: "acme/h5", RemoteURL: "https://github.com/acme/h5.git"}, true},
		{"github ssh remote", domain.Repo{Provider: domain.ProviderGitHub, OwnerRepo: "acme/h5", RemoteURL: "git@github.com:acme/h5.git"}, true},
		{"github without owner", domain.Repo{Provider: domain.ProviderGitHub, RemoteURL: "https://github.com/acme/h5.git"}, false},
		{"stale github on local dir", domain.Repo{Provider: domain.ProviderGitHub, OwnerRepo: "acme/h5", RemoteURL: "/tmp/h5-v2"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := repoCanListPRs(tc.repo); got != tc.want {
				t.Fatalf("repoCanListPRs = %v, want %v", got, tc.want)
			}
		})
	}
}
