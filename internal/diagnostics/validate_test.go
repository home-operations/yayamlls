package diagnostics_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/home-operations/yayamlls/internal/config"
	"github.com/home-operations/yayamlls/internal/diagnostics"
	"github.com/home-operations/yayamlls/internal/schema"
	"github.com/home-operations/yayamlls/internal/yamlast"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// hostnamePatternSchema is a minimal schema with the Gateway API hostname
// pattern constraint, matching what triggers the Flux substitution false positive.
const hostnamePatternSchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"type": "object",
	"properties": {
		"hostname": {
			"type": "string",
			"pattern": "^(\\*\\.)?[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$"
		}
	}
}`

func compileInlineSchema(t *testing.T, body string) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource("mem://test.json", doc); err != nil {
		t.Fatalf("add resource: %v", err)
	}
	sch, err := c.Compile("mem://test.json")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return sch
}

func TestValidateDoc_FluxSubstitution_PatternSuppressedWhenEnabled(t *testing.T) {
	sch := compileInlineSchema(t, hostnamePatternSchema)
	body := "hostname: ${EDGE_HOST}\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{FluxSubstitutions: true})
	for _, d := range diags {
		if strings.Contains(d.Message, "pattern") {
			t.Errorf("expected no pattern diagnostic with FluxSubstitutions=true, got: %s", d.Message)
		}
	}
}

func TestValidateDoc_FluxSubstitution_PatternFiredWhenDisabled(t *testing.T) {
	sch := compileInlineSchema(t, hostnamePatternSchema)
	body := "hostname: ${EDGE_HOST}\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{FluxSubstitutions: false})
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "pattern") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pattern diagnostic with FluxSubstitutions=false, got none; diags: %+v", diags)
	}
}

func TestValidateDoc_NonFluxPatternMismatch_AlwaysFires(t *testing.T) {
	sch := compileInlineSchema(t, hostnamePatternSchema)
	// A genuinely invalid hostname (uppercase letters) with no substitution token.
	body := "hostname: INVALID_HOST\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{FluxSubstitutions: true})
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "pattern") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pattern diagnostic for genuinely invalid value, got none; diags: %+v", diags)
	}
}

func TestValidateDoc_FluxSubstitution_PartialAndMultipleTokensSuppressed(t *testing.T) {
	sch := compileInlineSchema(t, hostnamePatternSchema)
	cases := []string{
		"${VAR}.example.com",    // suffix after token
		"prefix-${VAR}",         // prefix before token
		"${A}-${B}.example.com", // multiple tokens
	}
	for _, hostname := range cases {
		body := "hostname: " + hostname + "\n"
		doc := yamlast.Parse([]byte(body)).Docs()[0]
		diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{FluxSubstitutions: true})
		for _, d := range diags {
			if strings.Contains(d.Message, "pattern") {
				t.Errorf("hostname %q: expected no pattern diagnostic with FluxSubstitutions=true, got: %s", hostname, d.Message)
			}
		}
	}
}

// nullableAnyOfSchema mirrors how Kubernetes models an optional field:
// resources is anyOf:[<object>, {type:null}]. A populated, otherwise-invalid
// object fails both branches, so the {type:null} branch emits a spurious
// "want null" leaf alongside the real error.
const nullableAnyOfSchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"type": "object",
	"properties": {
		"resources": {
			"anyOf": [
				{
					"type": "object",
					"properties": {"cpu": {"type": "string"}},
					"additionalProperties": false
				},
				{"type": "null"}
			]
		}
	}
}`

func TestValidateDoc_NullableAnyOf_SuppressesWantNullNoise(t *testing.T) {
	sch := compileInlineSchema(t, nullableAnyOfSchema)
	body := "resources:\n  cpu: 1\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{})
	for _, d := range diags {
		if strings.Contains(d.Message, "want null") {
			t.Errorf("expected no \"want null\" noise from nullable anyOf, got: %s", d.Message)
		}
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "/resources/cpu") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the real /resources/cpu type error to survive; got: %+v", diags)
	}
}

