package updater

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"0.2.15", "0.2.16", true},
		{"v0.2.15", "v0.2.15", false},
		{"0.2.15", "0.2.15", false},
		{"0.2.9", "0.2.10", true},
		{"1.0", "1.0.0", false},
		{"1.0.1", "1.0", false},
		{"0.3.0-beta", "0.3.0", false},
	}
	for _, tc := range cases {
		if got := IsNewer(tc.current, tc.latest); got != tc.want {
			t.Fatalf("IsNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}
