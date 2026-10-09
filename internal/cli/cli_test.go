package cli

import (
	"reflect"
	"testing"

	"github.com/mehrabix/kubectl-ripple/internal/graph"
)

func TestParseKind(t *testing.T) {
	for _, in := range []string{"configmap", "configmaps", "ConfigMap", "cm"} {
		k, err := parseKind(in)
		if err != nil || k != graph.RefConfigMap {
			t.Errorf("parseKind(%q) = %v, %v; want ConfigMap", in, k, err)
		}
	}
	for _, in := range []string{"secret", "secrets", "Secret"} {
		k, err := parseKind(in)
		if err != nil || k != graph.RefSecret {
			t.Errorf("parseKind(%q) = %v, %v; want Secret", in, k, err)
		}
	}
	if _, err := parseKind("deployment"); err == nil {
		t.Error("parseKind(deployment) should fail")
	}
}

func TestObjectArg(t *testing.T) {
	cases := []struct {
		arg      string
		kindFlag string
		wantKind graph.RefKind
		wantName string
		wantErr  bool
	}{
		{"configmap/app-config", "", graph.RefConfigMap, "app-config", false},
		{"secret/db-creds", "", graph.RefSecret, "db-creds", false},
		{"cm/short", "", graph.RefConfigMap, "short", false},
		{"DB", "secret", graph.RefSecret, "DB", false},
		{"app-config", "configmap", graph.RefConfigMap, "app-config", false},
		{"app-config", "", "", "", true}, // bare name without --kind
		{"thing/name", "", "", "", true}, // unknown kind
		{"", "configmap", "", "", true},  // empty name
		{"configmap/", "", "", "", true}, // empty name
	}
	for _, c := range cases {
		kind, name, err := objectArg(c.arg, c.kindFlag)
		if c.wantErr {
			if err == nil {
				t.Errorf("objectArg(%q, %q) should have failed", c.arg, c.kindFlag)
			}
			continue
		}
		if err != nil {
			t.Errorf("objectArg(%q, %q) error: %v", c.arg, c.kindFlag, err)
			continue
		}
		if kind != c.wantKind || name != c.wantName {
			t.Errorf("objectArg(%q, %q) = %v/%q, want %v/%q",
				c.arg, c.kindFlag, kind, name, c.wantKind, c.wantName)
		}
	}
}

func TestIsSystemNamespace(t *testing.T) {
	for _, ns := range []string{"kube-system", "kube-public", "kube-node-lease", "kube-foo"} {
		if !isSystemNamespace(ns) {
			t.Errorf("%q should be a system namespace", ns)
		}
	}
	for _, ns := range []string{"default", "prod", "ripple-demo", "kubesystem"} {
		if isSystemNamespace(ns) {
			t.Errorf("%q should not be a system namespace", ns)
		}
	}
}

func TestParsedKindsFromFlag(t *testing.T) {
	got, err := parseKinds([]string{"configmap,secret"})
	if err != nil {
		t.Fatalf("parseKinds: %v", err)
	}
	want := []graph.RefKind{graph.RefConfigMap, graph.RefSecret}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseKinds = %v, want %v", got, want)
	}

	if _, err := parseKinds([]string{""}); err == nil {
		t.Error("empty --types should fail")
	}
	if _, err := parseKinds([]string{"pod"}); err == nil {
		t.Error("unsupported type should fail")
	}
}

func TestEffectLabels(t *testing.T) {
	cases := []struct {
		mode graph.RefMode
		want string
	}{
		{graph.ModeEnv, "restart required"},
		{graph.ModeEnvFrom, "restart required"},
		{graph.ModeVolumeSubPath, "restart required"},
		{graph.ModeVolume, "live reload"},
		{graph.ModeProjected, "live reload"},
		{graph.ModeImagePullSecret, "new pods only"},
	}
	for _, c := range cases {
		if got := effect(c.mode); got != c.want {
			t.Errorf("effect(%s) = %q, want %q", c.mode, got, c.want)
		}
	}
}

func TestKeysAndTouchedLabels(t *testing.T) {
	all := graph.Reference{Kind: graph.RefConfigMap, Name: "x", Mode: graph.ModeEnvFrom}
	if got := keysLabel(all); got != "*" {
		t.Errorf("keysLabel(all) = %q, want *", got)
	}

	some := graph.Reference{Kind: graph.RefConfigMap, Name: "x", Mode: graph.ModeEnv, Keys: []string{"a", "b"}}
	if got := keysLabel(some); got != "a,b" {
		t.Errorf("keysLabel = %q, want a,b", got)
	}

	change := graph.Change{Changed: []string{"b"}, Added: []string{"c"}}
	if got := touchedLabel(change, some); got != "b" {
		t.Errorf("touchedLabel = %q, want b", got)
	}
	if got := touchedLabel(change, all); got != "*" {
		t.Errorf("touchedLabel(all) = %q, want *", got)
	}

	unrelated := graph.Reference{Kind: graph.RefConfigMap, Name: "x", Mode: graph.ModeEnv, Keys: []string{"z"}}
	if got := touchedLabel(change, unrelated); got != "-" {
		t.Errorf("touchedLabel(unrelated) = %q, want -", got)
	}
}

func TestPlural(t *testing.T) {
	if got := plural(1, "workload"); got != "1 workload" {
		t.Errorf("plural(1) = %q", got)
	}
	if got := plural(2, "workload"); got != "2 workloads" {
		t.Errorf("plural(2) = %q", got)
	}
}

func TestVisibleWorkloads(t *testing.T) {
	in := []graph.Workload{
		{Kind: "Deployment", Namespace: "prod", Name: "web"},
		{Kind: "Deployment", Namespace: "kube-system", Name: "coredns"},
	}
	if got := visibleWorkloads(in, false); len(got) != 1 || got[0].Name != "web" {
		t.Errorf("visibleWorkloads(false) = %+v", got)
	}
	if got := visibleWorkloads(in, true); len(got) != 2 {
		t.Errorf("visibleWorkloads(true) = %+v", got)
	}
}
