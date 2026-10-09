// Package render formats command output.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Table writes rows as an aligned text table.
func Table(w io.Writer, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

// JSON writes v as indented JSON.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Painter adds ANSI colour when the output is a terminal.
type Painter struct{ Enabled bool }

// Red renders s in red when colour is on.
func (p Painter) Red(s string) string { return p.wrap("31", s) }

// Green renders s in green when colour is on.
func (p Painter) Green(s string) string { return p.wrap("32", s) }

// Yellow renders s in yellow when colour is on.
func (p Painter) Yellow(s string) string { return p.wrap("33", s) }

// Bold renders s bold when colour is on.
func (p Painter) Bold(s string) string { return p.wrap("1", s) }

// Dim renders s dimmed when colour is on.
func (p Painter) Dim(s string) string { return p.wrap("2", s) }

func (p Painter) wrap(code, s string) string {
	if !p.Enabled || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
