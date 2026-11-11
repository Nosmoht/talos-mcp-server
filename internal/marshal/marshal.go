// Package marshal reads and converts COSI resources for tools and resources, and lives apart from both to avoid an import cycle.
package marshal

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/state"
	yaml "go.yaml.in/yaml/v4"
)

// ReadResources gets one resource, or lists a type when id is empty, and marshals each item; limit <= 0 means no cap.
func ReadResources(ctx context.Context, st state.State, ns resource.Namespace, typ resource.Type, id resource.ID, limit int) ([]map[string]any, bool, error) {
	if id != "" {
		r, err := st.Get(ctx, resource.NewMetadata(ns, typ, id, resource.VersionUndefined))
		if err != nil {
			return nil, false, fmt.Errorf("get resource %s/%s/%s: %w", ns, typ, id, err)
		}
		data, err := Resource(r)
		if err != nil {
			return nil, false, fmt.Errorf("marshal resource: %w", err)
		}
		return []map[string]any{data}, false, nil
	}

	list, err := st.List(ctx, resource.NewMetadata(ns, typ, "", resource.VersionUndefined))
	if err != nil {
		return nil, false, fmt.Errorf("list resources %s/%s: %w", ns, typ, err)
	}
	var items []map[string]any
	for _, r := range list.Items {
		if limit > 0 && len(items) >= limit {
			return items, true, nil
		}
		data, err := Resource(r)
		if err != nil {
			return nil, false, fmt.Errorf("marshal resource: %w", err)
		}
		items = append(items, data)
	}
	return items, false, nil
}

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
