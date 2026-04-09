// Package marshal converts COSI resources to JSON-serializable maps.
// Both the tools and resources packages need this conversion; placing it
// here avoids a circular dependency between those two packages.
package marshal

import (
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	yaml "go.yaml.in/yaml/v4"
)

// Resource converts a COSI resource to a JSON-serializable map.
// It uses the same path as talosctl get --output json:
//
//	resource.MarshalYAML → yaml.Marshal → yaml.Unmarshal → map[string]any
//
// The YAML library used is go.yaml.in/yaml/v4 which follows YAML 1.2 semantics.
// YAML 1.1 boolean strings ("yes", "no", "on", "off") are preserved as strings.
// Standard YAML 1.2 booleans ("true"/"false") and unquoted numbers are coerced
// to their Go types (bool, int, float64), which is correct and expected.
func Resource(r resource.Resource) (map[string]any, error) {
	out, err := resource.MarshalYAML(r)
	if err != nil {
		return nil, err
	}

	yamlBytes, err := yaml.Marshal(out)
	if err != nil {
		return nil, err
	}

	var data map[string]any

	if err = yaml.Unmarshal(yamlBytes, &data); err != nil {
		return nil, err
	}

	return data, nil
}

// ResourceDefinition converts a ResourceDefinition to a compact summary map.
func ResourceDefinition(rd *meta.ResourceDefinition) map[string]any {
	spec := rd.TypedSpec()

	return map[string]any{
		"type":              spec.Type,
		"display_type":      spec.DisplayType,
		"default_namespace": spec.DefaultNamespace,
		"aliases":           spec.Aliases,
		"printColumns":      spec.PrintColumns,
	}
}
