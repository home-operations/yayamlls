package links

import "testing"

func TestLinks_ModelineSchemaURL(t *testing.T) {
	text := "# yaml-language-server: $schema=https://example.com/s.json\nname: x\n"
	got := Links(text)
	if len(got) != 1 {
		t.Fatalf("expected 1 link, got %d", len(got))
	}
	l := got[0]
	if l.Target == nil || *l.Target != "https://example.com/s.json" {
		t.Errorf("target = %v, want https://example.com/s.json", l.Target)
	}
	if l.Range.Start.Line != 0 || l.Range.End.Line != 0 {
		t.Errorf("link should be on line 0, got %+v", l.Range)
	}
	wantStart := uint32(len("# yaml-language-server: $schema="))
	if l.Range.Start.Character != wantStart {
		t.Errorf("start char = %d, want %d", l.Range.Start.Character, wantStart)
	}
}

func TestLinks_RelativeSchemaIgnored(t *testing.T) {
	text := "# yaml-language-server: $schema=./local.json\n"
	if got := Links(text); len(got) != 0 {
		t.Errorf("relative paths shouldn't produce DocumentLinks: %+v", got)
	}
}

func TestLinks_NoModeline(t *testing.T) {
	if got := Links("name: x\n"); len(got) != 0 {
		t.Errorf("expected 0 links, got %+v", got)
	}
}

func TestLinks_RangeIsUTF16AndAnchoredOnSchemaField(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		start uint32
		end   uint32
	}{
		{
			name:  "URL text repeated earlier on the line",
			text:  "# yaml-language-server: foo=https://x.io/a $schema=https://x.io/a\n",
			start: uint32(len("# yaml-language-server: foo=https://x.io/a $schema=")),
			end:   uint32(len("# yaml-language-server: foo=https://x.io/a $schema=https://x.io/a")),
		},
		{
			name:  "astral and CJK characters before the URL",
			text:  "# yaml-language-server: note=😀日本 $schema=https://x.io/s\n",
			start: 24 + 5 + 2 + 2 + 1 + 8,
			end:   24 + 5 + 2 + 2 + 1 + 8 + uint32(len("https://x.io/s")),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Links(tt.text)
			if len(got) != 1 {
				t.Fatalf("expected 1 link, got %d", len(got))
			}
			if r := got[0].Range; r.Start.Character != tt.start || r.End.Character != tt.end {
				t.Errorf("range chars = %d..%d, want %d..%d", r.Start.Character, r.End.Character, tt.start, tt.end)
			}
		})
	}
}
