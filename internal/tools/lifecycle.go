package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
	"go.yaml.in/yaml/v4"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
	"github.com/Nosmoht/talos-mcp-server/internal/version"
)

// ServiceActionArgs defines input for talos_service_action.
type ServiceActionArgs struct {
	ServiceName string   `json:"service_name" jsonschema:"Name of the service to act on (e.g. 'kubelet'\\, 'containerd'\\, 'etcd')."`
	Action      string   `json:"action" jsonschema:"Action to perform: 'start'\\, 'stop'\\, or 'restart'."`
	Nodes       []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// HandleServiceAction implements the talos_service_action tool.
func (h *Handlers) HandleServiceAction(ctx context.Context, _ *mcp.CallToolRequest, args ServiceActionArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_service_action", args, args.Nodes)

	ctx = talos.WithNodes(ctx, args.Nodes)

	var (
		resp any
		err  error
	)

	switch args.Action {
	case "start":
		resp, err = h.Client.ServiceStart(ctx, args.ServiceName)
	case "stop":
		resp, err = h.Client.ServiceStop(ctx, args.ServiceName)
	case "restart":
		resp, err = h.Client.ServiceRestart(ctx, args.ServiceName)
	default:
		return nil, nil, fmt.Errorf("unknown action %q: must be 'start', 'stop', or 'restart'", args.Action)
	}

	if err != nil {
		h.mcpLogError("talos_service_action", err)
		return nil, nil, fmt.Errorf("service %s %q: %w", args.Action, args.ServiceName, err)
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// RebootArgs defines input for talos_reboot.
type RebootArgs struct {
	Nodes   []string `json:"nodes" jsonschema:"REQUIRED: Target node IPs or hostnames to reboot. Must be explicitly specified."`
	Mode    string   `json:"mode,omitempty" jsonschema:"Reboot mode: 'default'\\, 'powercycle'\\, or 'force' (skips graceful shutdown — kube-drain and etcd leave — for stuck nodes). Defaults to 'default'."`
	Confirm bool     `json:"confirm" jsonschema:"REQUIRED: Must be explicitly set to true to confirm the reboot operation."`
}

// HandleReboot implements the talos_reboot tool.
func (h *Handlers) HandleReboot(ctx context.Context, req *mcp.CallToolRequest, args RebootArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_reboot", args, args.Nodes)

	if !args.Confirm {
		return nil, nil, fmt.Errorf("reboot refused: confirm must be explicitly set to true")
	}

	if len(args.Nodes) == 0 {
		return nil, nil, fmt.Errorf("reboot refused: nodes must be explicitly specified")
	}

	ctx = talos.WithNodes(ctx, args.Nodes)

	var opts []talosclient.RebootMode

	switch args.Mode {
	case "powercycle":
		opts = append(opts, talosclient.WithPowerCycle)
	case "force":
		opts = append(opts, talosclient.WithForce)
	case "default", "":
		// no extra opts
	default:
		return nil, nil, fmt.Errorf("unknown mode %q: must be 'default', 'powercycle', or 'force'", args.Mode)
	}

	if err := h.Client.Reboot(ctx, opts...); err != nil {
		h.mcpLogError("talos_reboot", err)
		return nil, nil, fmt.Errorf("reboot: %w", err)
	}

	notifyProgress(ctx, req, "Reboot initiated", 1, 1)

	return textResult(fmt.Sprintf("Reboot initiated for nodes: %v", args.Nodes)), nil, nil
}

// UpgradeArgs defines input for talos_upgrade.
type UpgradeArgs struct {
	Nodes      []string `json:"nodes" jsonschema:"REQUIRED: Target node IPs or hostnames to upgrade. Upgrade one node at a time."`
	Image      string   `json:"image" jsonschema:"REQUIRED: Talos installer image reference (e.g. 'ghcr.io/siderolabs/installer:v1.12.6')."`
	Confirm    bool     `json:"confirm" jsonschema:"REQUIRED: Must be explicitly set to true to confirm the upgrade."`
	Preserve   *bool    `json:"preserve,omitempty" jsonschema:"Preserve the EPHEMERAL partition (/var — etcd data\\, kubelet state\\, containerd cache\\, CNI state\\, logs) across the upgrade. Defaults to true — differs from talosctl (which defaults to false) — to prevent accidental data loss when the field is omitted. Set to false only when you intend to wipe ephemeral data."`
	Stage      bool     `json:"stage,omitempty" jsonschema:"Stage the upgrade to be applied on next reboot instead of rebooting immediately. Defaults to false."`
	Force      bool     `json:"force,omitempty" jsonschema:"Force the upgrade bypassing pre-upgrade safety checks. Dangerous — use only when the standard upgrade path is blocked. Defaults to false."`
	RebootMode string   `json:"reboot_mode,omitempty" jsonschema:"Reboot mode after upgrade: 'default' or 'powercycle'. Defaults to 'default'."`
}

// HandleUpgrade implements the talos_upgrade tool.
func (h *Handlers) HandleUpgrade(ctx context.Context, req *mcp.CallToolRequest, args UpgradeArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_upgrade", args, args.Nodes)

	if !args.Confirm {
		return nil, nil, fmt.Errorf("upgrade refused: confirm must be explicitly set to true")
	}

	if len(args.Nodes) == 0 {
		return nil, nil, fmt.Errorf("upgrade refused: nodes must be explicitly specified")
	}

	if args.Image == "" {
		return nil, nil, fmt.Errorf("upgrade refused: image must be specified")
	}

	// Validate reboot_mode before touching the gRPC client.
	var upgradeRebootMode machineapi.UpgradeRequest_RebootMode
	switch args.RebootMode {
	case "powercycle":
		upgradeRebootMode = machineapi.UpgradeRequest_POWERCYCLE
	case "default", "":
		upgradeRebootMode = machineapi.UpgradeRequest_DEFAULT
	default:
		return nil, nil, fmt.Errorf("unknown reboot_mode %q: must be 'default' or 'powercycle'", args.RebootMode)
	}

	ctx = talos.WithNodes(ctx, args.Nodes)

	// Upgrade path validation — skipped when TALOS_MCP_SKIP_VERSION_CHECK=true.
	// Decision matrix:
	//   image tag unparseable (custom/factory/latest) → warn + proceed
	//   image parseable, node version unfetchable      → warn + proceed
	//   image parseable, node version fetched, path ok → proceed silently
	//   image parseable, node version fetched, invalid → hard error (reject)
	if os.Getenv("TALOS_MCP_SKIP_VERSION_CHECK") != "true" {
		if len(args.Nodes) > 1 {
			h.mcpLogWarn("talos_upgrade", "multiple nodes targeted; validating upgrade path against the first node only — upgrade nodes one at a time", fmt.Errorf("%d nodes", len(args.Nodes)))
		}

		targetVer, parseErr := version.ExtractFromImage(args.Image)
		if parseErr != nil {
			h.mcpLogWarn("talos_upgrade", "could not parse version from image tag, skipping upgrade path validation", parseErr)
		} else {
			currentVer, fetchErr := h.Client.GetNodeVersion(ctx, args.Nodes[0])
			if fetchErr != nil {
				h.mcpLogWarn("talos_upgrade", "could not detect current node version, skipping upgrade path validation", fetchErr)
			} else if pathErr := version.ValidateUpgradePath(*currentVer, targetVer); pathErr != nil {
				return nil, nil, fmt.Errorf("upgrade refused: %w (set TALOS_MCP_SKIP_VERSION_CHECK=true to override)", pathErr)
			}
		}
	}

	notifyProgress(ctx, req, "Initiating upgrade", 1, 2)

	upgradeOpts := []talosclient.UpgradeOption{
		talosclient.WithUpgradeImage(args.Image),
		talosclient.WithUpgradePreserve(resolvePreserve(args.Preserve)),
		talosclient.WithUpgradeStage(args.Stage),
		talosclient.WithUpgradeForce(args.Force),
		talosclient.WithUpgradeRebootMode(upgradeRebootMode),
	}

	resp, err := h.Client.UpgradeWithOptions(ctx, upgradeOpts...)
	if err != nil {
		h.mcpLogError("talos_upgrade", err)
		return nil, nil, fmt.Errorf("upgrade: %w", err)
	}

	// Invalidate the cached cluster version — the node is now on the new version.
	h.Client.InvalidateVersionCache()

	notifyProgress(ctx, req, "Upgrade complete", 2, 2)

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// RollbackArgs defines input for talos_rollback.
type RollbackArgs struct {
	Nodes   []string `json:"nodes" jsonschema:"REQUIRED: Target node IPs or hostnames to roll back. Must be explicitly specified."`
	Confirm bool     `json:"confirm" jsonschema:"REQUIRED: Must be explicitly set to true to confirm the rollback."`
}

// HandleRollback implements the talos_rollback tool.
func (h *Handlers) HandleRollback(ctx context.Context, req *mcp.CallToolRequest, args RollbackArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_rollback", args, args.Nodes)

	if !args.Confirm {
		return nil, nil, fmt.Errorf("rollback refused: confirm must be explicitly set to true")
	}

	if len(args.Nodes) == 0 {
		return nil, nil, fmt.Errorf("rollback refused: nodes must be explicitly specified")
	}

	ctx = talos.WithNodes(ctx, args.Nodes)

	notifyProgress(ctx, req, "Initiating rollback", 1, 2)

	if err := h.Client.Rollback(ctx); err != nil {
		h.mcpLogError("talos_rollback", err)
		return nil, nil, fmt.Errorf("rollback: %w", err)
	}

	// Invalidate the cached cluster version — the node is now on the previous version.
	h.Client.InvalidateVersionCache()

	notifyProgress(ctx, req, "Rollback initiated", 2, 2)

	return textResult(fmt.Sprintf("Rollback initiated for nodes: %v", args.Nodes)), nil, nil
}

// PatchConfigArgs defines input for talos_patch_config.
type PatchConfigArgs struct {
	Patch  string   `json:"patch" jsonschema:"Machine config patch as a JSON or YAML string (strategic merge patch or RFC 6902 JSON Patch array). Must target a single node — the tool fetches the current config\\, merges the patch\\, and submits the result."`
	Mode   string   `json:"mode,omitempty" jsonschema:"Apply mode: 'auto' (default)\\, 'reboot'\\, 'no_reboot'\\, 'staged'\\, or 'try'."`
	DryRun *bool    `json:"dry_run,omitempty" jsonschema:"Run in dry-run mode without applying changes. Defaults to true. Set explicitly to false to actually apply."`
	Nodes  []string `json:"nodes,omitempty" jsonschema:"Target node IP or hostname (exactly one). Omit to use the default node from talosconfig."`
}

// HandlePatchConfig implements the talos_patch_config tool.
func (h *Handlers) HandlePatchConfig(ctx context.Context, req *mcp.CallToolRequest, args PatchConfigArgs) (*mcp.CallToolResult, any, error) {
	// Log a redacted copy: the patch content may contain TLS keys, tokens, or registry passwords.
	h.auditLog("talos_patch_config", struct {
		Mode   string   `json:"mode,omitempty"`
		DryRun *bool    `json:"dry_run,omitempty"`
		Nodes  []string `json:"nodes,omitempty"`
		Patch  string   `json:"patch"`
	}{
		Mode:   args.Mode,
		DryRun: args.DryRun,
		Nodes:  args.Nodes,
		Patch:  fmt.Sprintf("<redacted, %d bytes>", len(args.Patch)),
	}, args.Nodes)

	// Require exactly one node: the fetch→merge path fetches the current config from
	// the target node. Control-plane and worker nodes have different machine configs,
	// so applying a config merged from node A to node B would be incorrect.
	// Patch each node individually when multiple nodes need updating.
	if len(args.Nodes) > 1 {
		return nil, nil, fmt.Errorf("talos_patch_config requires exactly one target node (got %d); patch each node individually to ensure correct config merge", len(args.Nodes))
	}

	ctx = talos.WithNodes(ctx, args.Nodes)

	var mode machineapi.ApplyConfigurationRequest_Mode

	switch args.Mode {
	case "reboot":
		mode = machineapi.ApplyConfigurationRequest_REBOOT
	case "no_reboot":
		mode = machineapi.ApplyConfigurationRequest_NO_REBOOT
	case "staged":
		mode = machineapi.ApplyConfigurationRequest_STAGED
	case "try":
		mode = machineapi.ApplyConfigurationRequest_TRY
	case "auto", "":
		mode = machineapi.ApplyConfigurationRequest_AUTO
	default:
		return nil, nil, fmt.Errorf("unknown mode %q: must be 'auto', 'reboot', 'no_reboot', 'staged', or 'try'", args.Mode)
	}

	dryRun := resolveDryRun(args.DryRun)

	applyMsg := "Applying configuration"
	doneMsg := "Configuration applied"

	if dryRun {
		applyMsg = "Validating configuration (dry run)"
		doneMsg = "Configuration validated (dry run)"
	}

	// Step 1: parse the patch (supports strategic merge patches and RFC 6902 JSON Patches).
	patch, err := configpatcher.LoadPatch([]byte(args.Patch))
	if err != nil {
		return nil, nil, fmt.Errorf("load patch: %w", err)
	}

	notifyProgress(ctx, req, "Fetching current machine config", 1, 3)

	// Step 2: fetch the current MachineConfig from the node via COSI.
	// talos.WithNodes already set the single-node context, so COSI.Get uses one-to-one proxying.
	mc, err := h.Client.COSI.Get(ctx, resource.NewMetadata(
		talosconfig.NamespaceName,
		talosconfig.MachineConfigType,
		talosconfig.ActiveID,
		resource.VersionUndefined,
	))
	if err != nil {
		return nil, nil, fmt.Errorf("get current machine config: %w", err)
	}

	// Step 3: extract the raw YAML body from the COSI resource envelope.
	body, err := extractMachineConfigBody(mc)
	if err != nil {
		return nil, nil, fmt.Errorf("extract machine config body: %w", err)
	}

	// Step 4: apply the patch to the current config.
	cfg, err := configpatcher.Apply(configpatcher.WithBytes(body), []configpatcher.Patch{patch})
	if err != nil {
		return nil, nil, fmt.Errorf("apply patch: %w", err)
	}

	patched, err := cfg.Bytes()
	if err != nil {
		return nil, nil, fmt.Errorf("marshal patched config: %w", err)
	}

	notifyProgress(ctx, req, applyMsg, 2, 3)

	applyReq := &machineapi.ApplyConfigurationRequest{
		Data:   patched,
		Mode:   mode,
		DryRun: dryRun,
	}

	resp, err := h.Client.ApplyConfiguration(ctx, applyReq)
	if err != nil {
		h.mcpLogError("talos_patch_config", err)
		return nil, nil, fmt.Errorf("apply configuration: %w", err)
	}

	notifyProgress(ctx, req, doneMsg, 3, 3)

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// extractMachineConfigBody extracts the raw YAML config bytes from a MachineConfig COSI resource.
// Reimplemented from talosctl/cmd/talos/patch.go — handles both annotation-based (current Talos)
// and legacy protobuf-based serialization (pre-annotation Talos versions).
func extractMachineConfigBody(mc resource.Resource) ([]byte, error) {
	if mc.Metadata().Annotations().Empty() {
		// Legacy path: Talos versions that marshaled MachineConfig spec as a YAML document
		// rather than a string. Use the protobuf path to extract the full multi-document body.
		if pb, ok := mc.(*protobuf.Resource); ok {
			p, err := pb.Marshal()
			if err != nil {
				return nil, fmt.Errorf("marshal protobuf resource: %w", err)
			}

			return []byte(p.GetSpec().GetYamlSpec()), nil
		}

		return yaml.Marshal(mc.Spec())
	}

	// Current path: spec is marshaled as a YAML string (not a YAML document).
	// Unmarshal as string first to unwrap the envelope.
	spec, err := yaml.Marshal(mc.Spec())
	if err != nil {
		return nil, err
	}

	var bodyStr string
	if err = yaml.Unmarshal(spec, &bodyStr); err != nil {
		return nil, err
	}

	return []byte(bodyStr), nil
}
