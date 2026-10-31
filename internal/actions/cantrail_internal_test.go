package actions

import "testing"

func TestCanTrail(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"key: value", true},
		{"  key: value", true},
		{"", false},
		{"   ", false},
		{"# just a comment", false},
		{"key: value # trailing comment", false},
		// A '#' inside a quoted scalar is data, not a comment opener.
		{`key: "a # b"`, true},
		{`key: 'a # b'`, true},
		{`name: "value with # hash"`, true},
		{`msg: "say \" # x"`, true},
		// An apostrophe inside a plain scalar is not a quote opener.
		{`msg: it's # note`, false},
		{`msg: it's fine`, true},
		{`- 'a # b'`, true},
		{`k: ['a # b', c]`, true},
		{`k: ['a', c] # note`, false},
	}
	for _, c := range cases {
		if got := canTrail(c.line); got != c.want {
			t.Errorf("canTrail(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}
