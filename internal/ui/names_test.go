package ui

import "testing"

func TestProjectNamesStayShortUnlessTheyCollide(t *testing.T) {
	// Two roots that are both called infra.
	roots := []string{
		"/Users/dev/dev/acme/infra",
		"/Users/dev/dev/acme/platform",
		"/Users/dev/dev/acme/web-site",
		"/Users/dev/dev/globex/infra",
		"/Users/dev/dev/globex/tools",
		"/Users/dev/dev/globex/mobile-app",
	}
	got := projectNames(roots)
	want := map[string]string{
		roots[0]: "acme/infra",
		roots[1]: "platform",
		roots[2]: "web-site",
		roots[3]: "globex/infra",
		roots[4]: "tools",
		roots[5]: "mobile-app",
	}
	for r, w := range want {
		if got[r] != w {
			t.Errorf("projectNames[%s] = %q, want %q", r, got[r], w)
		}
	}
}

func TestProjectNamesAreAlwaysUnique(t *testing.T) {
	roots := []string{"/a/x/infra", "/b/x/infra", "/c/y/infra", "/solo/infra2"}
	got := projectNames(roots)
	seen := map[string]string{}
	for _, r := range roots {
		if other, dup := seen[got[r]]; dup {
			t.Errorf("%s and %s share the name %q", r, other, got[r])
		}
		seen[got[r]] = r
	}
	if got["/a/x/infra"] != "a/x/infra" || got["/b/x/infra"] != "b/x/infra" {
		t.Errorf("a collision on two levels must go deeper: %v", got)
	}
	if got["/c/y/infra"] != "y/infra" {
		t.Errorf("a root that needs fewer levels must not be over-extended: %q", got["/c/y/infra"])
	}
	if got["/solo/infra2"] != "infra2" {
		t.Errorf("unique roots keep the short name: %q", got["/solo/infra2"])
	}
}

func TestProjectNamesSameRootTwiceDoesNotLoop(t *testing.T) {
	got := projectNames([]string{"/a/infra", "/a/infra"})
	if got["/a/infra"] != "a/infra" && got["/a/infra"] != "infra" {
		t.Errorf("unexpected name %q", got["/a/infra"])
	}
}
