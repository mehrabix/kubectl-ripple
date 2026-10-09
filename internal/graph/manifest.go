package graph

import (
	"bytes"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

// ParseObject parses a ConfigMap or Secret manifest into an ObjectRef and its
// flat key/value data. For Secrets, base64-encoded values are decoded so the
// comparison is against the real content.
func ParseObject(raw []byte) (ObjectRef, map[string]string, error) {
	var meta struct {
		Kind     string `json:"kind"`
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	}
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		return ObjectRef{}, nil, fmt.Errorf("parsing manifest: %w", err)
	}

	switch meta.Kind {
	case "ConfigMap":
		var cm corev1.ConfigMap
		if err := yaml.Unmarshal(raw, &cm); err != nil {
			return ObjectRef{}, nil, fmt.Errorf("parsing ConfigMap: %w", err)
		}
		if cm.Name == "" {
			return ObjectRef{}, nil, fmt.Errorf("ConfigMap manifest has no metadata.name")
		}
		return ObjectRef{Kind: RefConfigMap, Namespace: cm.Namespace, Name: cm.Name}, cm.Data, nil

	case "Secret":
		var s corev1.Secret
		if err := yaml.Unmarshal(raw, &s); err != nil {
			return ObjectRef{}, nil, fmt.Errorf("parsing Secret: %w", err)
		}
		if s.Name == "" {
			return ObjectRef{}, nil, fmt.Errorf("Secret manifest has no metadata.name")
		}
		data := make(map[string]string, len(s.Data)+len(s.StringData))
		for k, v := range s.Data {
			data[k] = string(v)
		}
		for k, v := range s.StringData {
			data[k] = v
		}
		return ObjectRef{Kind: RefSecret, Namespace: s.Namespace, Name: s.Name}, data, nil

	case "":
		return ObjectRef{}, nil, fmt.Errorf("manifest has no kind")
	default:
		return ObjectRef{}, nil, fmt.Errorf("unsupported kind %q: expected ConfigMap or Secret", meta.Kind)
	}
}

// ParseObjects parses a multi-document YAML stream, returning every ConfigMap
// and Secret it contains.
func ParseObjects(raw []byte) ([]Object, error) {
	var out []Object
	for _, doc := range bytes.Split(raw, []byte("\n---")) {
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		ref, data, err := ParseObject(doc)
		if err != nil {
			// Skip documents that are neither ConfigMap nor Secret; only
			// fail when the document looks like one but is malformed.
			if bytes.Contains(doc, []byte("kind: ConfigMap")) || bytes.Contains(doc, []byte("kind: Secret")) {
				return nil, err
			}
			continue
		}
		out = append(out, Object{Ref: ref, Data: data})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no ConfigMap or Secret found in the manifest")
	}
	return out, nil
}
