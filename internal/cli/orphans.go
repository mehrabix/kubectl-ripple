package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mehrabix/kubectl-ripple/internal/graph"
	"github.com/mehrabix/kubectl-ripple/internal/render"
)

func newOrphansCmd(o *Options) *cobra.Command {
	var types []string

	cmd := &cobra.Command{
		Use:   "orphans",
		Short: "Find ConfigMaps and Secrets that no workload references",
		Long: `List ConfigMaps and Secrets that no workload in scope consumes.

Objects that are not user configuration (service account tokens, Helm release
records, ArgoCD bookkeeping, docker config secrets) are ignored.`,
		Example: `  kubectl ripple orphans -n prod
  kubectl ripple orphans -A
  kubectl ripple orphans --types configmap`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			kinds, err := parseKinds(types)
			if err != nil {
				return err
			}
			ns, err := o.scanNamespace()
			if err != nil {
				return err
			}
			l, err := o.loader()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			workloads, err := l.Workloads(ctx, ns)
			if err != nil {
				return err
			}
			g := &graph.Graph{Workloads: visibleWorkloads(workloads, o.IncludeSystem)}

			objects, err := l.ConfigObjects(ctx, ns, kinds)
			if err != nil {
				return err
			}
			refs := make([]graph.ObjectRef, 0, len(objects))
			for _, obj := range objects {
				if obj.System {
					continue
				}
				if !o.IncludeSystem && isSystemNamespace(obj.Ref.Namespace) {
					continue
				}
				refs = append(refs, obj.Ref)
			}

			orphans := g.Orphans(refs)

			if o.Output == "json" {
				out := make([]map[string]string, 0, len(orphans))
				for _, or := range orphans {
					out = append(out, map[string]string{
						"kind":      string(or.Kind),
						"namespace": or.Namespace,
						"name":      or.Name,
					})
				}
				return render.JSON(cmd.OutOrStdout(), map[string]any{"orphans": out})
			}

			w := cmd.OutOrStdout()
			p := painter(o)
			if len(orphans) == 0 {
				fmt.Fprintf(w, "%s\n", p.Green("no orphans found"))
				return nil
			}

			var rows [][]string
			for _, or := range orphans {
				rows = append(rows, []string{or.Namespace, string(or.Kind), or.Name})
			}
			render.Table(w, []string{"NAMESPACE", "KIND", "NAME"}, rows)
			fmt.Fprintf(w, "\n%s in scope\n", plural(len(orphans), "unreferenced object"))
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&types, "types", []string{"configmap", "secret"}, "object types to scan (configmap,secret)")
	return cmd
}

func parseKinds(in []string) ([]graph.RefKind, error) {
	var out []graph.RefKind
	for _, s := range in {
		for _, part := range strings.Split(s, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			k, err := parseKind(part)
			if err != nil {
				return nil, err
			}
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--types cannot be empty")
	}
	return out, nil
}
