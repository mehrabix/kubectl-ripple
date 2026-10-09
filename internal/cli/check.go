package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mehrabix/kubectl-ripple/internal/graph"
	"github.com/mehrabix/kubectl-ripple/internal/render"
)

func newCheckCmd(o *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Fail when a workload references a ConfigMap or Secret that does not exist",
		Long: `Verify that every ConfigMap and Secret a workload references actually exists.

A typo in a reference name is a common cause of a pod that never starts. This
command turns that into a non-zero exit code, which makes it usable as a CI or
pre-deploy gate.`,
		Example: `  kubectl ripple check -n prod
  kubectl ripple check -A --include-system`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			objects, err := l.ConfigObjects(ctx, ns, nil)
			if err != nil {
				return err
			}
			existing := map[string]bool{}
			for _, obj := range objects {
				existing[obj.Ref.Display()] = true
			}

			dangling := g.Dangling(existing)

			if o.Output == "json" {
				out := make([]map[string]string, 0, len(dangling))
				for _, d := range dangling {
					out = append(out, map[string]string{
						"kind":      string(d.Ref.Kind),
						"namespace": d.Workload.Namespace,
						"name":      d.Ref.Name,
						"workload":  d.Workload.Kind + "/" + d.Workload.Name,
						"mode":      string(d.Ref.Mode),
					})
				}
				if err := render.JSON(cmd.OutOrStdout(), map[string]any{
					"ok":       len(dangling) == 0,
					"dangling": out,
				}); err != nil {
					return err
				}
				if len(dangling) > 0 {
					return ErrFindings
				}
				return nil
			}

			w := cmd.OutOrStdout()
			p := painter(o)

			if len(dangling) == 0 {
				fmt.Fprintf(w, "%s every ConfigMap and Secret reference resolves\n", p.Green("ok:"))
				return nil
			}

			var rows [][]string
			for _, d := range dangling {
				rows = append(rows, []string{
					d.Workload.Namespace,
					d.Workload.Kind + "/" + d.Workload.Name,
					fmt.Sprintf("%s/%s", d.Ref.Kind, d.Ref.Name),
					string(d.Ref.Mode),
				})
			}
			render.Table(w, []string{"NAMESPACE", "WORKLOAD", "MISSING REFERENCE", "MODE"}, rows)
			fmt.Fprintf(w, "\n%s\n", p.Red(plural(len(dangling), "dangling reference")))
			return ErrFindings
		},
	}
	return cmd
}
