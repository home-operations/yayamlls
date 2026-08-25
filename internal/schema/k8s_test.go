package schema

import (
	"strings"
	"testing"
)

func buildK8sURL(t *testing.T, template, group, version, kind string) string {
	t.Helper()
	got, err := BuildK8sURL(template, group, version, kind)
	if err != nil {
		t.Fatalf("BuildK8sURL(%q) returned error: %v", template, err)
	}
	return got
}

func TestBuildK8sURL_DefaultPointsAtHomeOperations(t *testing.T) {
	got := buildK8sURL(t, "", "apps", "v1", "Deployment")
	want := "https://k8s-schemas.home-operations.com/apps/deployment_v1.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildK8sURL_DefaultCoreResource(t *testing.T) {
	got := buildK8sURL(t, "", "", "v1", "Pod")
	want := "https://k8s-schemas.home-operations.com/core/pod_v1.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildK8sURL_TemplatePlaceholders(t *testing.T) {
	tmpl := "https://schemas.example.com/{group}{group:+/}{kind@L}_{version@L}.json"
	got := buildK8sURL(t, tmpl, "helm.toolkit.fluxcd.io", "v2", "HelmRelease")
	want := "https://schemas.example.com/helm.toolkit.fluxcd.io/helmrelease_v2.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildK8sURL_YannhLayoutViaTemplate(t *testing.T) {
	tmpl := "https://yannh.example/{kind@L}-{groupFirst}{groupFirst:+-}{version@L}.json"
	if got := buildK8sURL(t, tmpl, "", "v1", "Pod"); got != "https://yannh.example/pod-v1.json" {
		t.Errorf("core: got %q", got)
	}
	if got := buildK8sURL(t, tmpl, "apps", "v1", "Deployment"); got != "https://yannh.example/deployment-apps-v1.json" {
		t.Errorf("grouped: got %q", got)
	}
}

func TestBuildK8sURL_EmptyWhenMissingFields(t *testing.T) {
	if got := buildK8sURL(t, "", "apps", "", "Deployment"); got != "" {
		t.Errorf("missing version should yield empty, got %q", got)
	}
	if got := buildK8sURL(t, "", "apps", "v1", ""); got != "" {
		t.Errorf("missing kind should yield empty, got %q", got)
	}
}

func TestBuildK8sURL_AllPlaceholdersResolve(t *testing.T) {
	tmpl := "{group}|{groupSeg}|{groupFirst}|{groupFirstSeg}|{kind}|{kindLower}|{version}|{versionLower}"
	got := buildK8sURL(t, tmpl, "apps.k8s.io", "v1beta1", "Deployment")
	if strings.Contains(got, "{") || strings.Contains(got, "}") {
		t.Errorf("unsubstituted placeholder leaked: %q", got)
	}
}

