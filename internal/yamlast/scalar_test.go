package yamlast

import "testing"

func TestQuoteString(t *testing.T) {
	tests := []struct{ in, want string }{
		{"nginx", "nginx"},
		{"ClusterIP", "ClusterIP"},
		{"http://x/y", "http://x/y"},
		{"a b", "a b"},
		{"true", `"true"`},
		{"False", `"False"`},
		{"null", `"null"`},
		{"~", `"~"`},
		{"yes", `"yes"`},
		{"on", `"on"`},
		{"1.0", `"1.0"`},
		{"10", `"10"`},
		{"0x1F", `"0x1F"`},
		{"1e3", `"1e3"`},
		{".inf", `".inf"`},
		{"", `""`},
		{"a: b", `"a: b"`},
		{"#x", `"#x"`},
		{"x #y", `"x #y"`},
		{"- x", `"- x"`},
		{"*x", `"*x"`},
		{"&x", `"&x"`},
		{"!x", `"!x"`},
		{"@x", `"@x"`},
		{"`x", "\"`x\""},
		{" x", `" x"`},
		{"x ", `"x "`},
		{"a\nb", `"a\nb"`},
		{`say "hi"`, `say "hi"`},
		{`"x`, `"\"x"`},
		{"k:", `"k:"`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := QuoteString(tt.in); got != tt.want {
				t.Errorf("QuoteString(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}
