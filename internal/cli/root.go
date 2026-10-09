// Package cli implements the ripple command line interface.
package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/mehrabix/kubectl-ripple/internal/graph"
)

// ErrFindings is returned by check when it has something to report. main maps
// it to exit code 1 without printing an extra "Error:" line.
var ErrFindings = errors.New("findings")

// Version is set at build time via -ldflags.
var Version = "dev"

// ConfigFlags carries the connection options shared by every command.
type ConfigFlags struct {
	Kubeconfig string
	Context    string
	Namespace  string
}

// RESTConfig resolves the client configuration.
func (f *ConfigFlags) RESTConfig() (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if f.Kubeconfig != "" {
		rules.ExplicitPath = f.Kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if f.Context != "" {
		overrides.CurrentContext = f.Context
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}
	return cfg, nil
}

// Scope resolves the namespace to scan. An empty string means all namespaces.
func (f *ConfigFlags) Scope(allNamespaces bool) (string, error) {
	if allNamespaces {
		return "", nil
	}
	if f.Namespace != "" {
		return f.Namespace, nil
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if f.Kubeconfig != "" {
		rules.ExplicitPath = f.Kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if f.Context != "" {
		overrides.CurrentContext = f.Context
	}
	raw, _, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).Namespace()
	if err != nil || raw == "" {
		return "default", nil
	}
	return raw, nil
}

// Options is the resolved runtime state shared by the commands.
type Options struct {
	Config        *ConfigFlags
	Output        string
	AllNamespaces bool
	IncludeSystem bool
}

func (o *Options) loader() (*graph.Loader, error) {
	cfg, err := o.Config.RESTConfig()
	if err != nil {
		return nil, err
	}
	return graph.NewLoader(cfg)
}

// scanNamespace returns the namespace to query; "" means all namespaces.
func (o *Options) scanNamespace() (string, error) {
	return o.Config.Scope(o.AllNamespaces)
}

// visibleWorkloads drops workloads that live in system namespaces unless the
// caller asked for them.
func visibleWorkloads(workloads []graph.Workload, includeSystem bool) []graph.Workload {
	if includeSystem {
		return workloads
	}
	out := make([]graph.Workload, 0, len(workloads))
	for _, w := range workloads {
		if isSystemNamespace(w.Namespace) {
			continue
		}
		out = append(out, w)
	}
	return out
}

// NewRootCmd builds the whole command tree.
func NewRootCmd(out io.Writer) *cobra.Command {
	opts := &Options{Config: &ConfigFlags{}}

	root := &cobra.Command{
		Use:   "kubectl-ripple",
		Short: "See the ripple before you make the change",
		Long: `ripple maps the ConfigMaps and Secrets that Kubernetes workloads consume,
and tells you what a change or a delete will actually do.

It is read-only: it never modifies anything in your cluster.`,
		Example: `  # who references a ConfigMap, and would a change restart them?
  kubectl ripple who-refs configmap/app-config -n prod

  # what happens if I apply this new version?
  kubectl ripple impact configmap/app-config --from-file=app-config.yaml -n prod

  # which ConfigMaps and Secrets does nothing reference?
  kubectl ripple orphans -A

  # fail CI on references to objects that do not exist
  kubectl ripple check -n prod`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.StringVar(&opts.Config.Kubeconfig, "kubeconfig", "", "path to the kubeconfig file")
	pf.StringVar(&opts.Config.Context, "context", "", "name of the kubeconfig context to use")
	pf.StringVarP(&opts.Config.Namespace, "namespace", "n", "", "namespace to scan")
	pf.BoolVarP(&opts.AllNamespaces, "all-namespaces", "A", false, "scan every namespace")
	pf.StringVarP(&opts.Output, "output", "o", "table", "output format: table|json|wide")
	pf.BoolVar(&opts.IncludeSystem, "include-system", false, "include system namespaces (kube-system, kube-public, kube-node-lease)")

	root.AddCommand(
		newWhoRefsCmd(opts),
		newImpactCmd(opts),
		newOrphansCmd(opts),
		newCheckCmd(opts),
		newVersionCmd(),
	)
	return root
}

// objectArg parses "kind/name" or a bare name with an explicit kind flag.
func objectArg(arg, kindFlag string) (graph.RefKind, string, error) {
	kind, name, found := strings.Cut(arg, "/")
	if !found {
		if kindFlag == "" {
			return "", "", fmt.Errorf("expected <kind>/<name> (for example configmap/app-config) or set --kind")
		}
		k, err := parseKind(kindFlag)
		if err != nil {
			return "", "", err
		}
		if arg == "" {
			return "", "", fmt.Errorf("object name is required")
		}
		return k, arg, nil
	}
	k, err := parseKind(kind)
	if err != nil {
		return "", "", err
	}
	if name == "" {
		return "", "", fmt.Errorf("object name is required")
	}
	return k, name, nil
}

func parseKind(s string) (graph.RefKind, error) {
	switch strings.ToLower(s) {
	case "configmap", "configmaps", "cm":
		return graph.RefConfigMap, nil
	case "secret", "secrets":
		return graph.RefSecret, nil
	default:
		return "", fmt.Errorf("unsupported kind %q: expected configmap or secret", s)
	}
}

// isSystemNamespace reports whether a namespace is a Kubernetes-owned one.
func isSystemNamespace(ns string) bool {
	switch ns {
	case "kube-system", "kube-public", "kube-node-lease":
		return true
	}
	return strings.HasPrefix(ns, "kube-")
}
