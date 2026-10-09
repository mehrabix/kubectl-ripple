package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mehrabix/kubectl-ripple/internal/graph"
	"github.com/mehrabix/kubectl-ripple/internal/render"
)

func newWhoRefsCmd(o *Options) *cobra.Command {
	var kindFlag string

	cmd := &cobra.Command{
		Use:     "who-refs <kind>/<name>",
		Aliases: []string{"who", "refs"},
		Short:   "Show which workloads reference a ConfigMap or Secret",
		Long: `Show every workload that consumes a ConfigMap or Secret, how it consumes it
(env, envFrom, volume, ...) and whether a change would require a restart.`,
		Example: `  kubectl ripple who-refs configmap/app-config -n prod
  kubectl ripple who-refs secret/db-creds -A
  kubectl ripple who-refs app-config --kind configmap -n prod`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, name, err := objectArg(args[0], kindFlag)
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
			workloads, err := l.Workloads(cmd.Context(), ns)
			if err != nil {
				return err
			}
			g := &graph.Graph{Workloads: visibleWorkloads(workloads, o.IncludeSystem)}
			consumers := g.Consumers(kind, ns, name)

			if o.Output == "json" {
				return render.JSON(cmd.OutOrStdout(), map[string]any{
					"kind":      kind,
					"namespace": ns,
					"name":      name,
					"consumers": toConsumersJSON(consumers),
				})
			}

			out := cmd.OutOrStdout()
			p := painter(o)
			target := fmt.Sprintf("%s/%s", kind, name)

			if len(consumers) == 0 {
				fmt.Fprintf(out, "%s is not referenced by any workload in scope.\n", target)
				return nil
			}

			fmt.Fprintf(out, "%s is referenced by %s (%s)\n\n",
				p.Bold(target), plural(len(consumers), "workload"), tally(consumers, p))

			var rows [][]string
			for _, c := range consumers {
				for _, r := range c.Refs {
					rows = append(rows, []string{
						c.Workload.Namespace,
						c.Workload.Kind + "/" + c.Workload.Name,
						string(r.Mode),
						effectCell(p, r.Mode),
						orDash(r.Container),
						keysLabel(r),
					})
				}
			}
			render.Table(out, []string{"NAMESPACE", "WORKLOAD", "MODE", "EFFECT", "CONTAINER", "KEYS"}, rows)
			return nil
		},
	}

	cmd.Flags().StringVar(&kindFlag, "kind", "", "object kind (configmap|secret) when the argument is a bare name")
	return cmd
}
