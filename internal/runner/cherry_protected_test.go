package runner

import "testing"

func TestProtectedCherrySource(t *testing.T) {
	for _, branch := range []string{"main", "master", "dev", "develop", "production", "staging", "MAIN"} {
		if !IsProtectedCherrySource(branch) {
			t.Fatalf("expected %q protected", branch)
		}
	}
	for _, branch := range []string{"feat/login", "fix/bug", "agent/run-1", ""} {
		if IsProtectedCherrySource(branch) {
			t.Fatalf("expected %q allowed", branch)
		}
	}
}
