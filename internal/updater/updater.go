// Package updater checks GitHub releases for xIssue updates.
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Release is the subset of the GitHub release API used by the updater.
type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []Asset `json:"assets"`
}

// Asset is one downloadable release artifact.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// FetchLatest returns the latest published GitHub release.
func FetchLatest(ctx context.Context) (*Release, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/ffqa/xissue/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "xissue-updater")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API 返回状态码 %d", resp.StatusCode)
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// IsNewer reports whether latest is a strictly newer dotted version than current.
func IsNewer(current, latest string) bool {
	cParts := ParseVersion(current)
	lParts := ParseVersion(latest)
	maxLen := len(cParts)
	if len(lParts) > maxLen {
		maxLen = len(lParts)
	}
	for len(cParts) < maxLen {
		cParts = append(cParts, 0)
	}
	for len(lParts) < maxLen {
		lParts = append(lParts, 0)
	}
	for i := 0; i < maxLen; i++ {
		if lParts[i] > cParts[i] {
			return true
		}
		if lParts[i] < cParts[i] {
			return false
		}
	}
	return false
}

// ParseVersion parses "v1.2.3" (or "1.2.3-beta") into numeric components.
func ParseVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	res := make([]int, 0, len(parts))
	for _, p := range parts {
		var numStr string
		for _, r := range p {
			if r >= '0' && r <= '9' {
				numStr += string(r)
			} else {
				break
			}
		}
		n, _ := strconv.Atoi(numStr)
		res = append(res, n)
	}
	return res
}

// FindMatchingAsset picks the tar.gz/zip archive for a GOOS/GOARCH pair.
func FindMatchingAsset(rel *Release, goos, goarch string) (string, string) {
	pattern := fmt.Sprintf("%s-%s", goos, goarch)
	for _, asset := range rel.Assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, pattern) && (strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip")) {
			return asset.Name, asset.BrowserDownloadURL
		}
	}
	return "", ""
}

// FindSumsURL returns the SHA256SUMS.txt asset URL, if present.
func FindSumsURL(rel *Release) string {
	for _, asset := range rel.Assets {
		if asset.Name == "SHA256SUMS.txt" {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}
