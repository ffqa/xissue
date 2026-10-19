package gitx

import (
	"testing"
)

func TestValidateBranchName(t *testing.T) {
	valids := []string{"main", "dev", "feat-xxx", "feat/login", "user_123.test", "cp/main-123"}
	for _, b := range valids {
		if err := ValidateBranchName(b); err != nil {
			t.Errorf("expected %q to be valid, got: %v", b, err)
		}
	}

	invalids := []string{"", "-flag", "--opt", "feat space", "branch..name", "branch~1", "branch^", "b:name", "b?name", "b*name", "b[name"}
	for _, b := range invalids {
		if err := ValidateBranchName(b); err == nil {
			t.Errorf("expected %q to be invalid, got nil", b)
		}
	}
}

func TestValidateSHA(t *testing.T) {
	valids := []string{"66ad2c1", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", "1234567"}
	for _, sha := range valids {
		if err := ValidateSHA(sha); err != nil {
			t.Errorf("expected %q to be valid, got: %v", sha, err)
		}
	}

	invalids := []string{"", "123", "12345", "66ad2c1z", "66ad 2c1", "-flag"}
	for _, sha := range invalids {
		if err := ValidateSHA(sha); err == nil {
			t.Errorf("expected %q to be invalid, got nil", sha)
		}
	}
}
