package schema

import (
	"strings"
	"testing"
)

func TestBuildK8sURL_DefaultPointsAtHomeOperations(t *testing.T) {
	got := BuildK8sURL("", "apps", "v1", "Deployment")
	want := "https://k8s-schemas.home-operations.com/apps/deployment_v1.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildK8sURL_DefaultCoreResource(t *testing.T) {
	got := BuildK8sURL("", "", "v1", "Pod")
	want := "https://k8s-schemas.home-operations.com/core/pod_v1.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildK8sURL_TemplatePlaceholders(t *testing.T) {
	tmpl := "https://schemas.example.com/{group}{group:+/}{kind@L}_{version@L}.json"
	got := BuildK8sURL(tmpl, "helm.toolkit.fluxcd.io", "v2", "HelmRelease")
	want := "https://schemas.example.com/helm.toolkit.fluxcd.io/helmrelease_v2.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildK8sURL_YannhLayoutViaTemplate(t *testing.T) {
	tmpl := "https://yannh.example/{kind@L}-{groupFirst}{groupFirst:+-}{version@L}.json"
	if got := BuildK8sURL(tmpl, "", "v1", "Pod"); got != "https://yannh.example/pod-v1.json" {
		t.Errorf("core: got %q", got)
	}
	if got := BuildK8sURL(tmpl, "apps", "v1", "Deployment"); got != "https://yannh.example/deployment-apps-v1.json" {
		t.Errorf("grouped: got %q", got)
	}
}

func TestBuildK8sURL_EmptyWhenMissingFields(t *testing.T) {
	if got := BuildK8sURL("", "apps", "", "Deployment"); got != "" {
		t.Errorf("missing version should yield empty, got %q", got)
	}
	if got := BuildK8sURL("", "apps", "v1", ""); got != "" {
		t.Errorf("missing kind should yield empty, got %q", got)
	}
}

func TestBuildK8sURL_AllPlaceholdersResolve(t *testing.T) {
	tmpl := "{group}|{groupSeg}|{groupFirst}|{groupFirstSeg}|{kind}|{kindLower}|{version}|{versionLower}"
	got := BuildK8sURL(tmpl, "apps.k8s.io", "v1beta1", "Deployment")
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
			if got := BuildK8sURL(tc.tmpl, tc.group, "v1", "Resource"); got != tc.want {
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
			if got := BuildK8sURL(tc.tmpl, tc.group, "v1", "Resource"); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildK8sURL_ExprSimpleSubstitution(t *testing.T) {
	if got := BuildK8sURL("{group}", "", "v1", "Namespace"); got != "" {
		t.Errorf("core: got %q", got)
	}
	if got := BuildK8sURL("{group}", "apps", "v1", "Resource"); got != "apps" {
		t.Errorf("got %q, want apps", got)
	}
	if got := BuildK8sURL("{kind}", "apps", "v1", "Deployment"); got != "Deployment" {
		t.Errorf("got %q, want Deployment", got)
	}
}

func TestBuildK8sURL_ExprComposeGroupSeg(t *testing.T) {
	tmpl := "{group:-core}/{kind}_{version}.json"

	if got := BuildK8sURL(tmpl, "", "v1", "Namespace"); got != "core/Namespace_v1.json" {
		t.Errorf("core: got %q", got)
	}
	if got := BuildK8sURL(tmpl, "apps", "v1", "Deployment"); got != "apps/Deployment_v1.json" {
		t.Errorf("grouped: got %q", got)
	}
	if got := BuildK8sURL(tmpl, "rbac.authorization.k8s.io", "v1", "ClusterRole"); got != "rbac.authorization.k8s.io/ClusterRole_v1.json" {
		t.Errorf("dotted group: got %q", got)
	}
}

func TestBuildK8sURL_ExprPreservesUnknownPlaceholders(t *testing.T) {
	tmpl := "{unknown}/{group}"
	if got := BuildK8sURL(tmpl, "apps", "v1", "Deployment"); got != "{unknown}/apps" {
		t.Errorf("got %q", got)
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
			if got := BuildK8sURL(tc.tmpl, tc.group, tc.version, tc.kind); got != tc.want {
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
			if got := BuildK8sURL(tc.tmpl, tc.group, tc.version, tc.kind); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildK8sURL_ExprUndefinedOperator(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{
			name: "known var with unknown operator returns var value",
			tmpl: "{group@x}",
			want: "apps",
		},
		{
			name: "unknown var with unknown operator preserved",
			tmpl: "{unknown@x}",
			want: "{unknown@x}",
		},
		{
			name: "empty operator returns var value",
			tmpl: "{kind@}",
			want: "Deployment",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BuildK8sURL(tc.tmpl, "apps", "v1", "Deployment"); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
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
			if got := BuildK8sURL(tc.tmpl, tc.group, tc.version, tc.kind); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildK8sURL_LegacyVarsMatchOperators(t *testing.T) {
	legacy := BuildK8sURL("{kindLower}_{versionLower}", "apps", "V1Beta1", "Deployment")
	operators := BuildK8sURL("{kind@L}_{version@L}", "apps", "V1Beta1", "Deployment")
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
			if got := BuildK8sURL(tc.tmpl, tc.group, "v1", "Resource"); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildK8sURL_LegacySegVarsMatchComposition(t *testing.T) {
	for _, group := range []string{"", "apps", "rbac.authorization.k8s.io"} {
		if got, want := BuildK8sURL("{groupSeg}", group, "v1", "Resource"), BuildK8sURL("{group}{group:+/}", group, "v1", "Resource"); got != want {
			t.Errorf("group %q: {groupSeg} %q != {group}{group:+/} %q", group, got, want)
		}
		if got, want := BuildK8sURL("{groupFirstSeg}", group, "v1", "Resource"), BuildK8sURL("{groupFirst}{groupFirst:+-}", group, "v1", "Resource"); got != want {
			t.Errorf("group %q: {groupFirstSeg} %q != {groupFirst}{groupFirst:+-} %q", group, got, want)
		}
	}
}
