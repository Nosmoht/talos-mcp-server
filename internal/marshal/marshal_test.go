package marshal

import (
	"testing"

	"github.com/cosi-project/runtime/pkg/resource"
)

// testSpec is a minimal resource spec for MarshalResource tests.
type testSpec struct {
	Name       string `yaml:"name"`
	BoolYes    string `yaml:"bool_yes"`
	BoolNo     string `yaml:"bool_no"`
	BoolOn     string `yaml:"bool_on"`
	BoolOff    string `yaml:"bool_off"`
	VersionStr string `yaml:"version_str"`
	FloatStr   string `yaml:"float_str"`
}

// testRes implements resource.Resource for unit tests.
type testRes struct {
	md   resource.Metadata
	spec testSpec
}

func (r *testRes) Metadata() *resource.Metadata { return &r.md }
func (r *testRes) Spec() any                    { return r.spec }
func (r *testRes) DeepCopy() resource.Resource  { return r }

// newTestResource constructs a testRes with the given namespace, type, and id.
func newTestResource(ns, typ, id string, spec testSpec) *testRes {
	return &testRes{
		md:   resource.NewMetadata(ns, typ, id, resource.VersionUndefined),
		spec: spec,
	}
}

// TestMarshalResource_Structure verifies that the output contains top-level
// "metadata" and "spec" keys.
func TestMarshalResource_Structure(t *testing.T) {
	r := newTestResource("default", "TestType", "test-id", testSpec{Name: "hello"})

	data, err := Resource(r)
	if err != nil {
		t.Fatalf("MarshalResource: %v", err)
	}

	if _, ok := data["metadata"]; !ok {
		t.Error("output missing top-level key 'metadata'")
	}

	if _, ok := data["spec"]; !ok {
		t.Error("output missing top-level key 'spec'")
	}
}

// TestMarshalResource_MetadataFields verifies that namespace, type, and id
// survive the YAML round-trip with the correct values.
func TestMarshalResource_MetadataFields(t *testing.T) {
	r := newTestResource("network", "Interface", "eth0", testSpec{Name: "x"})

	data, err := Resource(r)
	if err != nil {
		t.Fatalf("MarshalResource: %v", err)
	}

	md, ok := data["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata is %T, want map[string]any", data["metadata"])
	}

	checks := map[string]string{
		"namespace": "network",
		"type":      "Interface",
		"id":        "eth0",
	}

	for field, want := range checks {
		got, ok := md[field]
		if !ok {
			t.Errorf("metadata missing field %q", field)
			continue
		}

		if got != want {
			t.Errorf("metadata[%q] = %q, want %q", field, got, want)
		}
	}
}

// TestMarshalResource_StringPreservation verifies that Go string fields with
// YAML-significant values survive the round-trip as strings.
//
// go.yaml.in/yaml/v4 uses YAML 1.2 semantics:
//   - "yes", "no", "on", "off" are NOT boolean (YAML 1.1 artefacts) — stay strings
//   - "true", "false" are YAML 1.2 booleans but go/yaml marshals Go strings with
//     quotes so they round-trip as strings
//   - Version strings like "v1.9.0" contain no YAML-significant fragments — stay strings
func TestMarshalResource_StringPreservation(t *testing.T) {
	spec := testSpec{
		BoolYes:    "yes",
		BoolNo:     "no",
		BoolOn:     "on",
		BoolOff:    "off",
		VersionStr: "v1.9.0",
		FloatStr:   "1.0",
	}

	r := newTestResource("default", "T", "id", spec)

	data, err := Resource(r)
	if err != nil {
		t.Fatalf("MarshalResource: %v", err)
	}

	specMap, ok := data["spec"].(map[string]any)
	if !ok {
		t.Fatalf("spec is %T, want map[string]any", data["spec"])
	}

	// All of these were Go strings; they must remain strings after the round-trip.
	stringFields := map[string]string{
		"bool_yes":    "yes",
		"bool_no":     "no",
		"bool_on":     "on",
		"bool_off":    "off",
		"version_str": "v1.9.0",
		"float_str":   "1.0",
	}

	for field, want := range stringFields {
		got, ok := specMap[field]
		if !ok {
			t.Errorf("spec missing field %q", field)
			continue
		}

		gotStr, isStr := got.(string)
		if !isStr {
			t.Errorf("spec[%q] = %v (%T), want string %q (type coercion occurred)", field, got, got, want)
			continue
		}

		if gotStr != want {
			t.Errorf("spec[%q] = %q, want %q", field, gotStr, want)
		}
	}
}