// quantitySchema mirrors how Kubernetes schemas $ref the apimachinery Quantity
// type (a bare string) for resource fields, alongside an ordinary string field.
const quantitySchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"type": "object",
	"properties": {
		"cpu": {"$ref": "#/definitions/io.k8s.apimachinery.pkg.api.resource.Quantity"},
		"name": {"type": "string"}
	},
	"definitions": {
		"io.k8s.apimachinery.pkg.api.resource.Quantity": {"type": "string"}
	}
}`

func TestValidateDoc_Quantity_SuppressesNumericStringNoise(t *testing.T) {
	sch := compileInlineSchema(t, quantitySchema)
	body := "cpu: 1\nname: 5\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{})
	for _, d := range diags {
		if strings.Contains(d.Message, "/cpu") {
			t.Errorf("expected numeric Quantity to be accepted, got: %s", d.Message)
		}
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "/name") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the real /name type error to survive; got: %+v", diags)
	}
}

// nodeLabelsSchema mirrors a Talos/Kubernetes map[string]string field, where a
// strategic-merge `{$patch: delete}` value trips a "got object, want string".
const nodeLabelsSchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"type": "object",
	"properties": {
		"nodeLabels": {"type": "object", "additionalProperties": {"type": "string"}}
	}
}`

func TestValidateDoc_StrategicMergePatch_DirectiveSuppressed(t *testing.T) {
	sch := compileInlineSchema(t, nodeLabelsSchema)
	body := "nodeLabels:\n" +
		"  node.kubernetes.io/exclude-from-external-load-balancers:\n" +
		"    $patch: delete\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{})
	if len(diags) != 0 {
		t.Errorf("expected $patch directive to be accepted, got: %+v", diags)
	}
}

func TestValidateDoc_StrategicMergePatch_RealTypeErrorSurvives(t *testing.T) {
	sch := compileInlineSchema(t, nodeLabelsSchema)
	// A genuine object value (not a merge directive) must still fail.
	body := "nodeLabels:\n  some-label:\n    nested: oops\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{})
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "/nodeLabels/some-label") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the real type error on a non-directive object to survive; got: %+v", diags)
	}
}

// closedListSchema models a closed object with a list of closed objects, the
// shape that turns mixed-in strategic-merge directive keys into
// "additionalProperties" errors.
const closedListSchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"type": "object",
	"additionalProperties": false,
	"properties": {
		"containers": {
			"type": "array",
			"items": {
				"type": "object",
				"additionalProperties": false,
				"properties": {"name": {"type": "string"}}
			}
		}
	}
}`

func TestValidateDoc_StrategicMergePatch_ListElementDeleteSuppressed(t *testing.T) {
	sch := compileInlineSchema(t, closedListSchema)
	// `$patch: delete` beside the element's identity field.
	body := "containers:\n  - name: log-tailer\n    $patch: delete\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{})
	if len(diags) != 0 {
		t.Errorf("expected $patch list-element directive to be accepted, got: %+v", diags)
	}
}

func TestValidateDoc_StrategicMergePatch_PrefixedDirectivesSuppressed(t *testing.T) {
	sch := compileInlineSchema(t, closedListSchema)
	// Sibling ordering/deletion directives carrying a target-field suffix.
	body := "$setElementOrder/containers:\n  - name: a\n" +
		"$deleteFromPrimitiveList/containers:\n  - b\n" +
		"containers:\n  - name: a\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{})
	if len(diags) != 0 {
		t.Errorf("expected prefixed merge directives to be accepted, got: %+v", diags)
	}
}

func TestValidateDoc_StrategicMergePatch_DirectiveOnlyListElementDropped(t *testing.T) {
	sch := compileInlineSchema(t, closedListSchema)
	// A standalone `{$patch: delete}` element is the whole marker: post-merge it
	// is gone, so the projection validates even against a required-name item.
	body := "containers:\n  - name: keep\n  - $patch: delete\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{})
	if len(diags) != 0 {
		t.Errorf("expected directive-only list element to be dropped, got: %+v", diags)
	}
}

func TestValidateDoc_StrategicMergePatch_RealUnknownKeySurvives(t *testing.T) {
	sch := compileInlineSchema(t, closedListSchema)
	// A genuine typo mixed with a directive must still be reported.
	body := "containers:\n  - name: a\n    $patch: delete\n    typo: x\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{})
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "typo") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the real unknown-key error to survive alongside a directive; got: %+v", diags)
	}
}

func TestValidate_TypeMismatchProducesDiagnostic(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repo := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	docPath := filepath.Join(repo, "test", "fixtures", "person-invalid.yaml")
	body := `# yaml-language-server: $schema=./schemas/person.json
