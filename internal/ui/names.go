package ui

import (
	"path/filepath"
	"strings"
)

// projectNames gives every project root a short, unique name. It is the
// folder name, with parent folders added only while two roots would
// otherwise collide: ~/dev/acme/infra and ~/dev/globex/infra become
// acme/infra and globex/infra. The name prefixes tmux session names and
// status files, so a collision would let one project attach to another's
// worktree.
func projectNames(roots []string) map[string]string {
	parts := make(map[string][]string, len(roots))
	depth := make(map[string]int, len(roots))
	for _, r := range roots {
		var segs []string
		for _, s := range strings.Split(filepath.ToSlash(filepath.Clean(r)), "/") {
			if s != "" {
				segs = append(segs, s)
			}
		}
		parts[r], depth[r] = segs, 1
	}
	name := func(r string) string {
		segs, d := parts[r], depth[r]
		return strings.Join(segs[max(len(segs)-d, 0):], "/")
	}
	for {
		count := map[string]int{}
		for _, r := range roots {
			count[name(r)]++
		}
		grew := false
		for _, r := range roots {
			if count[name(r)] > 1 && depth[r] < len(parts[r]) {
				depth[r]++
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	out := make(map[string]string, len(roots))
	for _, r := range roots {
		out[r] = name(r)
	}
	return out
}

// projectName is root's display name among the configured projects.
func (m Model) projectName(root string) string {
	if n, ok := projectNames(m.cfg.Projects)[root]; ok && n != "" {
		return n
	}
	return filepath.Base(root)
}
