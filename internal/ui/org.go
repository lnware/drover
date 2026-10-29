package ui

import (
	"path/filepath"
	"slices"
	"sort"
	"unicode"
)

// projectInfo says how one project root appears in the list.
type projectInfo struct {
	org     string // header above the project; empty when orgs are not shown
	display string // project name under that header
	unique  string // session-name prefix, always unique across roots
}

// orgOf names the group a project root belongs to: the folder that holds it,
// capitalised, so ~/dev/acme/infra sits under "Acme".
func orgOf(root string) string {
	parent := filepath.Base(filepath.Dir(filepath.Clean(root)))
	if parent == "." || parent == "/" || parent == "" {
		return ""
	}
	r := []rune(parent)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// layoutProjects decides, for every configured root, its org header and
// display name. Org headers appear only when the roots live in more than one
// parent folder; with a single parent they would just be noise. The unique
// name (see projectNames) always prefixes sessions, whatever is displayed.
func layoutProjects(roots []string) map[string]projectInfo {
	unique := projectNames(roots)
	parents := map[string]bool{}
	for _, r := range roots {
		parents[orgOf(r)] = true
	}
	showOrgs := len(parents) >= 2

	info := make(map[string]projectInfo, len(roots))
	taken := map[[2]string]int{}
	for _, r := range roots {
		pi := projectInfo{display: unique[r], unique: unique[r]}
		if showOrgs {
			pi.org, pi.display = orgOf(r), filepath.Base(filepath.Clean(r))
			taken[[2]string{pi.org, pi.display}]++
		}
		info[r] = pi
	}
	// Two roots with the same folder name under one org keep their longer
	// unique names, so the header never shows an ambiguous project.
	for r, pi := range info {
		if showOrgs && taken[[2]string{pi.org, pi.display}] > 1 {
			pi.display = pi.unique
			info[r] = pi
		}
	}
	return info
}

// orderByOrg groups rows by org, in order of first appearance, keeping the
// configured project order and each project's own row order.
func orderByOrg(rows []row) {
	var order []string
	for _, r := range rows {
		if !slices.Contains(order, r.org) {
			order = append(order, r.org)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return slices.Index(order, rows[i].org) < slices.Index(order, rows[j].org)
	})
}