name: Alice
age: "thirty"
`
	parsed := yamlast.Parse([]byte(body))
	if parsed.Err != nil {
		t.Fatalf("parse: %v", parsed.Err)
	}

	store := schema.NewStore()
	sch, err := store.Get("./schemas/person.json", docPath)
	if err != nil {
		t.Fatalf("schema compile: %v", err)
	}

	diags := diagnostics.Validate(parsed, sch, diagnostics.Options{})
	if len(diags) == 0 {
		t.Fatalf("expected at least one diagnostic, got none")
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "/age") {
			found = true
			if d.Range.Start.Line == 0 {
				t.Errorf("expected /age diagnostic past line 0, got %+v", d.Range)
			}
		}
	}
	if !found {
		t.Errorf("no diagnostic mentioned /age; got: %+v", diags)
	}
}

func TestValidate_ParseErrorAnchoredAtPosition(t *testing.T) {
	// `- b` under a mapping key is a syntax error goccy reports at [2:3].
	body := "a:\n- b\n  c: d\n"
	parsed := yamlast.Parse([]byte(body))
	if parsed.Err == nil {
		t.Fatalf("expected a parse error")
	}
	diags := diagnostics.Validate(parsed, nil, diagnostics.Options{})
	if len(diags) != 1 {
		t.Fatalf("expected one parse diagnostic, got %d: %+v", len(diags), diags)
	}
	d := diags[0]
	if d.Range.Start.Line != 1 || d.Range.Start.Character != 2 {
		t.Errorf("parse error anchored at %+v, want line 1 char 2", d.Range.Start)
	}
	if strings.Contains(d.Message, "[2:3]") || strings.Contains(d.Message, "\n") {
		t.Errorf("message should be the clean text without the position prefix or snippet, got %q", d.Message)
	}
}

func TestValidate_ValidDocProducesNoDiagnostic(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repo := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	docPath := filepath.Join(repo, "test", "fixtures", "person-valid.yaml")
	body := `# yaml-language-server: $schema=./schemas/person.json
name: Alice
age: 30
`
	parsed := yamlast.Parse([]byte(body))
	store := schema.NewStore()
	sch, err := store.Get("./schemas/person.json", docPath)
	if err != nil {
		t.Fatalf("schema compile: %v", err)
	}
	diags := diagnostics.Validate(parsed, sch, diagnostics.Options{})
	if len(diags) != 0 {
		t.Errorf("expected zero diagnostics, got: %+v", diags)
	}
}

// replicasIntSchema requires an integer; a !Ref-tagged value decodes to a
// bare string, which would otherwise fail the type check.
const replicasIntSchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"type": "object",
	"properties": {"replicas": {"type": "integer"}}
}`

func TestValidateDoc_CustomTag_SuppressedWhenDeclared(t *testing.T) {
	sch := compileInlineSchema(t, replicasIntSchema)
	body := "replicas: !Ref desiredCount\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{CustomTags: []string{"!Ref"}})
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics for declared custom tag, got: %+v", diags)
	}
}

func TestValidateDoc_CustomTag_FiresWhenNotDeclared(t *testing.T) {
	sch := compileInlineSchema(t, replicasIntSchema)
	body := "replicas: !Ref desiredCount\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{})
	if len(diags) == 0 {
		t.Error("expected a type diagnostic when the tag is not declared")
	}
}

func TestValidateDoc_CustomTag_KindHintMatchesLeadingTag(t *testing.T) {
	sch := compileInlineSchema(t, replicasIntSchema)
	body := "replicas: !Ref desiredCount\n"
	doc := yamlast.Parse([]byte(body)).Docs()[0]

	diags := diagnostics.ValidateDoc(doc, sch, body, diagnostics.Options{CustomTags: []string{"!Ref scalar"}})
	if len(diags) != 0 {
		t.Errorf("expected kind-hinted tag to match, got: %+v", diags)
	}
}

