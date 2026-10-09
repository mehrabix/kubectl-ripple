package graph

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// workloadResources are the built-in kinds that embed a pod template.
var workloadResources = []schema.GroupVersionResource{
	{Group: "apps", Version: "v1", Resource: "deployments"},
	{Group: "apps", Version: "v1", Resource: "statefulsets"},
	{Group: "apps", Version: "v1", Resource: "daemonsets"},
	{Group: "apps", Version: "v1", Resource: "replicasets"},
	{Group: "batch", Version: "v1", Resource: "cronjobs"},
	{Group: "batch", Version: "v1", Resource: "jobs"},
	{Group: "", Version: "v1", Resource: "replicationcontrollers"},
	{Group: "", Version: "v1", Resource: "pods"},
}

// scannedKinds are the kinds we look at; an object owned by one of these is
// skipped so a Deployment does not show up three times through its
// ReplicaSet and Pods.
var scannedKinds = map[string]bool{
	"Deployment":            true,
	"StatefulSet":           true,
	"DaemonSet":             true,
	"ReplicaSet":            true,
	"CronJob":               true,
	"Job":                   true,
	"ReplicationController": true,
	"Pod":                   true,
}

// Loader reads workloads and configuration objects from a cluster.
type Loader struct {
	Dynamic dynamic.Interface
	Client  kubernetes.Interface
}

// NewLoader builds a Loader from a rest config.
func NewLoader(cfg *rest.Config) (*Loader, error) {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating clientset: %w", err)
	}
	return &Loader{Dynamic: dyn, Client: cs}, nil
}

// Workloads lists every pod-template-owning workload in the namespace. An empty
// namespace means all namespaces.
func (l *Loader) Workloads(ctx context.Context, namespace string) ([]Workload, error) {
	var out []Workload
	for _, gvr := range workloadResources {
		list, err := l.Dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			// A cluster may not serve every group (e.g. a stripped-down
			// distribution without batch/v1); skip rather than fail.
			continue
		}
		for i := range list.Items {
			u := &list.Items[i]
			if ownedByScannedKind(u) {
				continue
			}
			spec, ok := PodSpecFrom(u)
			if !ok {
				continue
			}
			out = append(out, Workload{
				Kind:      u.GetKind(),
				Namespace: u.GetNamespace(),
				Name:      u.GetName(),
				Refs:      ExtractReferences(spec),
			})
		}
	}
	g := &Graph{Workloads: out}
	g.Sort()
	return g.Workloads, nil
}

// ownedByScannedKind reports whether the object is controlled by another
// workload we also scan.
func ownedByScannedKind(u *unstructured.Unstructured) bool {
	for _, ref := range u.GetOwnerReferences() {
		if scannedKinds[ref.Kind] {
			return true
		}
	}
	return false
}

// PodSpecFrom extracts the pod spec out of any built-in workload object.
func PodSpecFrom(u *unstructured.Unstructured) (corev1.PodSpec, bool) {
	paths := [][]string{
		{"spec", "jobTemplate", "spec", "template", "spec"}, // CronJob
		{"spec", "template", "spec"},                        // Deployment, StatefulSet, DaemonSet, ReplicaSet, Job, RC
		{"spec"},                                            // Pod
	}
	for _, p := range paths {
		raw, found, err := unstructured.NestedMap(u.Object, p...)
		if err != nil || !found || raw == nil {
			continue
		}
		var spec corev1.PodSpec
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &spec); err != nil {
			continue
		}
		return spec, true
	}
	return corev1.PodSpec{}, false
}

// Object is a ConfigMap or Secret together with its data.
type Object struct {
	Ref  ObjectRef
	Data map[string]string
	// System marks objects that Kubernetes or an operator manages rather than
	// user configuration. They are still valid reference targets, but they
	// should not be reported as orphans.
	System bool
}

// ConfigObjects lists ConfigMaps and/or Secrets in the namespace. An empty
// namespace means all namespaces.
func (l *Loader) ConfigObjects(ctx context.Context, namespace string, kinds []RefKind) ([]Object, error) {
	want := map[RefKind]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	if len(want) == 0 {
		want[RefConfigMap] = true
		want[RefSecret] = true
	}

	var out []Object

	if want[RefConfigMap] {
		list, err := l.Client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("listing configmaps: %w", err)
		}
		for _, cm := range list.Items {
			out = append(out, Object{
				Ref:    ObjectRef{Kind: RefConfigMap, Namespace: cm.Namespace, Name: cm.Name},
				Data:   cm.Data,
				System: isSystemConfigMap(cm),
			})
		}
	}

	if want[RefSecret] {
		list, err := l.Client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("listing secrets: %w", err)
		}
		for _, s := range list.Items {
			data := make(map[string]string, len(s.Data))
			for k, v := range s.Data {
				data[k] = string(v)
			}
			for k := range s.StringData {
				data[k] = s.StringData[k]
			}
			out = append(out, Object{
				Ref:    ObjectRef{Kind: RefSecret, Namespace: s.Namespace, Name: s.Name},
				Data:   data,
				System: isSystemSecret(s),
			})
		}
	}

	return out, nil
}

// isSystemConfigMap reports whether a ConfigMap is managed by Kubernetes rather
// than by the user.
func isSystemConfigMap(cm corev1.ConfigMap) bool {
	switch cm.Name {
	case "kube-root-ca.crt", "extension-apiserver-authentication":
		return true
	}
	return false
}

// isSystemSecret reports whether a Secret is managed by Kubernetes, Helm or
// ArgoCD rather than by the user.
func isSystemSecret(s corev1.Secret) bool {
	switch s.Type {
	case corev1.SecretTypeServiceAccountToken,
		corev1.SecretTypeDockercfg,
		corev1.SecretTypeDockerConfigJson,
		corev1.SecretTypeBootstrapToken,
		corev1.SecretTypeBasicAuth,
		corev1.SecretTypeSSHAuth:
		return true
	}
	if _, ok := s.Annotations["kubernetes.io/service-account.name"]; ok {
		return true
	}
	if s.Name == "kube-root-ca.crt" {
		return true
	}
	if v, ok := s.Labels["owner"]; ok && v == "helm" {
		return true
	}
	if _, ok := s.Labels["argocd.argoproj.io/secret-type"]; ok {
		return true
	}
	return false
}
