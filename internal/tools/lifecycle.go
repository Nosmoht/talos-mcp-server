package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

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

const (
	defaultRebootTimeout  = 5 * time.Minute
	rebootProbeInterval   = 2 * time.Second
	comebackProbeInterval = 3 * time.Second
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

	if args.ServiceName == "" {
		return nil, nil, fmt.Errorf("service_name is required")
	}

	var (
		resp any
		err  error
	)

	ctx, err = talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

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

	return jsonResult(resp)
}

// RebootArgs defines input for talos_reboot.
type RebootArgs struct {
	Nodes   []string `json:"nodes" jsonschema:"REQUIRED: Target node IPs or hostnames to reboot. Must be explicitly specified. All listed nodes are rebooted simultaneously — reboot one node at a time to avoid a full cluster outage."`
	Mode    string   `json:"mode,omitempty" jsonschema:"Reboot mode: 'default'\\, 'powercycle'\\, or 'force' (skips graceful shutdown — kube-drain and etcd leave — for stuck nodes). Defaults to 'default'."`
	Confirm bool     `json:"confirm" jsonschema:"REQUIRED: Must be explicitly set to true to confirm the reboot operation."`
	Wait    bool     `json:"wait,omitempty" jsonschema:"Block until all node(s) complete the reboot and are back up (verified via boot ID change). Default: false (fire-and-forget)."`
	Timeout string   `json:"timeout,omitempty" jsonschema:"Maximum time to wait for reboot completion (e.g. '5m'\\, '10m'). Only used when wait=true. Default: '5m'. Powercycle reboots on bare-metal hardware may take longer than the default — use '10m' or more when mode='powercycle'."`
}

// rebootResult holds the per-node outcome when wait=true.
type rebootResult struct {
	Node      string `json:"node"`
	OldBootID string `json:"old_boot_id"`
	NewBootID string `json:"new_boot_id"`
	Duration  string `json:"duration"`
	Status    string `json:"status"`
}

// parseRebootTimeout parses a duration string for reboot wait timeout.
// Returns the default of 5 minutes when s is empty.
func parseRebootTimeout(s string) (time.Duration, error) {
	if s == "" {
		return defaultRebootTimeout, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout %q: %w", s, err)
	}

	return d, nil
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

	// Validate timeout early — before issuing the reboot — so a typo does not
	// trigger a reboot that we then cannot track.
	if args.Wait {
		if _, err := parseRebootTimeout(args.Timeout); err != nil {
			return nil, nil, err
		}
	}

	nodeCtx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	if !args.Wait {
		if err := h.Client.Reboot(nodeCtx, opts...); err != nil {
			h.mcpLogError("talos_reboot", err)
			return nil, nil, fmt.Errorf("reboot: %w", err)
		}

		notifyProgress(ctx, req, "Reboot initiated", 1, 1)

		return textResult(fmt.Sprintf("Reboot initiated for nodes: %v", args.Nodes)), nil, nil
	}

	// wait=true path -----------------------------------------------------------

	timeout, _ := parseRebootTimeout(args.Timeout) // already validated above

	notifyProgress(ctx, req, "Reading pre-reboot boot IDs", 1, 4)

	// Read boot IDs before issuing the reboot so we can verify they changed.
	preBootIDs := make(map[string]string, len(args.Nodes))

	for _, node := range args.Nodes {
		id, err := h.readBootID(ctx, node)
		if err != nil {
			return nil, nil, fmt.Errorf("pre-reboot boot ID for %s: %w", node, err)
		}

		preBootIDs[node] = id
	}

	notifyProgress(ctx, req, fmt.Sprintf("Rebooting %d node(s)", len(args.Nodes)), 2, 4)

	if err := h.Client.Reboot(nodeCtx, opts...); err != nil {
		h.mcpLogError("talos_reboot", err)
		return nil, nil, fmt.Errorf("reboot: %w", err)
	}

	notifyProgress(ctx, req, "Waiting for node(s) to complete reboot", 3, 4)

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startTime := time.Now()

	type nodeOutcome struct {
		result *rebootResult
		err    error
	}

	outCh := make(chan nodeOutcome, len(args.Nodes))

	for _, node := range args.Nodes {
		node := node
		preID := preBootIDs[node]

		go func() {
			res, err := h.waitForNodeReboot(timeoutCtx, req, node, preID, startTime)
			outCh <- nodeOutcome{result: res, err: err}
		}()
	}

	var (
		results []rebootResult
		errs    []string
	)

	for range args.Nodes {
		o := <-outCh
		if o.err != nil {
			errs = append(errs, o.err.Error())
		} else {
			results = append(results, *o.result)
		}
	}

	if len(errs) > 0 {
		combined := strings.Join(errs, "; ")
		if len(results) > 0 {
			// Some nodes succeeded — include partial results in the error message.
			partial, _ := json.Marshal(results)
			return nil, nil, fmt.Errorf("wait failed for %d/%d node(s): %s (succeeded: %s)", len(errs), len(args.Nodes), combined, partial)
		}

		return nil, nil, fmt.Errorf("wait failed for all nodes: %s", combined)
	}

	notifyProgress(ctx, req, "All node(s) rebooted successfully", 4, 4)

	return jsonResult(results)
}

