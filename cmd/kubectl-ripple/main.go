// Command kubectl-ripple shows what a change to a ConfigMap or Secret will do
// to the workloads that consume it.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/mehrabix/kubectl-ripple/internal/cli"
)

func main() {
	root := cli.NewRootCmd(os.Stdout)
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)

	if err := root.Execute(); err != nil {
		if errors.Is(err, cli.ErrFindings) {
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
