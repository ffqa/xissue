package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGetPRDetailsParsesCIStatusAndHeadSHA(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s' '{"number":7,"title":"fix","state":"OPEN","mergedAt":null,"url":"https://github.test/acme/repo/pull/7","headRefName":"feat","headRefOid":"abc123","baseRefName":"main","author":{"login":"alice"},"comments":[],"latestReviews":[],"statusCheckRollup":[{"status":"COMPLETED","conclusion":"FAILURE"},{"state":"PENDING"},{"status":"COMPLETED","conclusion":"SUCCESS"}]}'
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	detail, err := GetPRDetails(context.Background(), "https://github.test/acme/repo/pull/7")
	if err != nil {
		t.Fatal(err)
	}
	if detail.HeadSHA != "abc123" {
		t.Fatalf("head sha = %q", detail.HeadSHA)
	}
	if detail.CIState != "failure" || detail.CIFailed != 1 || detail.CIPending != 1 || detail.CIPassed != 1 {
		t.Fatalf("ci = %s passed=%d failed=%d pending=%d", detail.CIState, detail.CIPassed, detail.CIFailed, detail.CIPending)
	}
}
