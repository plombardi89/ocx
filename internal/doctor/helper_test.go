package doctor

import "testing"

func TestHelperProgram(t *testing.T) {
	cases := map[string]string{
		"store":                        "store",
		"cache --timeout=3600":         "cache",
		`/opt/My\ Tools/helper get`:    "/opt/My Tools/helper",
		`"/opt/My Tools/helper" --x`:   "/opt/My Tools/helper",
		`'/opt/My Tools/helper'`:       "/opt/My Tools/helper",
		"/usr/lib/git-core/git-cred-x": "/usr/lib/git-core/git-cred-x",
	}
	for in, want := range cases {
		if got := helperProgram(in); got != want {
			t.Errorf("helperProgram(%q) = %q, want %q", in, got, want)
		}
	}
}
