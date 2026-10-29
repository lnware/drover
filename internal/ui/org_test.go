package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/worktree"
)

var realRoots = []string{
	"/Users/dev/dev/acme/infra",
	"/Users/dev/dev/acme/platform",
	"/Users/dev/dev/acme/web-site",
	"/Users/dev/dev/globex/infra",
	"/Users/dev/dev/globex/tools",
	"/Users/dev/dev/globex/mobile-app",
}

func TestOrgOf(t *testing.T) {
	for in, want := range map[string]string{
		"/Users/dev/dev/acme/infra":    "Acme",
		"/Users/dev/dev/globex/tools/": "Globex",
		"/work/äpäjä/x":                "Äpäjä",
		"/infra":                       "",
		"infra":                        "",
	} {
		if got := orgOf(in); got != want {
			t.Errorf("orgOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLayoutProjectsForTheRealSetup(t *testing.T) {
	info := layoutProjects(realRoots)
	want := []struct{ org, display, unique string }{
		{"Acme", "infra", "acme/infra"},
		{"Acme", "platform", "platform"},
		{"Acme", "web-site", "web-site"},
		{"Globex", "infra", "globex/infra"},
		{"Globex", "tools", "tools"},
		{"Globex", "mobile-app", "mobile-app"},
	}
	for i, r := range realRoots {
		got := info[r]
		if got.org != want[i].org || got.display != want[i].display || got.unique != want[i].unique {
			t.Errorf("%s = %+v, want %+v", r, got, want[i])
		}
	}
}

func TestSingleParentShowsNoOrgHeader(t *testing.T) {
	info := layoutProjects([]string{"/dev/acme/infra", "/dev/acme/platform"})
	for r, pi := range info {
		if pi.org != "" {
			t.Errorf("%s has org %q although every project shares one parent", r, pi.org)
		}
	}
	// without headers the display name is the unique one
	for r, pi := range info {
		if pi.display != pi.unique {
			t.Errorf("%s: display %q differs from unique %q while no org is shown", r, pi.display, pi.unique)
		}
	}
	// different parents with the same folder name are told apart by the header
	info = layoutProjects([]string{"/a/x/infra", "/a/y/infra"})
	if a, b := info["/a/x/infra"], info["/a/y/infra"]; a.org == b.org || a.unique == b.unique {
		t.Errorf("same-named projects must differ by org and session name: %+v %+v", a, b)
	}
}

func TestSameNameInOneOrgKeepsLongerName(t *testing.T) {
	info := layoutProjects([]string{"/dev/acme/infra", "/dev/other/infra", "/dev/acme/a/infra"})
	seen := map[[2]string]string{}
	for r, pi := range info {
		key := [2]string{pi.org, pi.display}
		if prev, dup := seen[key]; dup {
			t.Errorf("%s and %s are both %q under %q", r, prev, pi.display, pi.org)
		}
		seen[key] = r
	}
}

func TestOrderByOrgKeepsProjectAndRowOrder(t *testing.T) {
	rows := []row{
		{org: "Acme", project: "a", session: "1"},
		{org: "Globex", project: "b", session: "2"},
		{org: "Acme", project: "c", session: "3"},
		{org: "Acme", project: "c", session: "4"},
		{org: "Globex", project: "d", session: "5"},
	}
	orderByOrg(rows)
	var got []string
	for _, r := range rows {
		got = append(got, r.session)
	}
	if strings.Join(got, "") != "13425" {
		t.Errorf("order = %v, want orgs grouped in first-appearance order with stable rows", got)
	}
}

func orgModel() Model {
	m := testModel()
	mk := func(org, project, group, branch, path string, running bool) row {
		return row{
			org: org, project: project, root: "/dev/" + org + "/" + project,
			wt:      worktree.Worktree{Path: path, Branch: branch, Group: group},
			session: project + "/" + branch, running: running,
		}
	}
	m.rows = []row{
		mk("Acme", "infra", "base", "main", "/dev/acme/infra/main", true),
		mk("Acme", "platform", "feature", "internals", "/dev/acme/platform/feature/internals", false),
		mk("Globex", "tools", "chore", "api-scripts", "/dev/globex/tools/chore/api-scripts", false),
	}
	m.all = m.rows
	m.selSession = m.rows[0].session
	return m
}

func TestOrgBandsRenderAndKeepWidth(t *testing.T) {
	m := orgModel()
	view := m.render()
	plain := strings.Split(ansi.Strip(view), "\n")
	var bands []int
	for i, l := range plain {
		if strings.Contains(l[:min(40, len(l))], "Acme") || strings.Contains(l[:min(40, len(l))], "Globex") {
			bands = append(bands, i)
		}
	}
	if len(bands) != 2 {
		t.Fatalf("want one band per org, got lines %v:\n%s", bands, ansi.Strip(view))
	}
	// the band is a filled bar: it must carry a background colour
	if !strings.Contains(strings.Split(view, "\n")[bands[0]], "\x1b[") {
		t.Error("band line has no styling")
	}
	for i, l := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(l); w != m.width {
			t.Errorf("line %d width %d, want %d", i, w, m.width)
		}
	}
	// project names sit under their band
	order := []string{"Acme", "infra", "platform", "Globex", "tools"}
	pos := 0
	for _, want := range order {
		i := strings.Index(strings.Join(plain, "\n")[pos:], want)
		if i < 0 {
			t.Fatalf("%q not found after position %d in order %v", want, pos, order)
		}
		pos += i + len(want)
	}
}

func TestSearchByOrgAndFilteredBandsDisappear(t *testing.T) {
	m := orgModel()
	m.search = "globex"
	m.applyFilter()
	if len(m.rows) != 1 || m.rows[0].project != "tools" {
		t.Fatalf("search by org: %+v", m.rows)
	}
	plain := ansi.Strip(m.render())
	if strings.Contains(plain, "Acme") {
		t.Error("an org with no visible rows must not show its band")
	}
	if !strings.Contains(plain, "Globex") {
		t.Error("the matching org's band is missing")
	}
}
