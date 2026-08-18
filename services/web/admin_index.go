package web

import (
	"os"
	"path/filepath"
	"strings"
)

func preferVueAdmin() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HR_ADMIN_VUE")))
	if v == "0" || v == "false" || v == "legacy" {
		return false
	}
	if v == "1" || v == "true" || v == "yes" {
		return true
	}
	return AdminVueDistDir() != ""
}

func useVueAdmin() bool {
	return preferVueAdmin()
}

// AdminVueDistDir returns absolute path to admin-ui/dist when present (cwd + exe-relative).
func AdminVueDistDir() string {
	var candidates []string
	candidates = append(candidates, "web/admin-ui/dist")
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "web", "admin-ui", "dist"),
			filepath.Join(dir, "..", "web", "admin-ui", "dist"),
		)
	}
	for _, p := range candidates {
		if st, err := os.Stat(filepath.Join(p, "index.html")); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	return ""
}

// AdminIndexHTML returns admin UI bytes and a short source label (file:… or embed).
// Prefers admin-ui/dist when built; set HR_ADMIN_VUE=legacy to force old index.html.
func AdminIndexHTML() ([]byte, string) {
	if p := os.Getenv("HR_ADMIN_STATIC_FILE"); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			return b, "file:" + p
		}
	}
	if preferVueAdmin() {
		if dist := AdminVueDistDir(); dist != "" {
			if b, err := os.ReadFile(filepath.Join(dist, "index.html")); err == nil {
				return b, "vue:" + dist
			}
		}
	}
	var candidates []string
	candidates = append(candidates, "web/admin/index.html")
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "web", "admin", "index.html"),
			filepath.Join(dir, "..", "web", "admin", "index.html"),
		)
	}
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			abs, _ := filepath.Abs(p)
			return b, "file:" + abs
		}
	}
	raw, err := FS.ReadFile("admin/index.html")
	if err != nil {
		return nil, ""
	}
	return raw, "embed"
}

// AdminVueAsset reads a file under admin-ui/dist (e.g. assets/index-xxx.js).
func AdminVueAsset(rel string) ([]byte, string, bool) {
	if !preferVueAdmin() {
		return nil, "", false
	}
	dist := AdminVueDistDir()
	if dist == "" {
		return nil, "", false
	}
	rel = filepath.Clean(strings.TrimPrefix(rel, "/"))
	if strings.Contains(rel, "..") {
		return nil, "", false
	}
	full := filepath.Join(dist, rel)
	if !strings.HasPrefix(full, dist) {
		return nil, "", false
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return nil, "", false
	}
	return b, full, true
}
