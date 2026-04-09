package tools

import (
	"encoding/json"
	"testing"
)

// benchPayload is a representative MCP tool response payload: a slice of
// resource-like structs with nested fields. Size is chosen to reflect a
// realistic talos_get or talos_services response.
type benchPayload struct {
	Metadata map[string]any `json:"metadata"`
	Spec     map[string]any `json:"spec"`
	Status   map[string]any `json:"status"`
}

func newBenchPayload() []benchPayload {
	item := benchPayload{
		Metadata: map[string]any{
			"namespace": "default",
			"type":      "Services.v1alpha1.talos.dev",
			"id":        "kubelet",
			"version":   "3",
		},
		Spec: map[string]any{
			"enabled":   true,
			"image":     "ghcr.io/siderolabs/talos:v1.9.0",
			"arguments": []string{"--config=/etc/talos/config", "--hostname=node-1"},
		},
		Status: map[string]any{
			"state":   "running",
			"healthy": true,
			"pid":     42,
			"uptime":  "72h13m5s",
		},
	}

	items := make([]benchPayload, 20)
	for i := range items {
		items[i] = item
	}
	return items
}

func BenchmarkJSONMarshal(b *testing.B) {
	payload := newBenchPayload()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(payload)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONMarshalIndent(b *testing.B) {
	payload := newBenchPayload()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkJSONMarshalOutputSize reports the serialized byte count difference.
// Run with -v to see the output sizes printed.
func BenchmarkJSONMarshalOutputSize(b *testing.B) {
	payload := newBenchPayload()
	compact, _ := json.Marshal(payload)
	indented, _ := json.MarshalIndent(payload, "", "  ")
	b.Logf("compact: %d bytes, indented: %d bytes, ratio: %.2fx",
		len(compact), len(indented), float64(len(indented))/float64(len(compact)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(payload)
	}
}