// fluxKustomizationSchema mirrors the parts of the Flux Kustomization schema
// that a child Kustomization patched by its parent trips over.
const fluxKustomizationSchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"type": "object",
	"required": ["apiVersion", "kind", "spec"],
	"properties": {
		"apiVersion": {"type": "string"},
		"kind": {"type": "string"},
		"spec": {
			"type": "object",
			"required": ["interval", "prune", "sourceRef"],
			"properties": {
				"interval": {"type": "string"},
				"prune": {"type": "boolean"},
				"sourceRef": {
					"type": "object",
					"required": ["kind", "name"],
					"properties": {"kind": {"type": "string"}, "name": {"type": "string"}}
				}
			}
		}
	}
}`

func TestValidateDoc_IgnoreRequired(t *testing.T) {
	const (
		fluxAPI   = "apiVersion: kustomize.toolkit.fluxcd.io/v1\nkind: Kustomization\n"
		fluxGroup = "kustomize.toolkit.fluxcd.io"
	)
	fluxRule := func(paths ...string) []config.IgnoreRequiredRule {
		return []config.IgnoreRequiredRule{{
			Match: config.KindMatch{Kind: "Kustomization", Group: fluxGroup},
			Paths: paths,
		}}
	}
	tests := []struct {
		name  string
		body  string
		rules []config.IgnoreRequiredRule
		want  []string // substrings, one per expected diagnostic, in order
	}{
		{
			name: "no rules reports every missing property",
			body: fluxAPI + "spec:\n  interval: 1h\n",
			want: []string{"missing properties 'prune', 'sourceRef' (at /spec)"},
		},
		{
			name:  "all missing properties exempt",
			body:  fluxAPI + "spec:\n  interval: 1h\n",
			rules: fluxRule("/spec/prune", "/spec/sourceRef"),
		},
		{
			name:  "partial exemption reports the rest",
			body:  fluxAPI + "spec:\n  interval: 1h\n",
			rules: fluxRule("/spec/prune"),
			want:  []string{"missing property 'sourceRef' (at /spec)"},
		},
		{
			name:  "non-exempt required property still reports",
			body:  fluxAPI + "spec: {}\n",
			rules: fluxRule("/spec/prune", "/spec/sourceRef"),
			want:  []string{"missing property 'interval' (at /spec)"},
		},
		{
			name:  "any version of the group matches",
			body:  "apiVersion: kustomize.toolkit.fluxcd.io/v1beta2\nkind: Kustomization\nspec:\n  interval: 1h\n",
			rules: fluxRule("/spec/prune", "/spec/sourceRef"),
		},
		{
			name: "kind mismatch",
			body: fluxAPI + "spec:\n  interval: 1h\n",
			rules: []config.IgnoreRequiredRule{{
				Match: config.KindMatch{Kind: "HelmRelease", Group: fluxGroup},
				Paths: []string{"/spec/prune", "/spec/sourceRef"},
			}},
			want: []string{"missing properties 'prune', 'sourceRef'"},
		},
		{
			name:  "group sharing a prefix does not match",
			body:  "apiVersion: kustomize.toolkit.fluxcd.iox/v1\nkind: Kustomization\nspec:\n  interval: 1h\n",
			rules: fluxRule("/spec/prune", "/spec/sourceRef"),
			want:  []string{"missing properties 'prune', 'sourceRef'"},
		},
		{
			name: "rule group that is a prefix does not match",
			body: fluxAPI + "spec:\n  interval: 1h\n",
			rules: []config.IgnoreRequiredRule{{
				Match: config.KindMatch{Kind: "Kustomization", Group: "kustomize.toolkit"},
				Paths: []string{"/spec/prune", "/spec/sourceRef"},
			}},
			want: []string{"missing properties 'prune', 'sourceRef'"},
		},
		{
			name: "omitted group matches only the core group",
			body: fluxAPI + "spec:\n  interval: 1h\n",
			rules: []config.IgnoreRequiredRule{{
				Match: config.KindMatch{Kind: "Kustomization"},
				Paths: []string{"/spec/prune", "/spec/sourceRef"},
			}},
			want: []string{"missing properties 'prune', 'sourceRef'"},
		},
		{
			name: "omitted group matches a core-group document",
			body: "apiVersion: v1\nkind: Kustomization\nspec:\n  interval: 1h\n",
			rules: []config.IgnoreRequiredRule{{
				Match: config.KindMatch{Kind: "Kustomization"},
				Paths: []string{"/spec/prune", "/spec/sourceRef"},
			}},
		},
		{
			name:  "document without apiVersion is never matched",
			body:  "kind: Kustomization\nspec:\n  interval: 1h\n",
			rules: fluxRule("/spec/prune", "/spec/sourceRef", "/apiVersion"),
			want:  []string{"missing property 'apiVersion'", "missing properties 'prune', 'sourceRef'"},
		},
		{
			name:  "path at another level does not exempt",
			body:  fluxAPI + "spec:\n  interval: 1h\n",
			rules: fluxRule("/prune", "/spec/sourceRef/prune"),
			want:  []string{"missing properties 'prune', 'sourceRef'"},
		},
		{
			name:  "type error on an exempt property still reports",
			body:  fluxAPI + "spec:\n  interval: 1h\n  prune: \"yes\"\n",
			rules: fluxRule("/spec/prune", "/spec/sourceRef"),
			want:  []string{"want boolean (at /spec/prune)"},
		},
		{
			name:  "required inside an exempt property still reports",
			body:  fluxAPI + "spec:\n  interval: 1h\n  prune: true\n  sourceRef:\n    kind: GitRepository\n",
			rules: fluxRule("/spec/prune", "/spec/sourceRef"),
			want:  []string{"missing property 'name' (at /spec/sourceRef)"},
		},
	}
	sch := compileInlineSchema(t, fluxKustomizationSchema)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := yamlast.Parse([]byte(tt.body)).Docs()[0]
			diags := diagnostics.ValidateDoc(doc, sch, tt.body, diagnostics.Options{IgnoreRequired: tt.rules})
			if len(diags) != len(tt.want) {
				t.Fatalf("got %d diagnostics, want %d: %+v", len(diags), len(tt.want), diags)
			}
			for i, want := range tt.want {
				if !strings.Contains(diags[i].Message, want) {
					t.Errorf("diagnostic %d = %q, want it to contain %q", i, diags[i].Message, want)
				}
			}
		})
	}
}

func TestLeafMessage(t *testing.T) {
	required := func(loc []string, missing ...string) *jsonschema.ValidationError {
		return &jsonschema.ValidationError{InstanceLocation: loc, ErrorKind: &kind.Required{Missing: missing}}
	}
	tests := []struct {
		name   string
		err    *jsonschema.ValidationError
		exempt map[string]bool
		want   string
		wantOK bool
	}{
		{name: "nil exempt passes through", err: required([]string{"spec"}, "a", "b"),
			want: "missing properties 'a', 'b'", wantOK: true},
		{name: "partial", err: required([]string{"spec"}, "a", "b"), exempt: map[string]bool{"/spec/a": true},
			want: "missing property 'b'", wantOK: true},
		{name: "all exempt", err: required([]string{"spec"}, "a", "b"),
			exempt: map[string]bool{"/spec/a": true, "/spec/b": true}},
		{name: "document root", err: required(nil, "spec"), exempt: map[string]bool{"/spec": true}},
		{name: "segments are pointer-escaped", err: required([]string{"metadata", "labels"}, "app.kubernetes.io/name"),
			exempt: map[string]bool{"/metadata/labels/app.kubernetes.io~1name": true}},
		{name: "unescaped path does not match", err: required([]string{"metadata", "labels"}, "app.kubernetes.io/name"),
			exempt: map[string]bool{"/metadata/labels/app.kubernetes.io/name": true},
			want:   "missing property 'app.kubernetes.io/name'", wantOK: true},
		{name: "other keywords are untouched",
			err:    &jsonschema.ValidationError{InstanceLocation: []string{"spec", "prune"}, ErrorKind: &kind.Type{Got: "string", Want: []string{"boolean"}}},
			exempt: map[string]bool{"/spec/prune": true},
			want:   "got string, want boolean", wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := diagnostics.LeafMessage(tt.err, tt.exempt)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("LeafMessage = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
