package graph

import (
	"fmt"
	"sort"
)

// Workload is a Kubernetes object that embeds a pod template.
type Workload struct {
	Kind      string      `json:"kind"`
	Namespace string      `json:"namespace"`
	Name      string      `json:"name"`
	Refs      []Reference `json:"refs"`
}

// Display renders the workload as kind/namespace/name.
func (w Workload) Display() string {
	return fmt.Sprintf("%s/%s/%s", w.Kind, w.Namespace, w.Name)
}

// ObjectRef identifies a ConfigMap or Secret.
type ObjectRef struct {
	Kind      RefKind `json:"kind"`
	Namespace string  `json:"namespace"`
	Name      string  `json:"name"`
}

// Display renders the object as kind/namespace/name.
func (o ObjectRef) Display() string {
	return fmt.Sprintf("%s/%s/%s", o.Kind, o.Namespace, o.Name)
}

// Graph holds every workload discovered in the scanned scope.
type Graph struct {
	Workloads []Workload `json:"workloads"`
}

// Sort orders workloads deterministically.
func (g *Graph) Sort() {
	sort.SliceStable(g.Workloads, func(i, j int) bool {
		a, b := g.Workloads[i], g.Workloads[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Name < b.Name
	})
}

// Consumer is a workload together with the references that point at one object.
type Consumer struct {
	Workload Workload
	Refs     []Reference
}

// RestartRequired reports whether at least one reference forces a rollout.
func (c Consumer) RestartRequired() bool {
	for _, r := range c.Refs {
		if r.Mode.RestartRequired() {
			return true
		}
	}
	return false
}

// LiveReload reports whether every reference is refreshed without a restart.
func (c Consumer) LiveReload() bool {
	for _, r := range c.Refs {
		if r.Mode.RestartRequired() {
			return false
		}
	}
	return true
}

// Consumers returns every workload referencing the given object. An empty
// namespace matches workloads in any namespace.
func (g *Graph) Consumers(kind RefKind, namespace, name string) []Consumer {
	var out []Consumer
	for _, w := range g.Workloads {
		if namespace != "" && w.Namespace != namespace {
			continue
		}
		var matched []Reference
		for _, r := range w.Refs {
			if r.Kind == kind && r.Name == name {
				matched = append(matched, r)
			}
		}
		if len(matched) > 0 {
			out = append(out, Consumer{Workload: w, Refs: matched})
		}
	}
	return out
}

// Dangling is a reference to an object that does not exist.
type Dangling struct {
	Workload Workload
	Ref      Reference
}

// Dangling returns references whose target object is absent from existing.
// existing is keyed by ObjectRef.Display().
func (g *Graph) Dangling(existing map[string]bool) []Dangling {
	var out []Dangling
	for _, w := range g.Workloads {
		for _, r := range w.Refs {
			key := ObjectRef{Kind: r.Kind, Namespace: w.Namespace, Name: r.Name}.Display()
			if !existing[key] {
				out = append(out, Dangling{Workload: w, Ref: r})
			}
		}
	}
	return out
}

// ReferencedObjects returns the set of objects any workload points at, keyed by
// ObjectRef.Display().
func (g *Graph) ReferencedObjects() map[string]bool {
	out := map[string]bool{}
	for _, w := range g.Workloads {
		for _, r := range w.Refs {
			out[ObjectRef{Kind: r.Kind, Namespace: w.Namespace, Name: r.Name}.Display()] = true
		}
	}
	return out
}

// Orphans returns objects that no scanned workload references.
func (g *Graph) Orphans(objects []ObjectRef) []ObjectRef {
	referenced := g.ReferencedObjects()
	var out []ObjectRef
	for _, o := range objects {
		if !referenced[o.Display()] {
			out = append(out, o)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Display() < out[j].Display() })
	return out
}

// Change is the set of key-level differences between two versions of an object.
type Change struct {
	Added   []string
	Removed []string
	Changed []string
}

// Empty reports whether nothing changed.
func (c Change) Empty() bool {
	return len(c.Added) == 0 && len(c.Removed) == 0 && len(c.Changed) == 0
}

// Keys returns every key touched by the change.
func (c Change) Keys() []string {
	out := make([]string, 0, len(c.Added)+len(c.Removed)+len(c.Changed))
	out = append(out, c.Added...)
	out = append(out, c.Removed...)
	out = append(out, c.Changed...)
	sort.Strings(out)
	return out
}

// Summary renders a short human description such as "2 changed, 1 added".
func (c Change) Summary() string {
	var parts []string
	if n := len(c.Changed); n > 0 {
		parts = append(parts, fmt.Sprintf("%d changed", n))
	}
	if n := len(c.Added); n > 0 {
		parts = append(parts, fmt.Sprintf("%d added", n))
	}
	if n := len(c.Removed); n > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", n))
	}
	if len(parts) == 0 {
		return "no change"
	}
	return join(parts, ", ")
}

// DiffKeys compares two flat key/value maps.
func DiffKeys(old, new map[string]string) Change {
	var c Change
	for k, nv := range new {
		ov, ok := old[k]
		if !ok {
			c.Added = append(c.Added, k)
			continue
		}
		if ov != nv {
			c.Changed = append(c.Changed, k)
		}
	}
	for k := range old {
		if _, ok := new[k]; !ok {
			c.Removed = append(c.Removed, k)
		}
	}
	sort.Strings(c.Added)
	sort.Strings(c.Removed)
	sort.Strings(c.Changed)
	return c
}

// Affects reports whether a change to the object matters to a given reference.
// A reference that reads specific keys only cares about those keys; a reference
// that consumes the whole object cares about any change.
func (c Change) Affects(ref Reference) bool {
	if c.Empty() {
		return false
	}
	if ref.AllKeys() {
		return true
	}
	touched := c.Keys()
	for _, k := range ref.Keys {
		if contains(touched, k) {
			return true
		}
	}
	return false
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// Touched returns the subset of keys that the change modifies, preserving the
// order of keys.
func (c Change) Touched(keys []string) []string {
	touched := c.Keys()
	var out []string
	for _, k := range keys {
		if contains(touched, k) {
			out = append(out, k)
		}
	}
	return out
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
