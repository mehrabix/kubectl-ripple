package graph

import (
	"reflect"
	"testing"
)

func TestDiffKeys(t *testing.T) {
	old := map[string]string{"a": "1", "b": "2", "gone": "x"}
	new := map[string]string{"a": "1", "b": "CHANGED", "fresh": "y"}

	got := DiffKeys(old, new)
	want := Change{Added: []string{"fresh"}, Removed: []string{"gone"}, Changed: []string{"b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiffKeys = %+v, want %+v", got, want)
	}
}

func TestDiffKeysNoChange(t *testing.T) {
	same := map[string]string{"a": "1"}
	if got := DiffKeys(same, map[string]string{"a": "1"}); !got.Empty() {
		t.Errorf("expected no change, got %+v", got)
	}
	if got := DiffKeys(nil, map[string]string{}); !got.Empty() {
		t.Errorf("expected no change, got %+v", got)
	}
}

func TestChangeAffects(t *testing.T) {
	change := Change{Changed: []string{"log-level"}}

	cases := []struct {
		name string
		ref  Reference
		want bool
	}{
		{"key ref to a changed key", Reference{Mode: ModeEnv, Keys: []string{"log-level"}}, true},
		{"key ref to an unrelated key", Reference{Mode: ModeEnv, Keys: []string{"other"}}, false},
		{"one of several keys matches", Reference{Mode: ModeEnv, Keys: []string{"other", "log-level"}}, true},
		{"whole-object ref", Reference{Mode: ModeEnvFrom}, true},
		{"volume ref reads every key", Reference{Mode: ModeVolume}, true},
	}
	for _, c := range cases {
		if got := change.Affects(c.ref); got != c.want {
			t.Errorf("%s: Affects = %v, want %v", c.name, got, c.want)
		}
	}

	// An empty change affects nothing, even a whole-object reference.
	if (Change{}).Affects(Reference{Mode: ModeEnvFrom}) {
		t.Error("an empty change should affect nothing")
	}
}

func sampleGraph() *Graph {
	return &Graph{Workloads: []Workload{
		{
			Kind: "Deployment", Namespace: "prod", Name: "web",
			Refs: []Reference{
				{Kind: RefConfigMap, Name: "app-config", Mode: ModeEnv, Keys: []string{"a"}},
				{Kind: RefSecret, Name: "db-creds", Mode: ModeEnvFrom},
			},
		},
		{
			Kind: "Deployment", Namespace: "prod", Name: "worker",
			Refs: []Reference{{Kind: RefConfigMap, Name: "app-config", Mode: ModeVolume}},
		},
		{
			Kind: "CronJob", Namespace: "prod", Name: "nightly",
			Refs: []Reference{{Kind: RefSecret, Name: "ghost-secret", Mode: ModeEnv}},
		},
		{
			Kind: "StatefulSet", Namespace: "dev", Name: "db",
			Refs: []Reference{{Kind: RefConfigMap, Name: "app-config", Mode: ModeEnvFrom}},
		},
	}}
}

func TestConsumers(t *testing.T) {
	g := sampleGraph()

	got := g.Consumers(RefConfigMap, "prod", "app-config")
	if len(got) != 2 {
		t.Fatalf("consumers = %d, want 2 (same namespace only)", len(got))
	}

	// web uses env (restart), worker uses a volume (live reload).
	byName := map[string]Consumer{}
	for _, c := range got {
		byName[c.Workload.Name] = c
	}
	if !byName["web"].RestartRequired() {
		t.Error("web should require a restart")
	}
	if byName["web"].LiveReload() {
		t.Error("web should not live-reload")
	}
	if byName["worker"].RestartRequired() {
		t.Error("worker should not require a restart")
	}
	if !byName["worker"].LiveReload() {
		t.Error("worker should live-reload")
	}

	if n := len(g.Consumers(RefConfigMap, "prod", "does-not-exist")); n != 0 {
		t.Errorf("unknown object returned %d consumers", n)
	}
	if n := len(g.Consumers(RefConfigMap, "other", "app-config")); n != 0 {
		t.Errorf("wrong namespace returned %d consumers", n)
	}
}

func TestOrphans(t *testing.T) {
	g := sampleGraph()

	objects := []ObjectRef{
		{Kind: RefConfigMap, Namespace: "prod", Name: "app-config"}, // referenced
		{Kind: RefConfigMap, Namespace: "prod", Name: "unused"},     // orphan
		{Kind: RefSecret, Namespace: "prod", Name: "db-creds"},      // referenced
		{Kind: RefSecret, Namespace: "dev", Name: "app-config"},     // same name, other namespace -> orphan
		{Kind: RefSecret, Namespace: "prod", Name: "ghost-secret"},  // referenced (dangling)
		{Kind: RefConfigMap, Namespace: "prod", Name: "aaa-unused"}, // orphan, ordering
	}
	got := g.Orphans(objects)
	want := []string{
		"ConfigMap/prod/aaa-unused",
		"ConfigMap/prod/unused",
		"Secret/dev/app-config",
	}
	var displays []string
	for _, o := range got {
		displays = append(displays, o.Display())
	}
	if !reflect.DeepEqual(displays, want) {
		t.Errorf("orphans = %v, want %v", displays, want)
	}
}

func TestDangling(t *testing.T) {
	g := sampleGraph()

	existing := map[string]bool{
		"ConfigMap/prod/app-config": true,
		"ConfigMap/dev/app-config":  true,
		"Secret/prod/db-creds":      true,
		// ghost-secret is missing
	}
	got := g.Dangling(existing)
	if len(got) != 1 {
		t.Fatalf("dangling = %d, want 1, got %+v", len(got), got)
	}
	if got[0].Ref.Name != "ghost-secret" {
		t.Errorf("dangling ref = %q, want ghost-secret", got[0].Ref.Name)
	}
	if got[0].Workload.Name != "nightly" {
		t.Errorf("dangling workload = %q, want nightly", got[0].Workload.Name)
	}

	if n := len(g.Dangling(g.ReferencedObjects())); n != 0 {
		t.Errorf("everything referenced should not be dangling, got %d", n)
	}
}

func TestGraphSortDeterministic(t *testing.T) {
	g := sampleGraph()
	g.Sort()
	want := []string{"db", "nightly", "web", "worker"}
	var got []string
	for _, w := range g.Workloads {
		got = append(got, w.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
