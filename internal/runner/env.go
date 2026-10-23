package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var (
	pathOnce sync.Once
)

// candidateDirs returns standard user/package-manager bin directories across OSes.
// Background services (launchd/systemd) start with a minimal PATH, so version
// managers such as nvm/fnm/asdf/mise/Herd must be discovered explicitly.
func candidateDirs() []string {
	var list []string
	home, _ := os.UserHomeDir()

	if home != "" {
		list = append(list,
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".yarn", "bin"),
			filepath.Join(home, ".config", "yarn", "global", "node_modules", ".bin"),
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".nvm", "current", "bin"),
			filepath.Join(home, "Library", "Application Support", "Herd", "config", "nvm", "current", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".local", "share", "pnpm"),
			filepath.Join(home, "Library", "pnpm"),
		)

		// Node version managers keep a bin directory per installed version.
		for _, root := range []string{
			filepath.Join(home, ".nvm", "versions", "node"),
			filepath.Join(home, ".config", "nvm", "versions", "node"),
			filepath.Join(home, ".local", "share", "nvm", "versions", "node"),
			filepath.Join(home, "Library", "Application Support", "Herd", "config", "nvm", "versions", "node"),
			filepath.Join(home, ".asdf", "installs", "nodejs"),
			filepath.Join(home, ".local", "share", "mise", "installs", "node"),
			filepath.Join(home, "Library", "Application Support", "mise", "installs", "node"),
		} {
			list = append(list, versionedBins(root, "bin")...)
		}
		// fnm stores each Node version under installation/bin.
		for _, root := range []string{
			filepath.Join(home, ".fnm", "node-versions"),
			filepath.Join(home, ".local", "share", "fnm", "node-versions"),
			filepath.Join(home, "Library", "Application Support", "fnm", "node-versions"),
		} {
			list = append(list, versionedBins(root, filepath.Join("installation", "bin"))...)
		}
	}

	if runtime.GOOS == "darwin" {
		list = append(list,
			"/opt/homebrew/bin",
			"/opt/homebrew/sbin",
			"/usr/local/bin",
			"/usr/local/sbin",
		)
	} else if runtime.GOOS == "linux" {
		list = append(list,
			"/home/linuxbrew/.linuxbrew/bin",
			"/home/linuxbrew/.linuxbrew/sbin",
			"/usr/local/bin",
			"/usr/local/sbin",
			"/snap/bin",
		)
	} else if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			list = append(list, filepath.Join(appData, "npm"))
		}
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			list = append(list, filepath.Join(localAppData, "pnpm"))
		}
	}
	return list
}

// versionedBins returns <root>/<version>/<suffix> directories, newest version
// first. It tolerates missing roots (version manager not installed).
func versionedBins(root, suffix string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var versions []string
	for _, entry := range entries {
		if entry.IsDir() {
			versions = append(versions, entry.Name())
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i], versions[j]) > 0
	})
	out := make([]string, 0, len(versions))
	for _, version := range versions {
		out = append(out, filepath.Join(root, version, suffix))
	}
	return out
}

// compareVersions compares simple dotted versions such as v22.22.3 and 9.11.2.
// Non-numeric suffixes are ignored; unknown forms fall back to string order.
func compareVersions(a, b string) int {
	pa := versionNumbers(a)
	pb := versionNumbers(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var av, bv int
		if i < len(pa) {
			av = pa[i]
		}
		if i < len(pb) {
			bv = pb[i]
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return strings.Compare(a, b)
}

func versionNumbers(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimLeft(part, "0")
		if part == "" {
			out = append(out, 0)
			continue
		}
		// Stop at prerelease/build separators (e.g. 22.22.3-rc.1).
		if i := strings.IndexAny(part, "-+"); i >= 0 {
			part = part[:i]
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

// EnsureEnhancedPath ensures that user-level tools and package-manager bin paths
// (often omitted in systemd / launchd background services) are appended to the process PATH.
func EnsureEnhancedPath() {
	pathOnce.Do(func() {
		currentPath := os.Getenv("PATH")
		pathSeparator := string(os.PathListSeparator)
		existing := map[string]bool{}
		for _, p := range strings.Split(currentPath, pathSeparator) {
			clean := filepath.Clean(strings.TrimSpace(p))
			if clean != "" {
				existing[clean] = true
			}
		}

		var toAdd []string
		for _, cand := range candidateDirs() {
			clean := filepath.Clean(cand)
			if clean == "" || existing[clean] {
				continue
			}
			if st, err := os.Stat(clean); err == nil && st.IsDir() {
				toAdd = append(toAdd, clean)
				existing[clean] = true
			}
		}

		if len(toAdd) > 0 {
			newPath := currentPath
			if newPath != "" {
				newPath += pathSeparator
			}
			newPath += strings.Join(toAdd, pathSeparator)
			_ = os.Setenv("PATH", newPath)
		}
	})
}

// LookupAgentBinary finds the executable path for a command, checking PATH and common user directories.
func LookupAgentBinary(command string) (string, error) {
	EnsureEnhancedPath()

	// 1. Direct LookPath with enhanced PATH
	if p, err := exec.LookPath(command); err == nil {
		return p, nil
	}

	// 2. If command already contains path separators, check directly
	if strings.ContainsAny(command, `/\`) {
		if st, err := os.Stat(command); err == nil && !st.IsDir() {
			return command, nil
		}
	}

	// 3. Fallback scan candidate directories
	for _, dir := range candidateDirs() {
		target := filepath.Join(dir, command)
		if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(target), ".exe") {
			target += ".exe"
		}
		if st, err := os.Stat(target); err == nil && !st.IsDir() {
			// The resolved command may rely on siblings (e.g. npx -> node via
			// shebang), so make its directory visible to child processes.
			prependPathDir(dir)
			return target, nil
		}
	}

	return "", exec.ErrNotFound
}

func prependPathDir(dir string) {
	clean := filepath.Clean(dir)
	if clean == "" {
		return
	}
	sep := string(os.PathListSeparator)
	for _, part := range strings.Split(os.Getenv("PATH"), sep) {
		if filepath.Clean(strings.TrimSpace(part)) == clean {
			return
		}
	}
	_ = os.Setenv("PATH", clean+sep+os.Getenv("PATH"))
}
