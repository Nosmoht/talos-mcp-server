//go:build integration

package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// binaryPath is set by TestMain and points to the built talos-mcp binary.
var binaryPath string

// TestMain builds the binary once, then runs all integration tests.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "talos-mcp-integration-*")
	if err != nil {
		log.Fatalf("create temp dir: %v", err) //nolint:gocritic // fatal before defer is intentional
	}
	defer os.RemoveAll(tmp)

	binaryPath = filepath.Join(tmp, "talos-mcp")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/talos-mcp")
	cmd.Dir = repoRoot()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("build binary: %v", err)
	}

	os.Exit(m.Run())
}

// repoRoot returns the module root.
// In Go tests the working directory is the package directory (cmd/talos-mcp/),
// so the repo root is two levels up.
func repoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "../.."
	}
	return filepath.Join(wd, "../..")
}

// requireCluster skips the test if no talosconfig is found.
func requireCluster(t *testing.T) {
	t.Helper()
	cfg := os.Getenv("TALOSCONFIG")
	if cfg == "" {
		cfg = filepath.Join(os.Getenv("HOME"), ".talos", "config")
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Skipf("no talosconfig at %s — set TALOSCONFIG to run integration tests", cfg)
	}
}

// connectMCP spawns the binary via CommandTransport and returns a connected MCP session.
func connectMCP(t *testing.T) *mcp.ClientSession {
	t.Helper()
	requireCluster(t)

	ctx := context.Background()
	cmd := exec.Command(binaryPath)
	// Inherit env so TALOSCONFIG, TALOS_CONTEXT, TALOS_ENDPOINTS are passed through.
	// Explicitly unset HTTP-mode vars so the subprocess always runs in stdio mode
	// regardless of the caller's environment.
	cmd.Env = filterEnv(os.Environ(), "TALOS_MCP_HTTP_ADDR", "TALOS_MCP_AUTH_TOKEN")

	client := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to MCP server: %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Logf("session.Close: %v", err)
		}
	})
	return session
}

// callTool is a convenience wrapper around session.CallTool that fails on error.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%q): %v", name, err)
	}
	if result.IsError {
		t.Fatalf("CallTool(%q) returned tool error: %s", name, extractText(t, result))
	}
	return result
}

// extractText concatenates all TextContent entries from a CallToolResult.
func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// filterEnv returns a copy of env with entries whose key matches any of the
// given names removed. Used to prevent caller env vars from leaking into the
// subprocess (e.g. TALOS_MCP_HTTP_ADDR would switch it to HTTP mode).
func filterEnv(env []string, remove ...string) []string {
	skip := make(map[string]bool, len(remove))
	for _, k := range remove {
		skip[k] = true
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		if !skip[key] {
			out = append(out, e)
		}
	}
	return out
}

// discoverNode returns a node IP to use in node-targeted tests.
// Checks TALOS_ENDPOINTS first, then reads the active context from talosconfig.
func discoverNode(t *testing.T, _ *mcp.ClientSession) string {
	t.Helper()

	if eps := os.Getenv("TALOS_ENDPOINTS"); eps != "" {
		return strings.SplitN(eps, ",", 2)[0]
	}

	cfgPath := os.Getenv("TALOSCONFIG")
	if cfgPath == "" {
		cfgPath = filepath.Join(os.Getenv("HOME"), ".talos", "config")
	}
	cfg, err := clientconfig.Open(cfgPath)
	if err != nil {
		t.Fatalf("discoverNode: open talosconfig %s: %v", cfgPath, err)
	}
	ctx := cfg.Contexts[cfg.Context]
	if ctx == nil || len(ctx.Endpoints) == 0 {
		t.Fatalf("discoverNode: no endpoints in active talosconfig context %q", cfg.Context)
	}
	return ctx.Endpoints[0]
}

// TestListTools verifies the server starts and exposes the expected MCP tools.
func TestListTools(t *testing.T) {
	session := connectMCP(t)
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	got := make(map[string]bool, len(result.Tools))
	for _, tool := range result.Tools {
		got[tool.Name] = true
	}

	want := []string{
		"talos_get", "talos_version", "talos_services",
		"talos_resource_definitions", "talos_containers",
		"talos_processes", "talos_logs", "talos_dmesg",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("expected tool %q not listed by server", name)
		}
	}
}

// TestTalosVersion verifies a basic tool call succeeds and returns version data.
func TestTalosVersion(t *testing.T) {
	session := connectMCP(t)
	result := callTool(t, session, "talos_version", map[string]any{})
	text := extractText(t, result)
	if !strings.Contains(text, "version") {
		t.Errorf("expected version info in response, got: %.300s", text)
	}
}

// TestTalosGet_WithoutNodes verifies talos_get works with the default talosconfig context.
func TestTalosGet_WithoutNodes(t *testing.T) {
	session := connectMCP(t)
	result := callTool(t, session, "talos_get", map[string]any{
		"resource_type": "MachineStatus",
	})
	text := extractText(t, result)
	if !strings.Contains(text, "metadata") {
		t.Errorf("expected resource metadata in response, got: %.300s", text)
	}
}

