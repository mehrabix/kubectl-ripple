// Package graph builds a dependency graph between Kubernetes workloads and the
// ConfigMaps and Secrets they consume.
package graph

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// RefKind is the kind of object a workload can reference.
type RefKind string

const (
	RefConfigMap RefKind = "ConfigMap"
	RefSecret    RefKind = "Secret"
)

// RefMode describes how a referenced object is consumed by a workload. It is the
// part that decides whether a change actually needs a restart.
type RefMode string

const (
	// ModeEnv is an env[].valueFrom.{configMapKeyRef,secretKeyRef} reference.
	// The value is snapshotted when the container starts.
	ModeEnv RefMode = "env"
	// ModeEnvFrom is an envFrom[] reference: every key is snapshotted at start.
	ModeEnvFrom RefMode = "envFrom"
	// ModeVolume is a configMap/secret volume. The kubelet refreshes the
	// projected files in place, so a running container can pick the change up.
	ModeVolume RefMode = "volume"
	// ModeVolumeSubPath is a volume mounted with subPath. subPath mounts are
	// not refreshed, which is the classic "I updated the ConfigMap and nothing
	// happened" trap.
	ModeVolumeSubPath RefMode = "volume-subPath"
	// ModeProjected is a projected volume source.
	ModeProjected RefMode = "projected"
	// ModeImagePullSecret is spec.imagePullSecrets. It is evaluated when an
	// image is pulled, so it only affects pods created afterwards.
	ModeImagePullSecret RefMode = "imagePullSecret"
)

// RestartRequired reports whether a change to the referenced object only takes
// effect after the workload's pods are recreated.
func (m RefMode) RestartRequired() bool {
	switch m {
	case ModeEnv, ModeEnvFrom, ModeVolumeSubPath:
		return true
	default:
		return false
	}
}

// LiveReload reports whether a running container can observe a change without
// being restarted (the application still has to re-read the file).
func (m RefMode) LiveReload() bool {
	switch m {
	case ModeVolume, ModeProjected:
		return true
	default:
		return false
	}
}

// Reference is a single edge from a workload to a ConfigMap or Secret.
type Reference struct {
	Kind RefKind `json:"kind"`
	Name string  `json:"name"`
	Mode RefMode `json:"mode"`
	// Keys lists the individual keys read through a keyRef. An empty slice
	// means every key of the object is consumed.
	Keys      []string `json:"keys,omitempty"`
	Container string   `json:"container,omitempty"`
	Optional  bool     `json:"optional,omitempty"`
}

// AllKeys reports whether the reference consumes every key of the object.
func (r Reference) AllKeys() bool { return len(r.Keys) == 0 }

// identity is the deduplication key for a reference.
func (r Reference) identity() string {
	return strings.Join([]string{
		string(r.Kind), r.Name, string(r.Mode), r.Container,
		strings.Join(r.Keys, ","), fmt.Sprint(r.Optional),
	}, "\x00")
}

// ExtractReferences returns every ConfigMap and Secret consumed by a pod spec.
func ExtractReferences(spec corev1.PodSpec) []Reference {
	// Track which volumes are mounted with subPath: those are not refreshed
	// after the initial sync even though they are backed by a volume.
	subPath := map[string]bool{}
	for _, c := range allContainers(spec) {
		for _, m := range c.VolumeMounts {
			if m.SubPath != "" {
				subPath[m.Name] = true
			}
		}
	}

	var refs []Reference

	for _, c := range allContainers(spec) {
		refs = append(refs, containerReferences(c.Name, c.Env, c.EnvFrom)...)
	}

	for _, v := range spec.Volumes {
		refs = append(refs, volumeReferences(v, subPath[v.Name])...)
	}

	for _, s := range spec.ImagePullSecrets {
		refs = append(refs, Reference{Kind: RefSecret, Name: s.Name, Mode: ModeImagePullSecret})
	}

	return dedupe(refs)
}

// containerLike is the subset of fields shared by containers, init containers
// and ephemeral containers.
type containerLike struct {
	Name         string
	Env          []corev1.EnvVar
	EnvFrom      []corev1.EnvFromSource
	VolumeMounts []corev1.VolumeMount
}

