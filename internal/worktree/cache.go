package worktree

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
)

// Listing worktrees costs a git process per project, and the answer only
// changes when something under the git directory changes: a worktree added or
// removed (the worktrees/ directory), or a branch switched or renamed inside
// one (that worktree's HEAD). Both show up in file modification times, which
// a few stat calls read without spawning anything. The UI refreshes the list
// often, so this turns most refreshes into a handful of stats.

// fingerprint summarises the files whose changes alter a worktree listing.
type fingerprint struct {
	latest int64 // newest modification time among them, in ns
	count  int   // how many were found, to notice removals
}

// fingerprintOf reads gitDir's watched files. ok is false when the layout is
// not one it understands, in which case the caller must not cache.
func fingerprintOf(gitDir string) (fp fingerprint, ok bool) {
	watch := func(path string) bool {
		st, err := os.Stat(path)
		if err != nil {
			return false
		}
		fp.latest = max(fp.latest, st.ModTime().UnixNano())
		fp.count++
		return true
	}
	if !watch(filepath.Join(gitDir, "HEAD")) {
		return fp, false
	}
	dir := filepath.Join(gitDir, "worktrees")
	if !watch(dir) {
		return fp, true // no linked worktrees yet: HEAD alone decides
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fp, false
	}
	for _, e := range entries {
		if e.IsDir() {
			watch(filepath.Join(dir, e.Name()))
			watch(filepath.Join(dir, e.Name(), "HEAD"))
		}
	}
	return fp, true
}

type cached struct {
	fp   fingerprint
	list []Worktree
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cached{}
	// gitListRuns counts real `git worktree list` runs, so tests can tell a
	// cache hit from a spawn.
	gitListRuns atomic.Int64
)

func lookup(root string, fp fingerprint) ([]Worktree, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	c, ok := cache[root]
	if !ok || c.fp != fp {
		return nil, false
	}
	return slices.Clone(c.list), true
}

func store(root string, fp fingerprint, list []Worktree) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache[root] = cached{fp: fp, list: slices.Clone(list)}
}

// invalidate forgets a project's cached listing. Add and Remove call it, so
// a change made by drover itself is never hidden behind a coarse mtime.
func invalidate(root string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	delete(cache, root)
}
