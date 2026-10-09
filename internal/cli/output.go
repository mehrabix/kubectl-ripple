package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/mehrabix/kubectl-ripple/internal/graph"
	"github.com/mehrabix/kubectl-ripple/internal/render"
)

// effect describes what a change to the referenced object does to a workload.
func effect(m graph.RefMode) string {
	switch {
	case m.RestartRequired():
		return "restart required"
	case m.LiveReload():
		return "live reload"
	case m == graph.ModeImagePullSecret:
		return "new pods only"
	default:
		return "none"
	}
}

func effectCell(p render.Painter, m graph.RefMode) string {
	switch {
	case m.RestartRequired():
		return p.Yellow(effect(m))
	case m.LiveReload():
		return p.Green(effect(m))
	default:
		return effect(m)
	}
}

func keysLabel(r graph.Reference) string {
	if r.AllKeys() {
		return "*"
	}
	return strings.Join(r.Keys, ",")
}

func touchedLabel(c graph.Change, r graph.Reference) string {
	if r.AllKeys() {
		return "*"
	}
	hit := c.Touched(r.Keys)
	if len(hit) == 0 {
		return "-"
	}
	return strings.Join(hit, ",")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func colorEnabled(output string) bool {
	if output != "table" {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func painter(o *Options) render.Painter {
	return render.Painter{Enabled: colorEnabled(o.Output)}
}

// tally summarises how many consumers restart versus reload in place.
func tally(consumers []graph.Consumer, p render.Painter) string {
	var restart, reload int
	for _, c := range consumers {
		switch {
		case c.RestartRequired():
			restart++
		case c.LiveReload():
			reload++
		}
	}
	var parts []string
	if restart > 0 {
		parts = append(parts, p.Yellow(fmt.Sprintf("%d restart required", restart)))
	}
	if reload > 0 {
		parts = append(parts, p.Green(fmt.Sprintf("%d live reload", reload)))
	}
	if other := len(consumers) - restart - reload; other > 0 {
		parts = append(parts, fmt.Sprintf("%d no action", other))
	}
	return strings.Join(parts, ", ")
}

// refJSON is the wire form of a reference.
type refJSON struct {
	Mode      string   `json:"mode"`
	Keys      []string `json:"keys,omitempty"`
	AllKeys   bool     `json:"allKeys"`
	Container string   `json:"container,omitempty"`
	Optional  bool     `json:"optional,omitempty"`
}

func toRefJSON(r graph.Reference) refJSON {
	return refJSON{
		Mode:      string(r.Mode),
		Keys:      r.Keys,
		AllKeys:   r.AllKeys(),
		Container: r.Container,
		Optional:  r.Optional,
	}
}

// consumerJSON is the wire form of a workload that references an object.
type consumerJSON struct {
	Kind            string    `json:"kind"`
	Namespace       string    `json:"namespace"`
	Name            string    `json:"name"`
	RestartRequired bool      `json:"restartRequired"`
	LiveReload      bool      `json:"liveReload"`
	References      []refJSON `json:"references"`
}

func toConsumerJSON(c graph.Consumer) consumerJSON {
	refs := make([]refJSON, 0, len(c.Refs))
	for _, r := range c.Refs {
		refs = append(refs, toRefJSON(r))
	}
	return consumerJSON{
		Kind:            c.Workload.Kind,
		Namespace:       c.Workload.Namespace,
		Name:            c.Workload.Name,
		RestartRequired: c.RestartRequired(),
		LiveReload:      c.LiveReload(),
		References:      refs,
	}
}

func toConsumersJSON(consumers []graph.Consumer) []consumerJSON {
	out := make([]consumerJSON, 0, len(consumers))
	for _, c := range consumers {
		out = append(out, toConsumerJSON(c))
	}
	return out
}