func allContainers(spec corev1.PodSpec) []containerLike {
	out := make([]containerLike, 0, len(spec.InitContainers)+len(spec.Containers)+len(spec.EphemeralContainers))
	for _, c := range spec.InitContainers {
		out = append(out, containerLike{c.Name, c.Env, c.EnvFrom, c.VolumeMounts})
	}
	for _, c := range spec.Containers {
		out = append(out, containerLike{c.Name, c.Env, c.EnvFrom, c.VolumeMounts})
	}
	for _, c := range spec.EphemeralContainers {
		out = append(out, containerLike{c.Name, c.Env, c.EnvFrom, c.VolumeMounts})
	}
	return out
}

func containerReferences(name string, env []corev1.EnvVar, envFrom []corev1.EnvFromSource) []Reference {
	var refs []Reference

	for _, e := range env {
		if e.ValueFrom == nil {
			continue
		}
		if cm := e.ValueFrom.ConfigMapKeyRef; cm != nil {
			refs = append(refs, Reference{
				Kind: RefConfigMap, Name: cm.Name, Mode: ModeEnv,
				Keys: []string{cm.Key}, Container: name, Optional: optBool(cm.Optional),
			})
		}
		if s := e.ValueFrom.SecretKeyRef; s != nil {
			refs = append(refs, Reference{
				Kind: RefSecret, Name: s.Name, Mode: ModeEnv,
				Keys: []string{s.Key}, Container: name, Optional: optBool(s.Optional),
			})
		}
	}

	for _, e := range envFrom {
		if cm := e.ConfigMapRef; cm != nil {
			refs = append(refs, Reference{
				Kind: RefConfigMap, Name: cm.Name, Mode: ModeEnvFrom,
				Container: name, Optional: optBool(cm.Optional),
			})
		}
		if s := e.SecretRef; s != nil {
			refs = append(refs, Reference{
				Kind: RefSecret, Name: s.Name, Mode: ModeEnvFrom,
				Container: name, Optional: optBool(s.Optional),
			})
		}
	}

	return refs
}

func volumeReferences(v corev1.Volume, hasSubPath bool) []Reference {
	mode := ModeVolume
	if hasSubPath {
		mode = ModeVolumeSubPath
	}

	var refs []Reference

	if cm := v.ConfigMap; cm != nil {
		refs = append(refs, Reference{
			Kind: RefConfigMap, Name: cm.Name, Mode: mode,
			Keys: keysFromKeyToPath(cm.Items), Optional: optBool(cm.Optional),
		})
	}
	if s := v.Secret; s != nil {
		refs = append(refs, Reference{
			Kind: RefSecret, Name: s.SecretName, Mode: mode,
			Keys: keysFromKeyToPath(s.Items), Optional: optBool(s.Optional),
		})
	}
	if p := v.Projected; p != nil {
		for _, src := range p.Sources {
			if src.ConfigMap != nil {
				refs = append(refs, Reference{
					Kind: RefConfigMap, Name: src.ConfigMap.Name, Mode: ModeProjected,
					Keys: keysFromKeyToPath(src.ConfigMap.Items), Optional: optBool(src.ConfigMap.Optional),
				})
			}
			if src.Secret != nil {
				refs = append(refs, Reference{
					Kind: RefSecret, Name: src.Secret.Name, Mode: ModeProjected,
					Keys: keysFromKeyToPath(src.Secret.Items), Optional: optBool(src.Secret.Optional),
				})
			}
		}
	}

	return refs
}

// keysFromKeyToPath returns the source keys selected by a volume's items. An
// empty items list projects every key.
func keysFromKeyToPath(items []corev1.KeyToPath) []string {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for _, i := range items {
		keys = append(keys, i.Key)
	}
	sort.Strings(keys)
	return keys
}

func optBool(b *bool) bool { return b != nil && *b }

func dedupe(refs []Reference) []Reference {
	seen := make(map[string]bool, len(refs))
	out := refs[:0]
	for _, r := range refs {
		id := r.identity()
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		if out[i].Container != out[j].Container {
			return out[i].Container < out[j].Container
		}
		return out[i].Mode < out[j].Mode
	})
	return out
}
