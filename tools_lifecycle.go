package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

// ServiceActionArgs defines input for talos_service_action.
type ServiceActionArgs struct {
	ServiceName string   `json:"service_name" jsonschema:"Name of the service to act on (e.g. 'kubelet'\\, 'containerd'\\, 'etcd')."`
	Action      string   `json:"action" jsonschema:"Action to perform: 'start'\\, 'stop'\\, or 'restart'."`
	Nodes       []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// handleServiceAction implements the talos_service_action tool.
func (tc *TalosClient) handleServiceAction(ctx context.Context, _ *mcp.CallToolRequest, args ServiceActionArgs) (*mcp.CallToolResult, any, error) {
	ctx = withNodes(ctx, args.Nodes)

	var (
		resp any
		err  error
	)

	switch args.Action {
	case "start":
		resp, err = tc.client.ServiceStart(ctx, args.ServiceName)
	case "stop":
		resp, err = tc.client.ServiceStop(ctx, args.ServiceName)
	case "restart":
		resp, err = tc.client.ServiceRestart(ctx, args.ServiceName)
	default:
		return nil, nil, fmt.Errorf("unknown action %q: must be 'start', 'stop', or 'restart'", args.Action)
	}

	if err != nil {
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
	Mode    string   `json:"mode,omitempty" jsonschema:"Reboot mode: 'default' or 'powercycle'. Defaults to 'default'."`
	Confirm bool     `json:"confirm" jsonschema:"REQUIRED: Must be explicitly set to true to confirm the reboot operation."`
}

// handleReboot implements the talos_reboot tool.
func (tc *TalosClient) handleReboot(ctx context.Context, _ *mcp.CallToolRequest, args RebootArgs) (*mcp.CallToolResult, any, error) {
	if !args.Confirm {
		return nil, nil, fmt.Errorf("reboot refused: confirm must be explicitly set to true")
	}

	if len(args.Nodes) == 0 {
		return nil, nil, fmt.Errorf("reboot refused: nodes must be explicitly specified")
	}

	ctx = withNodes(ctx, args.Nodes)

	var opts []talosclient.RebootMode

	switch args.Mode {
	case "powercycle":
		opts = append(opts, talosclient.WithPowerCycle)
	case "default", "":
		// no extra opts
	default:
		return nil, nil, fmt.Errorf("unknown mode %q: must be 'default' or 'powercycle'", args.Mode)
	}

	if err := tc.client.Reboot(ctx, opts...); err != nil {
		return nil, nil, fmt.Errorf("reboot: %w", err)
	}

	return textResult(fmt.Sprintf("Reboot initiated for nodes: %v", args.Nodes)), nil, nil
}

// UpgradeArgs defines input for talos_upgrade.
type UpgradeArgs struct {
	Nodes   []string `json:"nodes" jsonschema:"REQUIRED: Target node IPs or hostnames to upgrade."`
	Image   string   `json:"image" jsonschema:"REQUIRED: Talos installer image reference (e.g. 'ghcr.io/siderolabs/installer:v1.12.6')."`
	Confirm bool     `json:"confirm" jsonschema:"REQUIRED: Must be explicitly set to true to confirm the upgrade."`
}

// handleUpgrade implements the talos_upgrade tool.
func (tc *TalosClient) handleUpgrade(ctx context.Context, _ *mcp.CallToolRequest, args UpgradeArgs) (*mcp.CallToolResult, any, error) {
	if !args.Confirm {
		return nil, nil, fmt.Errorf("upgrade refused: confirm must be explicitly set to true")
	}

	if len(args.Nodes) == 0 {
		return nil, nil, fmt.Errorf("upgrade refused: nodes must be explicitly specified")
	}

	if args.Image == "" {
		return nil, nil, fmt.Errorf("upgrade refused: image must be specified")
	}

	ctx = withNodes(ctx, args.Nodes)

	resp, err := tc.client.Upgrade(ctx, args.Image, false, false)
	if err != nil {
		return nil, nil, fmt.Errorf("upgrade: %w", err)
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// PatchConfigArgs defines input for talos_patch_config.
type PatchConfigArgs struct {
	Patch  string   `json:"patch" jsonschema:"Machine config patch as a JSON or YAML string."`
	Mode   string   `json:"mode,omitempty" jsonschema:"Apply mode: 'auto' (default)\\, 'reboot'\\, 'no_reboot'\\, 'staged'\\, or 'try'."`
	DryRun *bool    `json:"dry_run,omitempty" jsonschema:"Run in dry-run mode without applying changes. Defaults to true. Set explicitly to false to actually apply."`
	Nodes  []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// handlePatchConfig implements the talos_patch_config tool.
func (tc *TalosClient) handlePatchConfig(ctx context.Context, _ *mcp.CallToolRequest, args PatchConfigArgs) (*mcp.CallToolResult, any, error) {
	ctx = withNodes(ctx, args.Nodes)

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

	req := &machineapi.ApplyConfigurationRequest{
		Data:   []byte(args.Patch),
		Mode:   mode,
		DryRun: dryRun,
	}

	resp, err := tc.client.ApplyConfiguration(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("apply configuration: %w", err)
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// resolveDryRun returns true (dry-run mode) unless v is explicitly set to false.
// A nil pointer means the caller did not provide the field, so we default to safe (dry-run).
func resolveDryRun(v *bool) bool {
	return v == nil || *v
}
