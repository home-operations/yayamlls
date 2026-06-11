package flate_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/home-operations/yayamlls/internal/render"
	"github.com/home-operations/yayamlls/internal/render/flate"
)

// Run with -race: Render (pipeline goroutine) and Configure (LSP request
// goroutine) touch the same config state and must not race.
func TestRenderer_ConfigureDuringRenderNoRace(t *testing.T) {
	r := flate.New()
	doc := &render.SourceDocument{
		URI:      "file:///tmp/x.yaml",
		Path:     "/tmp/x.yaml",
		Kind:     "HelmRelease",
		APIGroup: "helm.toolkit.fluxcd.io/v2",
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = r.Render(context.Background(), doc)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				_ = r.Configure(json.RawMessage(fmt.Sprintf(`{"path":"/repo-%d"}`, i%2)))
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// writeFixture writes a minimal Flux GitOps tree:
//
//   - kubernetes/flux/cluster.yaml — Kustomizations apps-a and apps-b
//   - kubernetes/apps-{a,b}/        — one ConfigMap each
//
// Bootstrap publishes a synthetic GitRepository for the local tree, so the
// sourceRef resolves offline. Returns the kubernetes/ path to build from.
func writeFixture(t *testing.T) string {
	t.Helper()
	// Keep flate's on-disk caches out of the real user cache dir.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	k8s := filepath.Join(root, "kubernetes")
	for _, name := range []string{"apps-a", "apps-b"} {
		writeFile(t, filepath.Join(k8s, "flux", name+".yaml"), `---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: `+name+`
  namespace: flux-system
spec:
  interval: 10m
  path: ./`+name+`
  sourceRef: {kind: GitRepository, name: flux-system, namespace: flux-system}
`)
		writeFile(t, filepath.Join(k8s, name, "kustomization.yaml"),
			"resources:\n- cm.yaml\n")
		writeFile(t, filepath.Join(k8s, name, "cm.yaml"), `---
apiVersion: v1
kind: ConfigMap
metadata: {name: cm-`+name+`, namespace: default}
data:
  greeting: hi
`)
	}
	return k8s
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ksDoc(name string) *render.SourceDocument {
	return &render.SourceDocument{
		Kind:     "Kustomization",
		APIGroup: "kustomize.toolkit.fluxcd.io/v1",
		Name:     name,
	}
}

func TestFlate_RenderKustomization_ScopesByName(t *testing.T) {
	k8s := writeFixture(t)
	r := flate.New()
	if err := r.Configure(json.RawMessage(`{"path":` + jsonQuote(k8s) + `}`)); err != nil {
		t.Fatalf("configure: %v", err)
	}
	out, err := r.Render(context.Background(), ksDoc("apps-a"))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(out.Manifests) != 1 {
		t.Fatalf("expected 1 manifest, got %d (raw: %s)", len(out.Manifests), out.Raw)
	}
	m := out.Manifests[0]
	if m.GVK.Kind != "ConfigMap" || m.Name != "cm-apps-a" {
		t.Errorf("manifest = %s/%s, want ConfigMap/cm-apps-a", m.GVK.Kind, m.Name)
	}
	if strings.Contains(string(out.Raw), "cm-apps-b") {
		t.Errorf("output not scoped to apps-a: %s", out.Raw)
	}
}

func TestFlate_RelativePathAnchoredAtWorkspaceRoot(t *testing.T) {
	k8s := writeFixture(t)
	r := flate.New()
	if err := r.Configure(json.RawMessage(`{"path":"kubernetes"}`)); err != nil {
		t.Fatalf("configure: %v", err)
	}
	r.SetWorkspaceRoot(filepath.Dir(k8s))
	out, err := r.Render(context.Background(), ksDoc("apps-b"))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(out.Manifests) != 1 || out.Manifests[0].Name != "cm-apps-b" {
		t.Fatalf("expected cm-apps-b, got %+v", out.Manifests)
	}
}

func TestFlate_SkipsWhenNameUnknown(t *testing.T) {
	r := flate.New()
	r.SetWorkspaceRoot("/nonexistent") // must not be touched when skipping
	out, err := r.Render(context.Background(), ksDoc(""))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(out.Manifests) != 0 || len(out.Raw) != 0 {
		t.Errorf("expected empty output when name is unknown, got %+v", out)
	}
}

func TestFlate_UnmatchedNameErrors(t *testing.T) {
	k8s := writeFixture(t)
	r := flate.New()
	if err := r.Configure(json.RawMessage(`{"path":` + jsonQuote(k8s) + `}`)); err != nil {
		t.Fatalf("configure: %v", err)
	}
	_, err := r.Render(context.Background(), ksDoc("no-such-ks"))
	if err == nil || !strings.Contains(err.Error(), `no Kustomization named "no-such-ks"`) {
		t.Fatalf("expected unmatched-name error, got %v", err)
	}
}

// One tree render serves multiple documents; editing the tree invalidates it.
func TestFlate_TreeCacheInvalidatedOnFileChange(t *testing.T) {
	k8s := writeFixture(t)
	r := flate.New()
	if err := r.Configure(json.RawMessage(`{"path":` + jsonQuote(k8s) + `}`)); err != nil {
		t.Fatalf("configure: %v", err)
	}
	out, err := r.Render(context.Background(), ksDoc("apps-a"))
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	if !strings.Contains(string(out.Raw), "greeting: hi") {
		t.Fatalf("unexpected first render: %s", out.Raw)
	}
	// Second doc against the same tree comes from the cached render.
	if _, err := r.Render(context.Background(), ksDoc("apps-b")); err != nil {
		t.Fatalf("second doc render: %v", err)
	}
	writeFile(t, filepath.Join(k8s, "apps-a", "cm.yaml"), `---
apiVersion: v1
kind: ConfigMap
metadata: {name: cm-apps-a, namespace: default}
data:
  greeting: hello again
`)
	out, err = r.Render(context.Background(), ksDoc("apps-a"))
	if err != nil {
		t.Fatalf("render after edit: %v", err)
	}
	if !strings.Contains(string(out.Raw), "hello again") {
		t.Errorf("edit not picked up, raw: %s", out.Raw)
	}
}

// With file watching active the tree cache is keyed by invalidation
// generation: the per-render fingerprint walk is skipped (an unannounced
// disk edit stays invisible), out-of-root invalidations keep the cache, and
// an in-root invalidation triggers a rebuild.
func TestFlate_EventModeInvalidatesByGenerationOnly(t *testing.T) {
	k8s := writeFixture(t)
	r := flate.New()
	if err := r.Configure(json.RawMessage(`{"path":` + jsonQuote(k8s) + `}`)); err != nil {
		t.Fatalf("configure: %v", err)
	}
	r.SetFileWatchActive(true)

	out, err := r.Render(context.Background(), ksDoc("apps-a"))
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	if !strings.Contains(string(out.Raw), "greeting: hi") {
		t.Fatalf("unexpected first render: %s", out.Raw)
	}

	writeFile(t, filepath.Join(k8s, "apps-a", "cm.yaml"), `---
apiVersion: v1
kind: ConfigMap
metadata: {name: cm-apps-a, namespace: default}
data:
  greeting: hello again
`)

	// No invalidation event yet: the edit must not be picked up (proves the
	// fingerprint walk is skipped in event mode).
	out, err = r.Render(context.Background(), ksDoc("apps-a"))
	if err != nil {
		t.Fatalf("render after silent edit: %v", err)
	}
	if strings.Contains(string(out.Raw), "hello again") {
		t.Fatal("event mode picked up an unannounced edit; fingerprinting not skipped")
	}

	// An event outside the build root must not invalidate either.
	r.InvalidateTree(filepath.Join(t.TempDir(), "elsewhere.yaml"))
	out, err = r.Render(context.Background(), ksDoc("apps-a"))
	if err != nil {
		t.Fatalf("render after out-of-root invalidation: %v", err)
	}
	if strings.Contains(string(out.Raw), "hello again") {
		t.Fatal("out-of-root invalidation rebuilt the tree")
	}

	r.InvalidateTree(filepath.Join(k8s, "apps-a", "cm.yaml"))
	out, err = r.Render(context.Background(), ksDoc("apps-a"))
	if err != nil {
		t.Fatalf("render after invalidation: %v", err)
	}
	if !strings.Contains(string(out.Raw), "hello again") {
		t.Errorf("in-root invalidation not picked up, raw: %s", out.Raw)
	}
}

// A timed-out build is negative-cached: other documents fail fast instead of
// re-spending the timeout, until the next tree change clears it.
func TestFlate_TimeoutNegativeCachedUntilInvalidated(t *testing.T) {
	k8s := writeFixture(t)
	r := flate.New()
	if err := r.Configure(json.RawMessage(`{"path":` + jsonQuote(k8s) + `}`)); err != nil {
		t.Fatalf("configure: %v", err)
	}
	r.SetFileWatchActive(true)

	ctx, cancel := context.WithTimeout(context.Background(), -time.Second)
	defer cancel()
	if _, err := r.Render(ctx, ksDoc("apps-a")); err == nil {
		t.Fatal("expected error from timed-out render")
	}

	// Without the negative cache this render would succeed.
	if _, err := r.Render(context.Background(), ksDoc("apps-a")); err == nil {
		t.Fatal("expected cached timeout error, got success")
	}

	r.InvalidateTree("")
	out, err := r.Render(context.Background(), ksDoc("apps-a"))
	if err != nil {
		t.Fatalf("render after invalidation: %v", err)
	}
	if len(out.Manifests) != 1 || out.Manifests[0].Name != "cm-apps-a" {
		t.Fatalf("expected cm-apps-a after invalidation, got %+v", out.Manifests)
	}
}

// A cancelled render must not be cached: the next caller gets a fresh one.
func TestFlate_CancelledRenderNotCached(t *testing.T) {
	k8s := writeFixture(t)
	r := flate.New()
	if err := r.Configure(json.RawMessage(`{"path":` + jsonQuote(k8s) + `}`)); err != nil {
		t.Fatalf("configure: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Render(ctx, ksDoc("apps-a")); err == nil {
		t.Fatal("expected error from cancelled render")
	}
	out, err := r.Render(context.Background(), ksDoc("apps-a"))
	if err != nil {
		t.Fatalf("render after cancellation: %v", err)
	}
	if len(out.Manifests) != 1 || out.Manifests[0].Name != "cm-apps-a" {
		t.Fatalf("expected cm-apps-a after retry, got %+v", out.Manifests)
	}
}

func TestFlate_MatchesKindsExactly(t *testing.T) {
	r := flate.New()
	cases := []struct {
		name string
		doc  *render.SourceDocument
		want bool
	}{
		{"helm release v2", &render.SourceDocument{Kind: "HelmRelease", APIGroup: "helm.toolkit.fluxcd.io/v2"}, true},
		{"kustomization", &render.SourceDocument{Kind: "Kustomization", APIGroup: "kustomize.toolkit.fluxcd.io/v1"}, true},
		{"vanilla pod", &render.SourceDocument{Kind: "Pod", APIGroup: "v1"}, false},
		{"unrelated CR", &render.SourceDocument{Kind: "Other", APIGroup: "example.com/v1"}, false},
	}
	for _, c := range cases {
		if got := r.Matches(c.doc); got != c.want {
			t.Errorf("%s: Matches = %v, want %v", c.name, got, c.want)
		}
	}
}

// jsonQuote JSON-quotes a path for embedding in a config literal.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
