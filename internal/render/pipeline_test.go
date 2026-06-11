package render_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/goccy/go-yaml/parser"
	"github.com/home-operations/yayamlls/internal/render"
)

type fakeRenderer struct {
	name    string
	matches bool
	out     []byte
	err     error

	mu    sync.Mutex
	calls int
}

func (f *fakeRenderer) Name() string                        { return f.name }
func (f *fakeRenderer) Matches(*render.SourceDocument) bool { return f.matches }
func (f *fakeRenderer) Render(_ context.Context, doc *render.SourceDocument) (*render.RenderedOutput, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	pf, err := parser.ParseBytes(f.out, 0)
	if err != nil {
		return nil, err
	}
	manifests := make([]render.RenderedManifest, 0, len(pf.Docs))
	for _, d := range pf.Docs {
		manifests = append(manifests, render.RenderedManifest{
			AST:  d,
			GVK:  render.GVK{Group: "", Version: "v1", Kind: "Pod"},
			Name: "test",
		})
	}
	return &render.RenderedOutput{
		Provider:  f.name,
		Raw:       f.out,
		Manifests: manifests,
	}, nil
}

type recordingSink struct {
	mu   sync.Mutex
	got  []*render.RenderedOutput
	errs []error
	uris []string
	done chan struct{}
}

func newRecordingSink() *recordingSink { return &recordingSink{done: make(chan struct{}, 16)} }

func (s *recordingSink) Notify(uri string, out *render.RenderedOutput, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uris = append(s.uris, uri)
	s.got = append(s.got, out)
	s.errs = append(s.errs, err)
	s.done <- struct{}{}
}

func TestPipeline_DebouncedRender(t *testing.T) {
	reg := render.NewRegistry()
	fr := &fakeRenderer{
		name:    "fake",
		matches: true,
		out:     []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: x\n"),
	}
	reg.Register(fr)
	sink := newRecordingSink()
	p := render.NewPipeline(reg, sink)
	p.SetDebounce(20 * time.Millisecond)

	doc := &render.SourceDocument{URI: "file:///tmp/x.yaml", Path: "/tmp/x.yaml", Text: "a"}
	p.Schedule(doc)

	select {
	case <-sink.done:
	case <-time.After(2 * time.Second):
		t.Fatalf("pipeline never delivered: calls=%d", fr.calls)
	}

	if fr.calls != 1 {
		t.Errorf("expected 1 render call, got %d", fr.calls)
	}
	if len(sink.got) != 1 || sink.got[0] == nil || len(sink.got[0].Manifests) != 1 {
		t.Errorf("unexpected sink output: %+v", sink.got)
	}
}

// blockingRenderer echoes the scheduled text and sleeps, so a render can
// still be in flight when the next Schedule supersedes it.
type blockingRenderer struct {
	delay time.Duration
}

func (b *blockingRenderer) Name() string                        { return "blocking" }
func (b *blockingRenderer) Matches(*render.SourceDocument) bool { return true }
func (b *blockingRenderer) Render(_ context.Context, doc *render.SourceDocument) (*render.RenderedOutput, error) {
	time.Sleep(b.delay)
	return &render.RenderedOutput{Provider: "blocking", Raw: []byte(doc.Text)}, nil
}

func TestPipeline_SupersededRenderIsDropped(t *testing.T) {
	reg := render.NewRegistry()
	reg.Register(&blockingRenderer{delay: 100 * time.Millisecond})
	sink := newRecordingSink()
	p := render.NewPipeline(reg, sink)
	p.SetDebounce(time.Millisecond)

	uri := "file:///tmp/x.yaml"
	p.Schedule(&render.SourceDocument{URI: uri, Text: "old"})
	time.Sleep(30 * time.Millisecond) // let the first render start
	p.Schedule(&render.SourceDocument{URI: uri, Text: "new"})

	select {
	case <-sink.done:
	case <-time.After(2 * time.Second):
		t.Fatal("pipeline never delivered")
	}
	// Give the superseded render time to (wrongly) deliver if it would.
	time.Sleep(150 * time.Millisecond)

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.got) != 1 {
		t.Fatalf("expected exactly one delivery, got %d", len(sink.got))
	}
	if string(sink.got[0].Raw) != "new" {
		t.Errorf("delivered stale content %q, want %q", sink.got[0].Raw, "new")
	}
}

// ctxRenderer blocks until its render context is cancelled, then returns the
// context error — standing in for a source fetch that stalls on a slow remote.
type ctxRenderer struct{}

func (ctxRenderer) Name() string                        { return "ctx" }
func (ctxRenderer) Matches(*render.SourceDocument) bool { return true }
func (ctxRenderer) Render(ctx context.Context, _ *render.SourceDocument) (*render.RenderedOutput, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestPipeline_SetTimeoutBoundsRender(t *testing.T) {
	reg := render.NewRegistry()
	reg.Register(ctxRenderer{})
	sink := newRecordingSink()
	p := render.NewPipeline(reg, sink)
	p.SetDebounce(time.Millisecond)
	p.SetTimeout(20 * time.Millisecond)

	p.Schedule(&render.SourceDocument{URI: "file:///tmp/x.yaml", Text: "a"})

	select {
	case <-sink.done:
	case <-time.After(2 * time.Second):
		t.Fatal("render never hit its deadline")
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.errs) != 1 || !errors.Is(sink.errs[0], context.DeadlineExceeded) {
		t.Errorf("want one context.DeadlineExceeded, got %v", sink.errs)
	}
}

func TestPipeline_NoMatchingProvider(t *testing.T) {
	reg := render.NewRegistry()
	reg.Register(&fakeRenderer{name: "fake", matches: false})
	sink := newRecordingSink()
	p := render.NewPipeline(reg, sink)
	p.SetDebounce(10 * time.Millisecond)

	p.Schedule(&render.SourceDocument{URI: "file:///tmp/y.yaml", Text: "z"})

	select {
	case <-sink.done:
		t.Fatalf("sink should not have been notified")
	case <-time.After(150 * time.Millisecond):
	}
}
