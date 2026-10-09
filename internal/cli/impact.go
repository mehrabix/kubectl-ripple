package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mehrabix/kubectl-ripple/internal/graph"
	"github.com/mehrabix/kubectl-ripple/internal/render"
)

func newImpactCmd(o *Options) *cobra.Command {
	var (
		kindFlag string
		fromFile string
	)

	cmd := &cobra.Command{
		Use:   "impact <kind>/<name> [--from-file=<manifest>]",
		Short: "Show what changing a ConfigMap or Secret will do to running workloads",
		Long: `Show every workload that consumes a ConfigMap or Secret and what a change does
to it.

With --from-file the new manifest is compared against the live object key by
key, so a workload that reads only keys you did not touch is reported as
unaffected. Without --from-file, any key is assumed to change.`,
		Example: `  kubectl ripple impact configmap/app-config --from-file=app-config.yaml -n prod
  kubectl ripple impact secret/db-creds -n prod`,
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
			ctx := cmd.Context()

			workloads, err := l.Workloads(ctx, ns)
			if err != nil {
				return err
			}
			g := &graph.Graph{Workloads: visibleWorkloads(workloads, o.IncludeSystem)}

			var (
				change   graph.Change
				haveDiff bool
				notes    []string
			)

			if fromFile != "" {
				raw, err := os.ReadFile(fromFile)
				if err != nil {
					return fmt.Errorf("reading %s: %w", fromFile, err)
				}
				ref, newData, err := graph.ParseObject(raw)
				if err != nil {
					return err
				}
				if ref.Kind != kind {
					return fmt.Errorf("%s contains a %s but you asked about a %s", fromFile, ref.Kind, kind)
				}
				if ref.Name != name {
					notes = append(notes, fmt.Sprintf("the manifest defines %s/%s, not %s", ref.Kind, ref.Name, name))
				}
				name = ref.Name

				targetNS := ref.Namespace
				if targetNS == "" {
					targetNS = ns
				}
				oldData, found, err := liveData(ctx, l, ref, targetNS)
				if err != nil {
					return err
				}
				if !found {
					notes = append(notes, "the object does not exist in the cluster yet; every key counts as new")
				}
				change = graph.DiffKeys(oldData, newData)
				haveDiff = true
			}

			consumers := g.Consumers(kind, ns, name)

			if o.Output == "json" {
				res := map[string]any{
					"kind":      kind,
					"namespace": ns,
					"name":      name,
					"consumers": toConsumersJSON(consumers),
				}
				if haveDiff {
					res["change"] = map[string]any{
						"added":   change.Added,
						"removed": change.Removed,
						"changed": change.Changed,
					}
				}
				return render.JSON(cmd.OutOrStdout(), res)
			}

			out := cmd.OutOrStdout()
			p := painter(o)
			target := fmt.Sprintf("%s/%s", kind, name)

			fmt.Fprintf(out, "%s (namespace %s)\n\n", p.Bold(target), orDash(ns))
			switch {
			case haveDiff:
				fmt.Fprintf(out, "  %s\n", change.Summary())
			default:
				fmt.Fprintf(out, "  no --from-file given; assuming any key may change\n")
			}
			for _, n := range notes {
				fmt.Fprintf(out, "  %s\n", p.Dim("note: "+n))
			}
			fmt.Fprintln(out)

			if len(consumers) == 0 {
				fmt.Fprintf(out, "%s is not referenced by any workload in scope.\n", target)
				return nil
			}

			var rows [][]string
			affected := 0
			for _, c := range consumers {
				for _, r := range c.Refs {
					eff := effectCell(p, r.Mode)
					keys := keysLabel(r)
					if haveDiff {
						if change.Affects(r) {
							affected++
							keys = touchedLabel(change, r)
						} else {
							eff = p.Dim("unaffected")
							keys = "-"
						}
					} else {
						affected++
					}
					rows = append(rows, []string{
						c.Workload.Namespace,
						c.Workload.Kind + "/" + c.Workload.Name,
						string(r.Mode),
						eff,
						keys,
					})
				}
			}
			render.Table(out, []string{"NAMESPACE", "WORKLOAD", "MODE", "EFFECT", "KEYS"}, rows)

			if haveDiff && affected == 0 {
				fmt.Fprintf(out, "\nthe change does not touch any key these workloads read; nothing will restart\n")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&kindFlag, "kind", "", "object kind (configmap|secret) when the argument is a bare name")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "path to the new ConfigMap or Secret manifest")
	return cmd
}

// liveData reads the current data of an object, returning found=false when the
// object does not exist.
func liveData(ctx context.Context, l *graph.Loader, ref graph.ObjectRef, namespace string) (map[string]string, bool, error) {
	objects, err := l.ConfigObjects(ctx, namespace, []graph.RefKind{ref.Kind})
	if err != nil {
		return nil, false, err
	}
	for _, o := range objects {
		if o.Ref.Name == ref.Name && (namespace == "" || o.Ref.Namespace == namespace) {
			return o.Data, true, nil
		}
	}
	return nil, false, nil
}