// readBootID reads /proc/sys/kernel/random/boot_id from the given node.
func (h *Handlers) readBootID(ctx context.Context, node string) (string, error) {
	// nil allowlist: nodes were already validated in HandleReboot before the reboot was issued.
	nodeCtx, err := talos.WithNodes(ctx, []string{node}, nil)
	if err != nil {
		return "", err
	}

	r, err := h.Client.Read(nodeCtx, "/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("read boot_id: %w", err)
	}

	defer r.Close() //nolint:errcheck

	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read boot_id content: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}

// waitForNodeReboot polls until the given node has completed its reboot.
// It returns an error if the context deadline is exceeded or if the boot ID
// did not change (indicating no actual reboot occurred).
func (h *Handlers) waitForNodeReboot(ctx context.Context, req *mcp.CallToolRequest, node, preBootID string, startTime time.Time) (*rebootResult, error) {
	// Phase 1: wait for the node to become unreachable (reboot starting).
	notifyProgress(ctx, req, fmt.Sprintf("Node %s: waiting for reboot to start...", node), 0, 0)

	for {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, vErr := h.Client.GetNodeVersion(callCtx, node)
		cancel()

		if vErr != nil {
			// Node is unreachable — reboot has started.
			break
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for node to go down: %w", ctx.Err())
		case <-time.After(rebootProbeInterval):
		}
	}

	// Phase 2: wait for the node to become reachable again (reboot complete).
	notifyProgress(ctx, req, fmt.Sprintf("Node %s: rebooting, waiting for node to come back...", node), 0, 0)

	for {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, vErr := h.Client.GetNodeVersion(callCtx, node)
		cancel()

		if vErr == nil {
			break
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for node to come back: %w", ctx.Err())
		case <-time.After(comebackProbeInterval):
		}
	}

	// Phase 3: verify boot ID changed to confirm an actual reboot occurred.
	newBootID, err := h.readBootID(ctx, node)
	if err != nil {
		// Boot ID read failure after a successful Version() call is unexpected but
		// not fatal — report it in the result rather than failing the whole wait.
		return &rebootResult{
			Node:      node,
			OldBootID: preBootID,
			NewBootID: "<unreadable: " + err.Error() + ">",
			Duration:  time.Since(startTime).Round(time.Second).String(),
			Status:    "rebooted (boot ID verification failed)",
		}, nil
	}

	if newBootID == preBootID {
		return nil, fmt.Errorf("node responded but boot ID did not change (%s) — reboot may not have occurred", preBootID)
	}

	return &rebootResult{
		Node:      node,
		OldBootID: preBootID,
		NewBootID: newBootID,
		Duration:  time.Since(startTime).Round(time.Second).String(),
		Status:    "rebooted successfully",
	}, nil
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

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	// Upgrade path validation — skipped when TALOS_MCP_SKIP_VERSION_CHECK=true.
	// Decision matrix:
	//   image tag unparseable (custom/factory/latest) → warn + proceed
	//   image parseable, node version unfetchable      → warn + proceed
	//   image parseable, node version fetched, path ok → proceed silently
	//   image parseable, node version fetched, invalid → hard error (reject)
	if !h.SkipVersionCheck {
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

	return jsonResult(resp)
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

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

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
	Patch   string   `json:"patch" jsonschema:"Machine config patch as a JSON or YAML string (strategic merge patch or RFC 6902 JSON Patch array). Must target a single node — the tool fetches the current config\\, merges the patch\\, and submits the result."`
	Mode    string   `json:"mode,omitempty" jsonschema:"Apply mode: 'auto' (default)\\, 'reboot'\\, 'no_reboot'\\, 'staged'\\, or 'try'."`
	DryRun  *bool    `json:"dry_run,omitempty" jsonschema:"Run in dry-run mode without applying changes. Defaults to true. Set explicitly to false to actually apply."`
	Confirm bool     `json:"confirm" jsonschema:"REQUIRED when dry_run is false: Must be explicitly set to true to confirm applying the patch. Not required for dry-run mode."`
	Nodes   []string `json:"nodes,omitempty" jsonschema:"Target node IP or hostname (exactly one). Omit to use the default node from talosconfig."`
}

// HandlePatchConfig implements the talos_patch_config tool.
func (h *Handlers) HandlePatchConfig(ctx context.Context, req *mcp.CallToolRequest, args PatchConfigArgs) (*mcp.CallToolResult, any, error) {
	// Log a redacted copy: the patch content may contain TLS keys, tokens, or registry passwords.
	h.auditLog("talos_patch_config", struct {
		Mode    string   `json:"mode,omitempty"`
		DryRun  *bool    `json:"dry_run,omitempty"`
		Confirm bool     `json:"confirm"`
		Nodes   []string `json:"nodes,omitempty"`
		Patch   string   `json:"patch"`
	}{
		Mode:    args.Mode,
		DryRun:  args.DryRun,
		Confirm: args.Confirm,
		Nodes:   args.Nodes,
		Patch:   fmt.Sprintf("<redacted, %d bytes>", len(args.Patch)),
	}, args.Nodes)

	// Require exactly one node: the fetch→merge path fetches the current config from
	// the target node. Control-plane and worker nodes have different machine configs,
	// so applying a config merged from node A to node B would be incorrect.
	// Patch each node individually when multiple nodes need updating.
	if len(args.Nodes) > 1 {
		return nil, nil, fmt.Errorf("talos_patch_config requires exactly one target node (got %d); patch each node individually to ensure correct config merge", len(args.Nodes))
	}

	// Confirm guard: require explicit confirmation for non-dry-run patches.
	// Dry-run mode (the default) does not require confirmation.
	if !resolveDryRun(args.DryRun) && !args.Confirm {
		return nil, nil, fmt.Errorf("patch_config refused: confirm must be explicitly set to true when dry_run is false")
	}

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

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

	// Acquire a per-node lock before the fetch→merge→apply sequence to prevent a
	// read-modify-write race in HTTP multi-session mode. Without this, two concurrent
	// sessions targeting the same node would both fetch the current config (v1), each
	// merge their own patch, and the second apply would silently overwrite the first.
	// The lock is held for the full duration of the operation and released on return.
	// In dry-run mode the lock is still acquired: dry-run still reads live config and
	// serialising it avoids confusing interleaved progress notifications.
	nodeID := "<default>"
	if len(args.Nodes) > 0 {
		nodeID = args.Nodes[0]
	}

	mu := h.nodePatchMu(nodeID)
	mu.Lock()
	defer mu.Unlock()

	notifyProgress(ctx, req, "Fetching current machine config", 1, 3)

	// Step 2: fetch the current MachineConfig from the node via COSI.
	// talos.WithNodes already set the single-node context, so COSI.Get uses one-to-one proxying.
	mc, err := h.Client.COSIState().Get(ctx, resource.NewMetadata(
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

	return jsonResult(resp)
}

// ApplyConfigArgs defines input for talos_apply_config.
type ApplyConfigArgs struct {
	Config  string   `json:"config" jsonschema:"Full machine config content to apply (YAML or JSON). Must target a single node — each node has a unique machine config. May contain TLS keys or secrets; content is redacted in audit logs."`
	Mode    string   `json:"mode,omitempty" jsonschema:"Apply mode: 'auto' (default)\\, 'reboot'\\, 'no_reboot'\\, 'staged'\\, or 'try'."`
	DryRun  *bool    `json:"dry_run,omitempty" jsonschema:"Run in dry-run mode without applying changes. Defaults to true. Set explicitly to false to actually apply."`
	Confirm bool     `json:"confirm" jsonschema:"REQUIRED when dry_run is false: Must be explicitly set to true to confirm applying the config. Not required for dry-run mode."`
	Nodes   []string `json:"nodes,omitempty" jsonschema:"Target node IP or hostname (exactly one). Omit to use the default node from talosconfig."`
}

// HandleApplyConfig implements the talos_apply_config tool.
func (h *Handlers) HandleApplyConfig(ctx context.Context, req *mcp.CallToolRequest, args ApplyConfigArgs) (*mcp.CallToolResult, any, error) {
	// Log a redacted copy: the config content may contain TLS keys, tokens, or registry passwords.
	h.auditLog("talos_apply_config", struct {
		Mode    string   `json:"mode,omitempty"`
		DryRun  *bool    `json:"dry_run,omitempty"`
		Confirm bool     `json:"confirm"`
		Nodes   []string `json:"nodes,omitempty"`
		Config  string   `json:"config"`
	}{
		Mode:    args.Mode,
		DryRun:  args.DryRun,
		Confirm: args.Confirm,
		Nodes:   args.Nodes,
		Config:  fmt.Sprintf("<redacted, %d bytes>", len(args.Config)),
	}, args.Nodes)

	if args.Config == "" {
		return nil, nil, fmt.Errorf("talos_apply_config requires a non-empty config")
	}

	// Require exactly one node: each node has a unique machine config.
	// Applying the same full config to multiple nodes risks overwriting node-specific settings
	// (e.g. hostname, network interface names, node-specific certificates).
	if len(args.Nodes) > 1 {
		return nil, nil, fmt.Errorf("talos_apply_config requires exactly one target node (got %d); apply each node individually", len(args.Nodes))
	}

	// Confirm guard: require explicit confirmation for non-dry-run applies.
	// Dry-run mode (the default) does not require confirmation.
	if !resolveDryRun(args.DryRun) && !args.Confirm {
		return nil, nil, fmt.Errorf("apply_config refused: confirm must be explicitly set to true when dry_run is false")
	}

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

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

	// Acquire the same per-node lock used by HandlePatchConfig to prevent a
	// PatchConfig-vs-ApplyConfig interleaving race: a concurrent PatchConfig
	// could fetch the current config, then ApplyConfig replaces it, and
	// PatchConfig would apply its patch against the now-stale base.
	nodeID := "<default>"
	if len(args.Nodes) > 0 {
		nodeID = args.Nodes[0]
	}

	mu := h.nodePatchMu(nodeID)
	mu.Lock()
	defer mu.Unlock()

	applyMsg := "Applying configuration"
	doneMsg := "Configuration applied"

	if dryRun {
		applyMsg = "Validating configuration (dry run)"
		doneMsg = "Configuration validated (dry run)"
	}

	notifyProgress(ctx, req, applyMsg, 1, 2)

	applyReq := &machineapi.ApplyConfigurationRequest{
		Data:   []byte(args.Config),
		Mode:   mode,
		DryRun: dryRun,
	}

	resp, err := h.Client.ApplyConfiguration(ctx, applyReq)
	if err != nil {
		h.mcpLogError("talos_apply_config", err)
		return nil, nil, fmt.Errorf("apply configuration: %w", err)
	}

	notifyProgress(ctx, req, doneMsg, 2, 2)

	return jsonResult(resp)
}

// ResetArgs defines input for talos_reset.
type ResetArgs struct {
	Nodes              []string `json:"nodes" jsonschema:"REQUIRED: Target node IPs or hostnames to reset. Must be explicitly specified. All listed nodes are reset simultaneously — reset one node at a time to avoid a full cluster outage."`
	Confirm            bool     `json:"confirm" jsonschema:"REQUIRED: Must be explicitly set to true to confirm the destructive reset operation."`
	Graceful           *bool    `json:"graceful,omitempty" jsonschema:"Stop services gracefully before wiping (kube-drain\\, etcd leave). Defaults to true. Set to false only on nodes that are already unresponsive."`
	Reboot             bool     `json:"reboot,omitempty" jsonschema:"Reboot the node after the reset. Defaults to false (node powers off after wiping). Set to true to have the node come back up immediately after reset."`
	SystemLabelsToWipe []string `json:"system_labels_to_wipe,omitempty" jsonschema:"Partition labels to wipe on the system disk (e.g. 'EPHEMERAL'\\, 'STATE'). If empty\\, all system disk partitions are wiped (full factory reset)."`
}

// HandleReset implements the talos_reset tool.
func (h *Handlers) HandleReset(ctx context.Context, _ *mcp.CallToolRequest, args ResetArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_reset", args, args.Nodes)

	if !args.Confirm {
		return nil, nil, fmt.Errorf("reset refused: confirm must be explicitly set to true")
	}

	if len(args.Nodes) == 0 {
		return nil, nil, fmt.Errorf("reset refused: nodes must be explicitly specified")
	}

	nodeCtx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	var partitions []*machineapi.ResetPartitionSpec

	for _, label := range args.SystemLabelsToWipe {
		partitions = append(partitions, &machineapi.ResetPartitionSpec{
			Label: label,
			Wipe:  true,
		})
	}

	req := &machineapi.ResetRequest{
		Graceful:               resolveGraceful(args.Graceful),
		Reboot:                 args.Reboot,
		SystemPartitionsToWipe: partitions,
		Mode:                   machineapi.ResetRequest_SYSTEM_DISK,
	}

	resp, err := h.Client.ResetGenericWithResponse(nodeCtx, req)
	if err != nil {
		h.mcpLogError("talos_reset", err)
		return nil, nil, fmt.Errorf("reset: %w", err)
	}

	return jsonResult(resp)
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
