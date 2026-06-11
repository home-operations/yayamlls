package render_test

import (
	"testing"
	"time"

	"github.com/home-operations/yayamlls/internal/render"
)

func TestPipeline_CancelDropsPendingRender(t *testing.T) {
	reg := render.NewRegistry()
	fr := &fakeRenderer{
		name:    "fake",
		matches: true,
		out:     []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: x\n"),
	}
	reg.Register(fr)
	sink := newRecordingSink()
	p := render.NewPipeline(reg, sink)
	p.SetDebounce(50 * time.Millisecond)

	uri := "file:///tmp/x.yaml"
	p.Schedule(&render.SourceDocument{URI: uri, Text: "a"})
	p.Cancel(uri) // before the debounce fires

	select {
	case <-sink.done:
		t.Fatal("cancelled render should not deliver")
	case <-time.After(150 * time.Millisecond):
	}
	if fr.calls != 0 {
		t.Errorf("expected 0 render calls after cancel, got %d", fr.calls)
	}
}

func TestPipeline_CacheHitSkipsRenderAndNotifiesImmediately(t *testing.T) {
	reg := render.NewRegistry()
	fr := &fakeRenderer{
		name:    "fake",
		matches: true,
		out:     []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: x\n"),
	}
	reg.Register(fr)
	sink := newRecordingSink()
	p := render.NewPipeline(reg, sink)
	p.SetDebounce(time.Millisecond)

	doc := &render.SourceDocument{URI: "file:///tmp/x.yaml", Text: "a"}
	p.Schedule(doc)
	select {
	case <-sink.done:
	case <-time.After(2 * time.Second):
		t.Fatal("first render never delivered")
	}

	// Same URI and content: the memoized result must be replayed without a
	// second render (and without waiting out the debounce).
	p.Schedule(doc)
	select {
	case <-sink.done:
	case <-time.After(2 * time.Second):
		t.Fatal("cache hit never delivered")
	}
	if fr.calls != 1 {
		t.Errorf("expected 1 render call (second was a cache hit), got %d", fr.calls)
	}
}

func TestPipeline_InvalidateAllForcesRerender(t *testing.T) {
	reg := render.NewRegistry()
	fr := &fakeRenderer{
		name:    "fake",
		matches: true,
		out:     []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: x\n"),
	}
	reg.Register(fr)
	sink := newRecordingSink()
	p := render.NewPipeline(reg, sink)
	p.SetDebounce(time.Millisecond)

	doc := &render.SourceDocument{URI: "file:///tmp/x.yaml", Text: "a"}
	p.Schedule(doc)
	select {
	case <-sink.done:
	case <-time.After(2 * time.Second):
		t.Fatal("first render never delivered")
	}

	// Identical content would normally replay from the cache; after
	// InvalidateAll it must render again (the on-disk tree changed).
	p.InvalidateAll()
	p.Schedule(doc)
	select {
	case <-sink.done:
	case <-time.After(2 * time.Second):
		t.Fatal("post-invalidation render never delivered")
	}
	if fr.calls != 2 {
		t.Errorf("expected 2 render calls after InvalidateAll, got %d", fr.calls)
	}
}

func TestPipeline_LatestMatchesCurrentTextOnly(t *testing.T) {
	reg := render.NewRegistry()
	fr := &fakeRenderer{
		name:    "fake",
		matches: true,
		out:     []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: x\n"),
	}
	reg.Register(fr)
	sink := newRecordingSink()
	p := render.NewPipeline(reg, sink)
	p.SetDebounce(time.Millisecond)

	uri := "file:///tmp/x.yaml"
	if _, ok := p.Latest(uri, "a"); ok {
		t.Fatal("Latest before any render should miss")
	}

	p.Schedule(&render.SourceDocument{URI: uri, Text: "a"})
	select {
	case <-sink.done:
	case <-time.After(2 * time.Second):
		t.Fatal("render never delivered")
	}

	if out, ok := p.Latest(uri, "a"); !ok || out == nil {
		t.Errorf("Latest for current text should hit, got ok=%v out=%v", ok, out)
	}
	if _, ok := p.Latest(uri, "edited"); ok {
		t.Error("Latest for stale text should miss")
	}

	p.Cancel(uri)
	if _, ok := p.Latest(uri, "a"); ok {
		t.Error("Latest after Cancel should miss (cache dropped)")
	}
}
