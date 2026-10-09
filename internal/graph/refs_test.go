package graph

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func boolPtr(b bool) *bool { return &b }

func hasRef(refs []Reference, kind RefKind, name string, mode RefMode) bool {
	for _, r := range refs {
		if r.Kind == kind && r.Name == name && r.Mode == mode {
			return true
		}
	}
	return false
}

func findRef(t *testing.T, refs []Reference, kind RefKind, name string) Reference {
	t.Helper()
	for _, r := range refs {
		if r.Kind == kind && r.Name == name {
			return r
		}
	}
	t.Fatalf("no reference to %s/%s in %+v", kind, name, refs)
	return Reference{}
}

func findRefMode(t *testing.T, refs []Reference, kind RefKind, name string, mode RefMode) Reference {
	t.Helper()
	for _, r := range refs {
		if r.Kind == kind && r.Name == name && r.Mode == mode {
			return r
		}
	}
	t.Fatalf("no reference to %s/%s mode=%s in %+v", kind, name, mode, refs)
	return Reference{}
}

func TestExtractReferences(t *testing.T) {
	spec := corev1.PodSpec{
		InitContainers: []corev1.Container{{
			Name: "init",
			Env: []corev1.EnvVar{{
				Name: "SETUP",
				ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "init-cm"},
					Key:                  "setup",
				}},
			}},
		}},
		Containers: []corev1.Container{{
			Name: "app",
			Env: []corev1.EnvVar{
				{Name: "A", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"},
					Key:                  "a",
				}}},
				{Name: "B", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "db-creds"},
					Key:                  "password",
				}}},
				// duplicate reference must be deduplicated
				{Name: "A2", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"},
					Key:                  "a",
				}}},
				// a plain value is not a reference
				{Name: "PLAIN", Value: "x"},
			},
			EnvFrom: []corev1.EnvFromSource{
				{ConfigMapRef: &corev1.ConfigMapEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "env-cm"},
					Optional:             boolPtr(true),
				}},
				{SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "env-secret"},
				}},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "cfg", MountPath: "/etc/cfg"},
				{Name: "sub", MountPath: "/etc/sub", SubPath: "key"},
			},
		}},
		Volumes: []corev1.Volume{
			{Name: "cfg", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"},
				Items: []corev1.KeyToPath{
					{Key: "a", Path: "a.txt"},
					{Key: "b", Path: "b.txt"},
				},
			}}},
			{Name: "sub", VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: "tls-cert"},
			}},
			{Name: "proj", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{ConfigMap: &corev1.ConfigMapProjection{
						LocalObjectReference: corev1.LocalObjectReference{Name: "proj-cm"},
					}},
					{Secret: &corev1.SecretProjection{
						LocalObjectReference: corev1.LocalObjectReference{Name: "proj-secret"},
					}},
				},
			}}},
		},
		ImagePullSecrets: []corev1.LocalObjectReference{{Name: "registry-creds"}},
	}

	refs := ExtractReferences(spec)

	expected := []struct {
		kind RefKind
		name string
		mode RefMode
	}{
		{RefConfigMap, "init-cm", ModeEnv},
		{RefConfigMap, "app-config", ModeEnv},
		{RefSecret, "db-creds", ModeEnv},
		{RefConfigMap, "env-cm", ModeEnvFrom},
		{RefSecret, "env-secret", ModeEnvFrom},
		{RefConfigMap, "app-config", ModeVolume},
		{RefSecret, "tls-cert", ModeVolumeSubPath},
		{RefConfigMap, "proj-cm", ModeProjected},
		{RefSecret, "proj-secret", ModeProjected},
		{RefSecret, "registry-creds", ModeImagePullSecret},
	}
	for _, e := range expected {
		if !hasRef(refs, e.kind, e.name, e.mode) {
			t.Errorf("missing reference %s/%s mode=%s", e.kind, e.name, e.mode)
		}
	}

	// The env reference records its key and container.
	envRef := findRefMode(t, refs, RefConfigMap, "app-config", ModeEnv)
	if len(envRef.Keys) != 1 || envRef.Keys[0] != "a" {
		t.Errorf("env ref keys = %v, want [a]", envRef.Keys)
	}
	if envRef.Container != "app" {
		t.Errorf("env ref container = %q, want app", envRef.Container)
	}

	// The volume reference is not tied to a container and lists both keys.
	var volumeRefs int
	for _, r := range refs {
		if r.Kind == RefConfigMap && r.Name == "app-config" && r.Mode == ModeVolume {
			volumeRefs++
			if r.Container != "" {
				t.Errorf("volume ref should not be tied to a container, got %q", r.Container)
			}
			if len(r.Keys) != 2 {
				t.Errorf("volume ref keys = %v, want 2", r.Keys)
			}
		}
	}
	if volumeRefs != 1 {
		t.Errorf("app-config volume refs = %d, want 1", volumeRefs)
	}

	// The init container is not the one holding the app ref.
	if ref := findRef(t, refs, RefConfigMap, "init-cm"); ref.Container != "init" {
		t.Errorf("init-cm container = %q, want init", ref.Container)
	}

	// Optional is carried through.
	if r := findRef(t, refs, RefConfigMap, "env-cm"); !r.Optional {
		t.Error("env-cm should be optional")
	}

	// No duplicate edges.
	seen := map[string]int{}
	for _, r := range refs {
		seen[r.identity()]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("duplicate reference %q x%d", id, n)
		}
	}
}

