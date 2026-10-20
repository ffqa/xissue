package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	pathOnce sync.Once
)

// candidateDirs returns standard user/package-manager bin directories across OSes.
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
			filepath.Join(home, ".fnm", "current", "bin"),
			filepath.Join(home, ".volta", "bin"),
		)

		// Scan latest nvm node versions if present
		nvmDir := filepath.Join(home, ".nvm", "versions", "node")
		if entries, err := os.ReadDir(nvmDir); err == nil {
			for i := len(entries) - 1; i >= 0; i-- {
				if entries[i].IsDir() {
					list = append(list, filepath.Join(nvmDir, entries[i].Name(), "bin"))
					break
				}
			}
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
	}

	return list
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
			return target, nil
		}
	}

	return "", exec.ErrNotFound
}
