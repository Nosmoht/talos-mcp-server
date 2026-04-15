package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// TestOutputSchemas_AllAccessorsResolve calls every output-schema accessor
// and resolves the resulting schema. Two purposes:
//  1. Catch any mustDeriveSchema panic in CI rather than at production startup.
//  2. Ensure each schema is structurally valid per JSON Schema 2020-12.
//
// This test is the CI-enforceable half of plan step 4.2.
func TestOutputSchemas_AllAccessorsResolve(t *testing.T) {
	cases := map[string]func() *jsonschema.Schema{
		"talos_resource_definitions": ResourceDefinitionsOutputSchema,
		"talos_get":                  GetResourceOutputSchema,
		"talos_version":              VersionOutputSchema,
		"talos_services":             ServicesOutputSchema,
		"talos_containers":           ContainersOutputSchema,
		"talos_processes":            ProcessesOutputSchema,
		"talos_health":               HealthOutputSchema,
		"talos_logs":                 LogsOutputSchema,
		"talos_dmesg":                DmesgOutputSchema,
		"talos_events":               EventsOutputSchema,
		"talos_etcd":                 EtcdOutputSchema,
		"talos_etcd_snapshot":        EtcdSnapshotOutputSchema,
		"talos_list_files":           ListFilesOutputSchema,
		"talos_read_file":            ReadFileOutputSchema,
		"talos_validate":             ValidateOutputSchema,
	}

	for tool, accessor := range cases {
		t.Run(tool, func(t *testing.T) {
			s := accessor()
			if s == nil {
				t.Fatal("accessor returned nil schema")
			}
			if s.Type != "object" {
				t.Fatalf("schema type = %q, want \"object\" (MCP spec requires structuredContent to be a JSON object)", s.Type)
			}
			if _, err := s.Resolve(&jsonschema.ResolveOptions{}); err != nil {
				t.Fatalf("schema failed to resolve: %v", err)
			}
		})
	}
}

// TestOutputSchema_ValidatesPayload satisfies issue #145 acceptance criterion #3
// programmatically: marshals a fixture payload for three tools and validates it
// against the declared schema. Runs in CI — no manual Inspector session needed.
func TestOutputSchema_ValidatesPayload(t *testing.T) {
	ctx := context.Background()

	type fixture struct {
		name    string
		schema  *jsonschema.Schema
		payload any
	}

	fixtures := []fixture{
		{
			name:   "talos_validate",
			schema: ValidateOutputSchema(),
			payload: validateResult{
				Valid:    true,
				Mode:     "metal",
				Strict:   false,
				Warnings: []string{},
			},
		},
		{
			name:   "talos_health",
			schema: HealthOutputSchema(),
			payload: healthResult{
				Messages: []string{"etcd is healthy", "nodes are ready"},
			},
		},
		{
			name:   "talos_etcd_snapshot",
			schema: EtcdSnapshotOutputSchema(),
			payload: etcdSnapshotResult{
				Path:  "/tmp/etcd.db",
				Bytes: 12345,
			},
		},
	}

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			resolved, err := fx.schema.Resolve(&jsonschema.ResolveOptions{})
			if err != nil {
				t.Fatalf("resolve schema: %v", err)
			}

			raw, err := json.Marshal(fx.payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}

			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}

			if err := resolved.Validate(value); err != nil {
				t.Fatalf("payload failed schema validation: %v\npayload=%s", err, raw)
			}

			_ = ctx
		})
	}
}
