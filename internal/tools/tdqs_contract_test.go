package tools

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// Read-only mode must not expose tools that modify either the Talos cluster
// or the filesystem of the MCP server host.
func TestTDQS_ReadOnlyRegistration(t *testing.T) {
	full := listToolsWithMode(t, false)
	ro := listToolsWithMode(t, true)

	for name, tool := range ro {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s exposed in read-only mode without readOnlyHint=true", name)
		}
	}
	for _, name := range []string{"talos_etcd_snapshot", "talos_meta"} {
		if _, ok := ro[name]; ok {
			t.Errorf("%s must not be exposed in read-only mode", name)
		}
		if _, ok := full[name]; !ok {
			t.Errorf("%s missing in read/write mode", name)
		}
	}

	snapshot := full["talos_etcd_snapshot"]
	if snapshot == nil || snapshot.Annotations == nil {
		t.Fatal("snapshot tool or annotations missing")
	}
	if snapshot.Annotations.ReadOnlyHint {
		t.Error("snapshot creates a file on the MCP server: readOnlyHint must be false")
	}
	if snapshot.Annotations.DestructiveHint == nil || *snapshot.Annotations.DestructiveHint {
		t.Error("snapshot refuses existing files: destructiveHint must explicitly be false")
	}
	if snapshot.OutputSchema == nil {
		t.Error("snapshot must retain its output schema after reclassification")
	}
}

func TestTDQS_MetaReadHasNoMutationInputs(t *testing.T) {
	for _, readonly := range []bool{false, true} {
		tools := listToolsWithMode(t, readonly)
		metaRead := tools["talos_meta_read"]
		if metaRead == nil {
			t.Fatalf("talos_meta_read must be registered in readOnly=%t mode", readonly)
		}
		if metaRead.Annotations == nil || !metaRead.Annotations.ReadOnlyHint {
			t.Error("META read tool must advertise readOnlyHint=true")
		}
		if metaRead.OutputSchema == nil {
			t.Error("META read tool must publish an output schema")
		}
		for _, property := range []string{"action", "value", "confirm"} {
			if schemaHasProperty(t, metaRead.InputSchema, property) {
				t.Errorf("META read schema exposes mutating property %q", property)
			}
		}
		if !schemaHasProperty(t, metaRead.InputSchema, "key") || !slices.Contains(schemaRequired(t, metaRead.InputSchema), "key") {
			t.Error("META read input schema must require a META key")
		}
	}

	// The wrapper must use the existing gated read path, including transport
	// and validation checks, rather than implementing an unguarded shortcut.
	h := safeH()
	for _, tt := range []struct {
		name string
		args MetaReadArgs
		want string
	}{
		{"invalid key", MetaReadArgs{Key: "invalid"}, "invalid syntax"},
		{"insecure without opt-in", MetaReadArgs{Key: "12", Insecure: true, Endpoint: "192.0.2.5"}, "TALOS_MCP_ENABLE_INSECURE"},
		{"fingerprint without insecure", MetaReadArgs{Key: "12", CertFingerprint: strings.Repeat("a", 64)}, "cert_fingerprint requires insecure=true"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := h.HandleMetaRead(context.Background(), nil, tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestTDQS_EtcdSubcommandIsOptional(t *testing.T) {
	tool := listToolsWithMode(t, true)["talos_etcd"]
	if tool == nil {
		t.Fatal("missing talos_etcd")
	}
	if slices.Contains(schemaRequired(t, tool.InputSchema), "subcommand") {
		t.Error("subcommand defaults to members but is required by input schema")
	}
}