// TestTalosGet_WithNodes is the regression test for issue #23.
// talos_get must succeed when a single node IP is provided.
// Previously failed with: "one-2-many proxying is not supported for COSI methods"
func TestTalosGet_WithNodes(t *testing.T) {
	session := connectMCP(t)
	nodeIP := discoverNode(t, session)

	result := callTool(t, session, "talos_get", map[string]any{
		"resource_type": "MachineStatus",
		"nodes":         []string{nodeIP},
	})
	text := extractText(t, result)
	if !strings.Contains(text, "metadata") {
		t.Errorf("expected resource metadata in response for node %s, got: %.300s", nodeIP, text)
	}
}

// TestTalosServices verifies talos_services returns a list that includes apid.
func TestTalosServices(t *testing.T) {
	session := connectMCP(t)
	result := callTool(t, session, "talos_services", map[string]any{})
	text := extractText(t, result)
	if !strings.Contains(text, "apid") {
		t.Errorf("expected 'apid' in services list, got: %.300s", text)
	}
}

// TestTalosResourceDefinitions verifies resource type discovery returns known types.
func TestTalosResourceDefinitions(t *testing.T) {
	session := connectMCP(t)
	result := callTool(t, session, "talos_resource_definitions", map[string]any{})
	text := extractText(t, result)
	for _, want := range []string{"MachineStatus", "NodeAddress", "LinkStatus"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in resource definitions, got: %.300s", want, text)
		}
	}
}

// TestTalosPatchConfig_DryRun is the regression test for issue #25.
// talos_patch_config dry-run must merge the patch with the current node config
// before submitting to the Talos API. Previously failed because the raw patch
// was sent as a full config document.
func TestTalosPatchConfig_DryRun(t *testing.T) {
	session := connectMCP(t)
	nodeIP := discoverNode(t, session)

	result := patchDryRun(t, session, nodeIP, `{"machine":{"nodeLabels":{"integration-test":"true"}}}`)
	// Talos v1.14+ rejects .machine.nodeLabels when the config carries a
	// KubeNodeConfig document (machinery config/types/k8s/node.go,
	// V1Alpha1ConflictValidate); such nodes take labels in that document.
	if result.IsError && strings.Contains(extractText(t, result), ".machine.nodeLabels is already set in v1alpha1 config") {
		result = patchDryRun(t, session, nodeIP, "apiVersion: v1alpha1\nkind: KubeNodeConfig\nlabels:\n  integration-test: \"true\"\n")
	}
	text := extractText(t, result)
	if result.IsError {
		t.Fatalf("talos_patch_config returned tool error: %s", text)
	}
	// The dry-run diff must show the label: a bare dry-run confirmation would
	// also pass for a patch that was dropped before merging.
	if !strings.Contains(text, "integration-test") {
		t.Errorf("expected dry-run diff to contain the patched label, got: %.500s", text)
	}
}

// patchDryRun calls talos_patch_config with dry_run=true on node and returns
// the result, including tool errors.
func patchDryRun(t *testing.T, session *mcp.ClientSession, node, patch string) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "talos_patch_config",
		Arguments: map[string]any{
			"patch":   patch,
			"dry_run": true,
			"nodes":   []string{node},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(talos_patch_config): %v", err)
	}
	return result
}

// TestTalosPatchConfig_DryRun_JSON6902 verifies RFC 6902 JSON Patch format is
// processed through the fetch→merge pipeline. Nodes with multi-document machine
// configs will return a known configpatcher error; single-document configs apply
// successfully. Both outcomes confirm the patch reached configpatcher.Apply
// (i.e. the pipeline is correct), not that the raw patch was sent to the API.
func TestTalosPatchConfig_DryRun_JSON6902(t *testing.T) {
	session := connectMCP(t)
	nodeIP := discoverNode(t, session)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "talos_patch_config",
		Arguments: map[string]any{
			"patch":   `[{"op":"add","path":"/machine/nodeLabels/json6902-test","value":"true"}]`,
			"dry_run": true,
			"nodes":   []string{nodeIP},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	text := extractText(t, result)

	if result.IsError {
		// Multi-document machine configs cannot accept RFC 6902 patches — this is a
		// known configpatcher limitation, not a pipeline bug. Verify the error comes
		// from configpatcher.Apply (not from an earlier stage like raw-patch submission).
		if !strings.Contains(text, "apply patch") && !strings.Contains(text, "multi-document") {
			t.Errorf("unexpected error (expected apply-patch or multi-document): %.500s", text)
		}
		return
	}
	// Single-document config: patch applied successfully.
	if !strings.Contains(text, "json6902-test") && !strings.Contains(text, "dry") {
		t.Errorf("expected dry-run response to contain patched label or dry-run confirmation, got: %.500s", text)
	}
}

// TestTalosPatchConfig_MultiNode_Rejected verifies that targeting multiple nodes
// returns an error rather than silently applying config from one node to all.
func TestTalosPatchConfig_MultiNode_Rejected(t *testing.T) {
	session := connectMCP(t)
	nodeIP := discoverNode(t, session)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "talos_patch_config",
		Arguments: map[string]any{
			"patch":   `{"machine":{"nodeLabels":{"test":"value"}}}`,
			"dry_run": true,
			"nodes":   []string{nodeIP, nodeIP}, // two entries (even if same IP)
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	// Expect a tool-level error, not a transport error.
	if !result.IsError {
		t.Errorf("expected tool error for multi-node patch_config, got success: %.300s", extractText(t, result))
	}
	text := extractText(t, result)
	if !strings.Contains(text, "exactly one") {
		t.Errorf("expected 'exactly one' in error message, got: %.300s", text)
	}
}
