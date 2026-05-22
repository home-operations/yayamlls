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
