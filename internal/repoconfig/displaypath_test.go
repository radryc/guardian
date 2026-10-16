package repoconfig

import "testing"

func TestDisplayPathFromSource(t *testing.T) {
	cases := map[string]string{
		"https://github.com/example/strata.git":      "github.com/example/strata",
		"https://github.com/example/strata":          "github.com/example/strata",
		"git@github.com:example/strata.git":          "github.com/example/strata",
		"https://user@gitlab.com/group/project.git":  "gitlab.com/group/project",
		"ssh://git@gitlab.com/group/sub/project.git": "gitlab.com/group/sub/project",
		"/.repos/github.com/example/strata":          "github.com/example/strata",
		"/.repos/github.com/example/strata/":         "github.com/example/strata",
		"":                                           "",
		"not-a-url":                                  "",
		"/tmp/repo":                                  "",
	}
	for input, want := range cases {
		if got := DisplayPathFromSource(input); got != want {
			t.Errorf("DisplayPathFromSource(%q) = %q, want %q", input, got, want)
		}
	}
}