func TestExtractReferencesNoRefs(t *testing.T) {
	if refs := ExtractReferences(corev1.PodSpec{}); len(refs) != 0 {
		t.Errorf("empty spec produced %d refs", len(refs))
	}

	spec := corev1.PodSpec{Containers: []corev1.Container{{
		Name: "app",
		Env:  []corev1.EnvVar{{Name: "PLAIN", Value: "x"}},
	}}}
	if refs := ExtractReferences(spec); len(refs) != 0 {
		t.Errorf("plain env produced %d refs, want 0", len(refs))
	}
}

func TestRefModeBehavior(t *testing.T) {
	cases := []struct {
		mode       RefMode
		restart    bool
		liveReload bool
	}{
		{ModeEnv, true, false},
		{ModeEnvFrom, true, false},
		{ModeVolume, false, true},
		{ModeVolumeSubPath, true, false},
		{ModeProjected, false, true},
		{ModeImagePullSecret, false, false},
	}
	for _, c := range cases {
		if got := c.mode.RestartRequired(); got != c.restart {
			t.Errorf("%s.RestartRequired() = %v, want %v", c.mode, got, c.restart)
		}
		if got := c.mode.LiveReload(); got != c.liveReload {
			t.Errorf("%s.LiveReload() = %v, want %v", c.mode, got, c.liveReload)
		}
	}
}

func TestPodSpecFrom(t *testing.T) {
	cmRef := []any{map[string]any{"configMapRef": map[string]any{"name": "c"}}}
	keyRef := []any{map[string]any{
		"name":      "X",
		"valueFrom": map[string]any{"configMapKeyRef": map[string]any{"name": "c", "key": "a"}},
	}}

	cases := []struct {
		name     string
		obj      map[string]any
		wantRefs int
	}{
		{
			name: "deployment",
			obj: map[string]any{
				"apiVersion": "apps/v1", "kind": "Deployment",
				"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
					"containers": []any{map[string]any{"name": "app", "envFrom": cmRef}},
				}}},
			},
			wantRefs: 1,
		},
		{
			name: "cronjob",
			obj: map[string]any{
				"apiVersion": "batch/v1", "kind": "CronJob",
				"spec": map[string]any{"jobTemplate": map[string]any{"spec": map[string]any{
					"template": map[string]any{"spec": map[string]any{
						"containers": []any{map[string]any{"name": "job", "env": keyRef}},
					}},
				}}},
			},
			wantRefs: 1,
		},
		{
			name: "bare pod",
			obj: map[string]any{
				"apiVersion": "v1", "kind": "Pod",
				"spec": map[string]any{
					"containers": []any{map[string]any{"name": "app", "envFrom": cmRef}},
				},
			},
			wantRefs: 1,
		},
		{
			name: "configmap has no pod spec",
			obj: map[string]any{
				"apiVersion": "v1", "kind": "ConfigMap",
				"data": map[string]any{"a": "b"},
			},
			wantRefs: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := &unstructured.Unstructured{Object: c.obj}
			spec, ok := PodSpecFrom(u)
			if c.wantRefs == 0 {
				if ok {
					t.Errorf("expected no pod spec, got %+v", spec)
				}
				return
			}
			if !ok {
				t.Fatal("expected a pod spec")
			}
			if got := len(ExtractReferences(spec)); got != c.wantRefs {
				t.Errorf("refs = %d, want %d", got, c.wantRefs)
			}
		})
	}
}

func TestOwnedByScannedKind(t *testing.T) {
	owned := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"ownerReferences": []any{
				map[string]any{"kind": "ReplicaSet", "name": "web-abc", "apiVersion": "apps/v1"},
			},
		},
	}}
	if !ownedByScannedKind(owned) {
		t.Error("a pod owned by a ReplicaSet should be skipped")
	}

	custom := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"ownerReferences": []any{
				map[string]any{"kind": "CustomThing", "name": "x", "apiVersion": "example.com/v1"},
			},
		},
	}}
	if ownedByScannedKind(custom) {
		t.Error("an object owned by an unknown kind should not be skipped")
	}

	if ownedByScannedKind(&unstructured.Unstructured{Object: map[string]any{}}) {
		t.Error("an object with no owners should not be skipped")
	}
}