func TestBuildK8sURL_ExprDefaultOnEmpty(t *testing.T) {
	tests := []struct {
		name  string
		group string
		want  string
		tmpl  string
	}{
		{name: "empty gets 'core' fallback", group: "", tmpl: "{group:-core}", want: "core"},
		{name: "'apps' keeps value", group: "apps", tmpl: "{group:-core}", want: "apps"},
		{name: "empty gets 'test' fallback", group: "", tmpl: "{group:-test}", want: "test"},
		{name: "'batch' keeps value", group: "batch", tmpl: "{group:-default}", want: "batch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildK8sURL(t, tc.tmpl, tc.group, "v1", "Resource"); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildK8sURL_ExprAltOnNonEmpty(t *testing.T) {
	tests := []struct {
		name  string
		group string
		want  string
		tmpl  string
	}{
		{name: "empty gets empty", group: "", tmpl: "{group:+/}", want: ""},
		{name: "'apps' gets '/'", group: "apps", tmpl: "{group:+/}", want: "/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildK8sURL(t, tc.tmpl, tc.group, "v1", "Resource"); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildK8sURL_ExprSimpleSubstitution(t *testing.T) {
	if got := buildK8sURL(t, "{group}", "", "v1", "Namespace"); got != "" {
		t.Errorf("core: got %q", got)
	}
	if got := buildK8sURL(t, "{group}", "apps", "v1", "Resource"); got != "apps" {
		t.Errorf("got %q, want apps", got)
	}
	if got := buildK8sURL(t, "{kind}", "apps", "v1", "Deployment"); got != "Deployment" {
		t.Errorf("got %q, want Deployment", got)
	}
}

func TestBuildK8sURL_ExprComposeGroupSeg(t *testing.T) {
	tmpl := "{group:-core}/{kind}_{version}.json"

	if got := buildK8sURL(t, tmpl, "", "v1", "Namespace"); got != "core/Namespace_v1.json" {
		t.Errorf("core: got %q", got)
	}
	if got := buildK8sURL(t, tmpl, "apps", "v1", "Deployment"); got != "apps/Deployment_v1.json" {
		t.Errorf("grouped: got %q", got)
	}
	if got := buildK8sURL(t, tmpl, "rbac.authorization.k8s.io", "v1", "ClusterRole"); got != "rbac.authorization.k8s.io/ClusterRole_v1.json" {
		t.Errorf("dotted group: got %q", got)
	}
}

func TestBuildK8sURL_UnknownPlaceholderErrors(t *testing.T) {
	tmpls := []string{
		"{unknown}",
		"{unknown}/{group}",
		"{unknown@x}",
	}
	for _, tmpl := range tmpls {
		t.Run(tmpl, func(t *testing.T) {
			if got, err := BuildK8sURL(tmpl, "apps", "v1", "Deployment"); err == nil {
				t.Errorf("expected error for %q, got %q", tmpl, got)
			}
		})
	}
}

func TestBuildK8sURL_ExprUppercaseOperator(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		group   string
		version string
		kind    string
		want    string
	}{
		{
			name:    "kind@U uppercases kind",
			tmpl:    "{kind@U}",
			group:   "apps",
			version: "v1",
			kind:    "Deployment",
			want:    "DEPLOYMENT",
		},
		{
			name:    "version@U uppercases version",
			tmpl:    "{version@U}",
			group:   "apps",
			version: "v1beta1",
			kind:    "Deployment",
			want:    "V1BETA1",
		},
		{
			name:    "group@U uppercases group",
			tmpl:    "{group@U}",
			group:   "rbac.authorization.k8s.io",
			version: "v1",
			kind:    "Deployment",
			want:    "RBAC.AUTHORIZATION.K8S.IO",
		},
		{
			name:    "empty value stays empty",
			tmpl:    "{group@U}",
			version: "v1",
			kind:    "Deployment",
			want:    "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildK8sURL(t, tc.tmpl, tc.group, tc.version, tc.kind); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildK8sURL_ExprLowercaseOperator(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		group   string
		version string
		kind    string
		want    string
	}{
		{
			name:    "kind@L lowercases kind",
			tmpl:    "{kind@L}",
			group:   "apps",
			version: "v1",
			kind:    "Deployment",
			want:    "deployment",
		},
		{
			name:    "version@L lowercases version",
			tmpl:    "{version@L}",
			group:   "apps",
			version: "V1Beta1",
			kind:    "Deployment",
			want:    "v1beta1",
		},
		{
			name:    "group@L lowercases group",
			tmpl:    "{group@L}",
			group:   "RBAC.Authorization.K8s.IO",
			version: "v1",
			kind:    "Deployment",
			want:    "rbac.authorization.k8s.io",
		},
		{
			name:    "empty value stays empty",
			tmpl:    "{group@L}",
			version: "v1",
			kind:    "Deployment",
			want:    "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildK8sURL(t, tc.tmpl, tc.group, tc.version, tc.kind); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildK8sURL_ExprUndefinedOperator(t *testing.T) {
	tmpls := []string{
		"{group@xyz}",
		"{kind@}",
	}
	for _, tmpl := range tmpls {
		t.Run(tmpl, func(t *testing.T) {
			if got, err := BuildK8sURL(tmpl, "apps", "v1", "Deployment"); err == nil {
				t.Errorf("expected error for %q, got %q", tmpl, got)
			}
		})
	}
}

func TestBuildK8sURL_NestedExpressions(t *testing.T) {
	tests := []struct {
		name  string
		tmpl  string
		group string
		want  string
	}{
		{
			name:  "empty group uppercased fallback",
			tmpl:  "{{group:-core}@U}",
			group: "",
			want:  "CORE",
		},
		{
			name:  "grouped group uppercased",
			tmpl:  "{{group:-core}@U}",
			group: "apps",
			want:  "APPS",
		},
		{
			name:  "empty group lowercased fallback",
			tmpl:  "{{group:-Core}@L}",
			group: "",
			want:  "core",
		},
		{
			name:  "nested operand lowercased then defaulted",
			tmpl:  "{{group@L}:-core}",
			group: "APPS",
			want:  "apps",
		},
		{
			name:  "nested operand lowercased empty uses fallback",
			tmpl:  "{{group@L}:-Core}",
			group: "",
			want:  "Core",
		},
		{
			name:  "nested expression in fallback word",
			tmpl:  "{group:-{kind@U}}",
			group: "",
			want:  "DEPLOYMENT",
		},
		{
			name:  "nested word only when value empty",
			tmpl:  "{group:-{kind@L}}",
			group: "apps",
			want:  "apps",
		},
		{
			name:  "nested in url",
			tmpl:  "https://schemas.example.com/{{group:-core}@U}/{kind@L}.json",
			group: "",
			want:  "https://schemas.example.com/CORE/deployment.json",
		},
		{
			name:  "depth 3 lowercased fallback",
			tmpl:  "{{{group:-core}@U}@L}",
			group: "",
			want:  "core",
		},
		{
			name:  "depth 3 lowercased value",
			tmpl:  "{{{group:-core}@U}@L}",
			group: "apps",
			want:  "apps",
		},
		{
			name:  "depth 4 uppercased fallback",
			tmpl:  "{{{{group:-core}@U}@L}@U}",
			group: "",
			want:  "CORE",
		},
		{
			name:  "depth 4 uppercased value",
			tmpl:  "{{{{group:-core}@U}@L}@U}",
			group: "apps",
			want:  "APPS",
		},
		{
			name:  "operator outside braces is literal",
			tmpl:  "{group:-core}@U",
			group: "",
			want:  "core@U",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildK8sURL(t, tc.tmpl, tc.group, "v1", "Deployment"); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildK8sURL_UnbalancedBraces(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
	}{
		{
			name: "unterminated expression",
			tmpl: "{group:-core",
		},
		{
			name: "unterminated expression in url",
			tmpl: "https://schemas.example.com/{group:-core",
		},
		{
			name: "extra opening brace",
			tmpl: "{{group:-core}",
		},
		{
			name: "extra closing brace",
			tmpl: "{group:-core}}",
		},
		{
			name: "lone opening brace",
			tmpl: "foo{bar",
		},
		{
			name: "lone closing brace",
			tmpl: "foo}bar",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := BuildK8sURL(tc.tmpl, "", "v1", "Resource"); err == nil {
				t.Errorf("expected error for %q, got %q", tc.tmpl, got)
			}
		})
	}
}

func TestBuildK8sURL_LegacyLowercaseVars(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		group   string
		version string
		kind    string
		want    string
	}{
		{
			name:    "kindLower",
			tmpl:    "{kindLower}",
			group:   "apps",
			version: "v1",
			kind:    "Deployment",
			want:    "deployment",
		},
		{
			name:    "versionLower",
			tmpl:    "{versionLower}",
			group:   "apps",
			version: "V1Beta1",
			kind:    "Deployment",
			want:    "v1beta1",
		},
		{
			name:    "core default form",
			tmpl:    "{group:-core}/{kindLower}_{versionLower}.json",
			version: "V1",
			kind:    "Namespace",
			want:    "core/namespace_v1.json",
		},
		{
			name:    "grouped default form",
			tmpl:    "{group:-core}/{kindLower}_{versionLower}.json",
			group:   "apps",
			version: "v1",
			kind:    "Deployment",
			want:    "apps/deployment_v1.json",
		},
		{
			name:    "dotted group default form",
			tmpl:    "{group:-core}/{kindLower}_{versionLower}.json",
			group:   "rbac.authorization.k8s.io",
			version: "v1",
			kind:    "ClusterRole",
			want:    "rbac.authorization.k8s.io/clusterrole_v1.json",
		},
		{
			name:    "legacy var in expression",
			tmpl:    "{kindLower:-pod}",
			version: "v1",
			kind:    "Deployment",
			want:    "deployment",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildK8sURL(t, tc.tmpl, tc.group, tc.version, tc.kind); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildK8sURL_LegacyVarsMatchOperators(t *testing.T) {
	legacy := buildK8sURL(t, "{kindLower}_{versionLower}", "apps", "V1Beta1", "Deployment")
	operators := buildK8sURL(t, "{kind@L}_{version@L}", "apps", "V1Beta1", "Deployment")
	if legacy != operators {
		t.Errorf("legacy %q != operator form %q", legacy, operators)
	}
}

func TestBuildK8sURL_LegacySegVars(t *testing.T) {
	tests := []struct {
		name  string
		tmpl  string
		group string
		want  string
	}{
		{
			name: "groupSeg core",
			tmpl: "{groupSeg}",
			want: "",
		},
		{
			name:  "groupSeg grouped",
			tmpl:  "{groupSeg}",
			group: "apps",
			want:  "apps/",
		},
		{
			name:  "groupSeg dotted",
			tmpl:  "{groupSeg}",
			group: "rbac.authorization.k8s.io",
			want:  "rbac.authorization.k8s.io/",
		},
		{
			name: "groupFirstSeg core",
			tmpl: "{groupFirstSeg}",
			want: "",
		},
		{
			name:  "groupFirstSeg grouped",
			tmpl:  "{groupFirstSeg}",
			group: "apps",
			want:  "apps-",
		},
		{
			name:  "groupFirstSeg dotted",
			tmpl:  "{groupFirstSeg}",
			group: "rbac.authorization.k8s.io",
			want:  "rbac-",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildK8sURL(t, tc.tmpl, tc.group, "v1", "Resource"); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildK8sURL_LegacySegVarsMatchComposition(t *testing.T) {
	for _, group := range []string{"", "apps", "rbac.authorization.k8s.io"} {
		if got, want := buildK8sURL(t, "{groupSeg}", group, "v1", "Resource"), buildK8sURL(t, "{group}{group:+/}", group, "v1", "Resource"); got != want {
			t.Errorf("group %q: {groupSeg} %q != {group}{group:+/} %q", group, got, want)
		}
		if got, want := buildK8sURL(t, "{groupFirstSeg}", group, "v1", "Resource"), buildK8sURL(t, "{groupFirst}{groupFirst:+-}", group, "v1", "Resource"); got != want {
			t.Errorf("group %q: {groupFirstSeg} %q != {groupFirst}{groupFirst:+-} %q", group, got, want)
		}
	}
}
