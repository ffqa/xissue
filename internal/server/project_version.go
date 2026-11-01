package server

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
)

var semverTag = regexp.MustCompile(`^v?\d+\.\d+(\.\d+)?([-.+][0-9A-Za-z.-]+)?$`)

type projectVersionEntry struct {
	version string
	at      time.Time
}

// fillProjectVersions annotates projects with the newest semver tag found in
// their mirrored repositories (the first repo that has a tag wins).
func (s *Server) fillProjectVersions(ctx context.Context, projects []domain.Project) {
	for i := range projects {
		s.fillProjectVersion(ctx, &projects[i])
	}
}

func (s *Server) fillProjectVersion(ctx context.Context, p *domain.Project) {
	for _, repo := range p.Repos {
		if v := s.repoLatestTag(ctx, repo.ID); v != "" {
			p.Version = v
			return
		}
	}
}

func (s *Server) repoLatestTag(ctx context.Context, repoID int64) string {
	if cached, ok := s.versionCache.Load(repoID); ok {
		entry := cached.(projectVersionEntry)
		if time.Since(entry.at) < 2*time.Minute {
			return entry.version
		}
	}
	mirror := filepath.Join(s.cfg.MirrorsDir(), fmt.Sprintf("repo-%d.git", repoID))
	version := ""
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if out, err := gitx.Run(cctx, mirror, "for-each-ref", "--sort=-v:refname", "--count=20", "--format=%(refname:short)", "refs/tags"); err == nil {
		for _, tag := range strings.Split(out, "\n") {
			if tag = strings.TrimSpace(tag); semverTag.MatchString(tag) {
				version = tag
				break
			}
		}
	}
	s.versionCache.Store(repoID, projectVersionEntry{version: version, at: time.Now()})
	return version
}
