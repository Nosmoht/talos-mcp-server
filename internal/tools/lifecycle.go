package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/compatibility"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
	"go.yaml.in/yaml/v4"
	"google.golang.org/grpc/codes"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
	"github.com/Nosmoht/talos-mcp-server/internal/version"
)

const (
	defaultRebootTimeout = 5 * time.Minute
	rebootProbeInterval  = 3 * time.Second
	rebootProbeTimeout   = 5 * time.Second

	// maxConfigFileSize caps the size of a config file read by readConfigFile.
	// A machine config should never exceed 1 MiB in practice.
	maxConfigFileSize = 1 << 20 // 1 MiB
)

// ServiceActionArgs defines input for talos_service_action.
type ServiceActionArgs struct {
	ServiceName string   `json:"service_name" jsonschema:"Name of the service to act on (e.g. 'kubelet'\\, 'containerd'\\, 'etcd')."`
	Action      string   `json:"action" jsonschema:"Action to perform: 'start'\\, 'stop'\\, or 'restart'."`
	Confirm     bool     `json:"confirm" jsonschema:"REQUIRED: Must be explicitly set to true to confirm the service action."`
	Nodes       []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// HandleServiceAction implements the talos_service_action tool.
func (h *Handlers) HandleServiceAction(ctx context.Context, _ *mcp.CallToolRequest, args ServiceActionArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_service_action", args, args.Nodes)

	if !args.Confirm {
		return nil, nil, fmt.Errorf("service_action refused: confirm must be explicitly set to true")
	}

	if args.ServiceName == "" {
		return nil, nil, fmt.Errorf("service_name is required")
	}

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	var call func(context.Context) (any, error)

	switch args.Action {
	case "start":
		call = func(c context.Context) (any, error) { return h.Client.ServiceStart(c, args.ServiceName) }
	case "stop":
		call = func(c context.Context) (any, error) { return h.Client.ServiceStop(c, args.ServiceName) }
	case "restart":
		call = func(c context.Context) (any, error) { return h.Client.ServiceRestart(c, args.ServiceName) }
	default:
		return nil, nil, fmt.Errorf("unknown action %q: must be 'start', 'stop', or 'restart'", args.Action)
	}

	rs, out := talos.FanOut(ctx, targets, call)
	if _, err := out.FailClosed(); err != nil {
		h.mcpLogError("talos_service_action", err)
		return nil, nil, fmt.Errorf("service %s %q: %w", args.Action, args.ServiceName, err)
	}

	return unaryResult(rs)
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

	mode, err := parseRebootMode(args.Mode)
	if err != nil {
		return nil, nil, err
	}

	// Validate timeout early — before issuing the reboot — so a typo does not
	// trigger a reboot that we then cannot track.
	var timeout time.Duration
	if args.Wait {
		if timeout, err = parseRebootTimeout(args.Timeout); err != nil {
			return nil, nil, err
		}
	}

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	reboot := func() error {
		_, out := talos.FanOut(ctx, targets, func(c context.Context) (struct{}, error) {
			return struct{}{}, h.Client.RebootNodes(c, mode)
		})
		if _, err := out.FailClosed(); err != nil {
			h.mcpLogError("talos_reboot", err)
			return fmt.Errorf("reboot: %w", err)
		}
		return nil
	}

	if args.Wait {
		return h.rebootAndWait(ctx, req, args.Nodes, timeout, reboot)
	}

	if err := reboot(); err != nil {
		return nil, nil, err
	}

	notifyProgress(ctx, req, "Reboot initiated", 1, 1)

	return textResult(fmt.Sprintf("Reboot initiated for nodes: %v", args.Nodes)), nil, nil
}

func (h *Handlers) rebootAndWait(ctx context.Context, req *mcp.CallToolRequest, nodes []string, timeout time.Duration, reboot func() error) (*mcp.CallToolResult, any, error) {
	notifyProgress(ctx, req, "Reading pre-reboot boot IDs", 1, 4)

	// Read boot IDs before issuing the reboot so we can verify they changed.
	preBootIDs, err := h.readBootIDs(ctx, nodes)
	if err != nil {
		return nil, nil, err
	}

	notifyProgress(ctx, req, fmt.Sprintf("Rebooting %d node(s)", len(nodes)), 2, 4)

	if err := reboot(); err != nil {
		return nil, nil, err
	}

	notifyProgress(ctx, req, "Waiting for node(s) to complete reboot", 3, 4)

	results, err := h.waitForReboots(ctx, req, preBootIDs, nodes, timeout)
	if err != nil {
		return nil, nil, err
	}

	notifyProgress(ctx, req, "All node(s) rebooted successfully", 4, 4)

	return jsonResult(results)
}

func parseRebootMode(mode string) (machineapi.RebootRequest_Mode, error) {
	switch mode {
	case "powercycle":
		return machineapi.RebootRequest_POWERCYCLE, nil
	case "force":
		return machineapi.RebootRequest_FORCE, nil
	case "default", "":
		return machineapi.RebootRequest_DEFAULT, nil
	default:
		return 0, fmt.Errorf("unknown mode %q: must be 'default', 'powercycle', or 'force'", mode)
	}
}

func (h *Handlers) readBootIDs(ctx context.Context, nodes []string) (map[string]string, error) {
	ids := make(map[string]string, len(nodes))

	for _, node := range nodes {
		id, err := h.readBootID(ctx, node)
		if err != nil {
			return nil, fmt.Errorf("pre-reboot boot ID for %s: %w", node, err)
		}

		ids[node] = id
	}

	return ids, nil
}

func (h *Handlers) waitForReboots(ctx context.Context, req *mcp.CallToolRequest, preBootIDs map[string]string, nodes []string, timeout time.Duration) ([]rebootResult, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startTime := time.Now()

	type nodeOutcome struct {
		result *rebootResult
		err    error
	}

	outCh := make(chan nodeOutcome, len(nodes))

	for _, node := range nodes {
		preID := preBootIDs[node]

		go func() {
			res, err := h.waitForNodeReboot(timeoutCtx, req, node, preID, startTime, rebootProbeInterval)
			outCh <- nodeOutcome{result: res, err: err}
		}()
	}

	var (
		results []rebootResult
		errs    []string
	)

	for range nodes {
		o := <-outCh
		if o.err != nil {
			errs = append(errs, o.err.Error())
		} else {
			results = append(results, *o.result)
		}
	}

	if len(errs) == 0 {
		return results, nil
	}

	combined := strings.Join(errs, "; ")
	if len(results) > 0 {
		partial, _ := json.Marshal(results)
		return nil, fmt.Errorf("wait failed for %d/%d node(s): %s (succeeded: %s)", len(errs), len(nodes), combined, partial)
	}

	return nil, fmt.Errorf("wait failed for all nodes: %s", combined)
}

// readBootID reads /proc/sys/kernel/random/boot_id from the given node.
func (h *Handlers) readBootID(ctx context.Context, node string) (string, error) {
	// nil allowlist: nodes were already validated in HandleReboot before the reboot was issued.
	nodeCtx, err := talos.WithSingleNode(ctx, []string{node}, nil)
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

	// machined's Read returns nil once its stream context is cancelled, so a read
	// cut short by a shutdown can end without an error and with partial data.
	id := strings.TrimSpace(string(data))
	if len(id) != bootIDLength {
		return "", fmt.Errorf("read boot_id: %w: got %q", errIncompleteBootID, id)
	}

	return id, nil
}

// bootIDLength is the length of the UUID text in /proc/sys/kernel/random/boot_id.
const bootIDLength = 36

var errIncompleteBootID = errors.New("incomplete boot ID")

// waitForNodeReboot polls the node's boot ID until it differs from preBootID,
// the same post check talosctl runs after a reboot.
func (h *Handlers) waitForNodeReboot(ctx context.Context, req *mcp.CallToolRequest, node, preBootID string, startTime time.Time, interval time.Duration) (*rebootResult, error) {
	notifyProgress(ctx, req, fmt.Sprintf("Node %s: waiting for a new boot ID...", node), 0, 0)

	for {
		callCtx, cancel := context.WithTimeout(ctx, rebootProbeTimeout)
		bootID, err := h.readBootID(callCtx, node)
		cancel()

		if err == nil && bootID != preBootID {
			return &rebootResult{
				Node:      node,
				OldBootID: preBootID,
				NewBootID: bootID,
				Duration:  time.Since(startTime).Round(time.Second).String(),
				Status:    "rebooted successfully",
			}, nil
		}

		if err != nil && !isRebootTransient(err) {
			return nil, fmt.Errorf("node %s: %w", node, err)
		}

		select {
		case <-ctx.Done():
			reason := "timed out"
			if errors.Is(ctx.Err(), context.Canceled) {
				reason = "wait cancelled"
			}

			if err != nil {
				return nil, fmt.Errorf("node %s: %s waiting for the node to come back: %w", node, reason, err)
			}

			return nil, fmt.Errorf("node %s: %s, boot ID did not change (%s) — reboot may not have occurred", node, reason, preBootID)
		case <-time.After(interval):
		}
	}
}

// isRebootTransient reports whether err is one talosctl's post check retries
// (cmd/talosctl/pkg/talos/action/node.go, unexported there), a timed-out probe,
// or a boot ID read cut short by the shutdown.
func isRebootTransient(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		errors.Is(err, errIncompleteBootID) {
		return true
	}

	switch talosclient.StatusCode(err) { //nolint:exhaustive // every other code ends the wait
	case codes.Unavailable, codes.Canceled, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
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

	upgradeRebootMode, err := parseUpgradeRebootMode(args.RebootMode)
	if err != nil {
		return nil, nil, err
	}

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	if err := h.checkUpgradeVersion(ctx, args.Nodes, args.Image); err != nil {
		return nil, nil, err
	}

	notifyProgress(ctx, req, "Initiating upgrade", 1, 2)

	upgradeReq := &machineapi.UpgradeRequest{
		Image:      args.Image,
		Preserve:   resolvePreserve(args.Preserve),
		Stage:      args.Stage,
		Force:      args.Force,
		RebootMode: upgradeRebootMode,
	}

	rs, out := talos.FanOut(ctx, targets, func(c context.Context) (*machineapi.UpgradeResponse, error) {
		return h.Client.UpgradeNodes(c, upgradeReq)
	})
	if _, err := out.FailClosed(); err != nil {
		h.mcpLogError("talos_upgrade", err)
		return nil, nil, fmt.Errorf("upgrade: %w", err)
	}

	// Invalidate the cached cluster version — the node is now on the new version.
	h.Client.InvalidateVersionCache()

	notifyProgress(ctx, req, "Upgrade complete", 2, 2)

	return unaryResult(rs)
}

func parseUpgradeRebootMode(mode string) (machineapi.UpgradeRequest_RebootMode, error) {
	switch mode {
	case "powercycle":
		return machineapi.UpgradeRequest_POWERCYCLE, nil
	case "default", "":
		return machineapi.UpgradeRequest_DEFAULT, nil
	default:
		return 0, fmt.Errorf("unknown reboot_mode %q: must be 'default' or 'powercycle'", mode)
	}
}

func (h *Handlers) checkUpgradeVersion(ctx context.Context, nodes []string, image string) error {
	if h.SkipVersionCheck {
		return nil
	}

	if len(nodes) > 1 {
		h.mcpLogWarn("talos_upgrade", "multiple nodes targeted; validating upgrade path against the first node only — upgrade nodes one at a time", fmt.Errorf("%d nodes", len(nodes)))
	}

	if err := h.checkUpgradePath(ctx, nodes[0], image); err != nil {
		return fmt.Errorf("upgrade refused: %w (set TALOS_MCP_SKIP_VERSION_CHECK=true to override)", err)
	}

	return nil
}

// checkUpgradePath applies the rules the target installer enforces on the node,
// which it checks only after draining the node and stopping its services.
// An image tag without a version cannot be checked and passes with a warning.
func (h *Handlers) checkUpgradePath(ctx context.Context, node, image string) error {
	targetVer, err := version.ExtractFromImage(image)
	if err != nil {
		h.mcpLogWarn("talos_upgrade", "could not parse version from image tag, skipping upgrade path validation", err)
		return nil
	}

	currentVer, err := h.Client.GetNodeVersion(ctx, node)
	if err != nil {
		return fmt.Errorf("could not detect the current Talos version of node %s: %w", node, err)
	}

	target, err := compatibility.ParseTalosVersion(&machineapi.VersionInfo{Tag: targetVer.String()})
	if err != nil {
		return fmt.Errorf("parse target version: %w", err)
	}

	host, err := compatibility.ParseTalosVersion(&machineapi.VersionInfo{Tag: currentVer.String()})
	if err != nil {
		return fmt.Errorf("parse node version: %w", err)
	}

	return target.UpgradeableFrom(host)
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

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	notifyProgress(ctx, req, "Initiating rollback", 1, 2)

	_, out := talos.FanOut(ctx, targets, func(c context.Context) (struct{}, error) {
		return struct{}{}, h.Client.Rollback(c)
	})
	if _, err := out.FailClosed(); err != nil {
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
	Mode    string   `json:"mode,omitempty" jsonschema:"Apply mode: 'auto' (default)\\, 'no_reboot'\\, 'staged'\\, or 'try'. To restart the node after applying\\, use 'no_reboot' and then talos_reboot."`
	DryRun  *bool    `json:"dry_run,omitempty" jsonschema:"Run in dry-run mode without applying changes. Defaults to true. Set explicitly to false to actually apply."`
	Confirm bool     `json:"confirm,omitempty" jsonschema:"REQUIRED when dry_run is false: Must be explicitly set to true to confirm applying the patch. Not required for dry-run mode."`
	Nodes   []string `json:"nodes,omitempty" jsonschema:"Target node IP or hostname (exactly one). Omit to use the default node from talosconfig."`
}

// HandlePatchConfig implements the talos_patch_config tool.
func (h *Handlers) HandlePatchConfig(ctx context.Context, req *mcp.CallToolRequest, args PatchConfigArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_patch_config", redactedPatchArgs(args), args.Nodes)

	if len(args.Nodes) > 1 {
		return nil, nil, fmt.Errorf("talos_patch_config requires exactly one target node (got %d); patch each node individually to ensure correct config merge", len(args.Nodes))
	}

	if !resolveDryRun(args.DryRun) && !args.Confirm {
		return nil, nil, fmt.Errorf("patch_config refused: confirm must be explicitly set to true when dry_run is false")
	}

	if err := checkBlockedPaths([]byte(args.Patch), h.BlockedConfigPaths); err != nil {
		return nil, nil, err
	}

	ctx, err := talos.WithSingleNode(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	mode, err := talos.ParseApplyMode(args.Mode)
	if err != nil {
		return nil, nil, err
	}

	patch, err := configpatcher.LoadPatch([]byte(args.Patch))
	if err != nil {
		return nil, nil, fmt.Errorf("load patch: %w", err)
	}

	resp, err := h.writePatchedConfig(ctx, req, patch, mode, resolveDryRun(args.DryRun))
	if err != nil {
		return nil, nil, err
	}

	return jsonResult(resp)
}

func (h *Handlers) writePatchedConfig(ctx context.Context, req *mcp.CallToolRequest, patch configpatcher.Patch, mode machineapi.ApplyConfigurationRequest_Mode, dryRun bool) (*machineapi.ApplyConfigurationResponse, error) {
	// Held from the config fetch through ApplyConfiguration, dry-run included: a write in between would be silently overwritten.
	ctx, release, err := h.lockConfigWrite(ctx, "talos_patch_config")
	if err != nil {
		return nil, err
	}
	defer release()

	applyMsg, doneMsg := applyProgressMessages(dryRun, false)

	notifyProgress(ctx, req, "Fetching current machine config", 1, 3)

	patched, err := h.patchedMachineConfig(ctx, patch)
	if err != nil {
		return nil, err
	}

	notifyProgress(ctx, req, applyMsg, 2, 3)

	resp, err := h.Client.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data:   patched,
		Mode:   mode,
		DryRun: dryRun,
	})
	if err != nil {
		_, wrapped := configWriteError(ctx, "talos_patch_config", dryRun, err)
		h.mcpLogError("talos_patch_config", wrapped)
		return nil, wrapped
	}

	notifyProgress(ctx, req, doneMsg, 3, 3)

	return resp, nil
}

// redactedPatchArgs drops the patch content from the audit record: it may contain TLS keys, tokens, or registry passwords.
func redactedPatchArgs(args PatchConfigArgs) any {
	return struct {
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
	}
}

func applyProgressMessages(dryRun, maintenance bool) (apply, done string) {
	switch {
	case maintenance && dryRun:
		return "Validating configuration (maintenance mode, dry run)", "Configuration validated (maintenance mode, dry run)"
	case maintenance:
		return "Applying configuration (maintenance mode)", "Configuration applied (maintenance mode)"
	case dryRun:
		return "Validating configuration (dry run)", "Configuration validated (dry run)"
	default:
		return "Applying configuration", "Configuration applied"
	}
}

func (h *Handlers) patchedMachineConfig(ctx context.Context, patch configpatcher.Patch) ([]byte, error) {
	// COSI Get needs the one-to-one proxying that WithSingleNode sets.
	mc, err := h.Client.COSIState().Get(ctx, resource.NewMetadata(
		talosconfig.NamespaceName,
		talosconfig.MachineConfigType,
		talosconfig.ActiveID,
		resource.VersionUndefined,
	))
	if err != nil {
		err = fmt.Errorf("get current machine config: %w", err)
		h.mcpLogError("talos_patch_config", err)
		return nil, err
	}

	body, err := extractMachineConfigBody(mc)
	if err != nil {
		return nil, fmt.Errorf("extract machine config body: %w", err)
	}

	cfg, err := configpatcher.Apply(configpatcher.WithBytes(body), []configpatcher.Patch{patch})
	if err != nil {
		return nil, fmt.Errorf("apply patch: %w", err)
	}

	patched, err := cfg.Bytes()
	if err != nil {
		return nil, fmt.Errorf("marshal patched config: %w", err)
	}

	return patched, nil
}

// readConfigFile reads a machine config from a local file path.
// It validates that the path is absolute, clean (no ".." components), points to a
// regular file (symlinks are rejected), and does not exceed maxConfigFileSize.
//
// The Open+LimitReader pattern avoids a TOCTOU race between a size check and the
// actual read, and caps memory consumption regardless of actual file size.
func readConfigFile(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("config_file must be an absolute path (got %q)", path)
	}
	if cleaned := filepath.Clean(path); cleaned != path {
		return nil, fmt.Errorf("config_file must not contain .. or redundant separators (got %q)", path)
	}
	// Lstat does not follow symlinks — rejects symlink targets to prevent reading
	// arbitrary files via symlink indirection (e.g. /tmp/config.yaml -> /etc/shadow).
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("config_file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("config_file %q is not a regular file", path)
	}
	f, err := os.Open(path) //nolint:gosec // path is validated above: absolute, clean, no symlinks
	if err != nil {
		return nil, fmt.Errorf("config_file: %w", err)
	}
	defer func() { _ = f.Close() }()
	// Read at most maxConfigFileSize+1 bytes so we can detect the oversize case.
	data, err := io.ReadAll(io.LimitReader(f, maxConfigFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("config_file: %w", err)
	}
	if len(data) > maxConfigFileSize {
		return nil, fmt.Errorf("config_file %q exceeds maximum size (%d bytes)", path, maxConfigFileSize)
	}
	return data, nil
}

// ApplyConfigArgs defines input for talos_apply_config.
type ApplyConfigArgs struct {
	ConfigFile      string   `json:"config_file" jsonschema:"Absolute path to a local machine config file (YAML or JSON). Must be absolute\\, no '..' components\\, regular file (symlinks are rejected)\\, max 1 MiB. The file is read server-side — secrets never enter the conversation context."`
	Mode            string   `json:"mode,omitempty" jsonschema:"Apply mode: 'auto' (default)\\, 'no_reboot'\\, 'staged'\\, or 'try'. To restart the node after applying\\, use 'no_reboot' and then talos_reboot."`
	DryRun          *bool    `json:"dry_run,omitempty" jsonschema:"Run in dry-run mode without applying changes. Defaults to true. Set explicitly to false to actually apply."`
	Confirm         bool     `json:"confirm,omitempty" jsonschema:"REQUIRED when dry_run is false: Must be explicitly set to true to confirm applying the config. Not required for dry-run mode."`
	Nodes           []string `json:"nodes,omitempty" jsonschema:"Authenticated mode: target node IP or hostname (exactly one). Omit to use the default node from talosconfig. Mutually exclusive with endpoint."`
	Insecure        bool     `json:"insecure,omitempty" jsonschema:"Maintenance-mode insecure connection (bypasses mTLS). Requires endpoint (bare IP) and TALOS_MCP_ENABLE_INSECURE=true. The transport is TLS-encrypted; server cert verified by cert_fingerprint if provided\\, otherwise accepted without verification."`
	Endpoint        string   `json:"endpoint,omitempty" jsonschema:"Required when insecure=true: bare IPv4 or IPv6 address of the maintenance-mode node. No port\\, no scheme\\, no hostname. Ignored otherwise."`
	CertFingerprint string   `json:"cert_fingerprint,omitempty" jsonschema:"Optional SHA-256 server cert fingerprint (hex; 64 chars after stripping colons/whitespace) for TOFU pinning. Only valid when insecure=true. Recommended for non-loopback endpoints to mitigate MITM."`
}

// HandleApplyConfig implements the talos_apply_config tool.
func (h *Handlers) HandleApplyConfig(ctx context.Context, req *mcp.CallToolRequest, args ApplyConfigArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_apply_config", applyConfigAuditArgs(args), args.Nodes)

	var (
		resp    *machineapi.ApplyConfigurationResponse
		outcome string
		err     error
	)
	defer func() { h.auditOutcome("talos_apply_config", outcome, err) }()

	if err = fingerprintRequiresInsecure("talos_apply_config", args.CertFingerprint, args.Insecure); err != nil {
		outcome = OutcomeRefusedFPWithoutInsec
		return nil, nil, err
	}

	if args.Insecure {
		resp, outcome, err = h.applyConfigInsecure(ctx, req, args)
	} else {
		resp, outcome, err = h.applyConfigAuthenticated(ctx, req, args)
	}
	if err != nil {
		return nil, nil, err
	}

	return jsonResult(resp)
}

func applyConfigAuditArgs(args ApplyConfigArgs) any {
	return struct {
		ConfigFile        string   `json:"config_file"`
		Mode              string   `json:"mode,omitempty"`
		DryRun            *bool    `json:"dry_run,omitempty"`
		Confirm           bool     `json:"confirm"`
		Nodes             []string `json:"nodes,omitempty"`
		Insecure          bool     `json:"insecure,omitempty"`
		Endpoint          string   `json:"endpoint,omitempty"`
		FingerprintPinned bool     `json:"fingerprint_pinned,omitempty"`
	}{
		ConfigFile:        args.ConfigFile,
		Mode:              args.Mode,
		DryRun:            args.DryRun,
		Confirm:           args.Confirm,
		Nodes:             args.Nodes,
		Insecure:          args.Insecure,
		Endpoint:          args.Endpoint,
		FingerprintPinned: args.CertFingerprint != "",
	}
}

func (h *Handlers) applyConfigAuthenticated(ctx context.Context, req *mcp.CallToolRequest, args ApplyConfigArgs) (*machineapi.ApplyConfigurationResponse, string, error) {
	// A full config replaces every blocked path, so path checks cannot make it safe.
	if len(h.BlockedConfigPaths) > 0 {
		return nil, OutcomeRefusedArgs, fmt.Errorf("talos_apply_config is disabled while TALOS_MCP_BLOCKED_CONFIG_PATHS is set: use talos_patch_config for targeted changes that respect the blocklist")
	}

	if len(args.Nodes) > 1 {
		return nil, OutcomeRefusedArgs, fmt.Errorf("talos_apply_config requires exactly one target node (got %d); apply each node individually", len(args.Nodes))
	}

	if !resolveDryRun(args.DryRun) && !args.Confirm {
		return nil, OutcomeRefusedConfirm, fmt.Errorf("apply_config refused: confirm must be explicitly set to true when dry_run is false")
	}

	ctx, err := talos.WithSingleNode(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, OutcomeRefusedAllowlist, err
	}

	configData, err := readConfigFile(args.ConfigFile)
	if err != nil {
		return nil, OutcomeRefusedArgs, err
	}

	mode, err := talos.ParseApplyMode(args.Mode)
	if err != nil {
		return nil, OutcomeRefusedArgs, err
	}

	dryRun := resolveDryRun(args.DryRun)
	applyMsg, doneMsg := applyProgressMessages(dryRun, false)

	// Same lock as HandlePatchConfig: this apply would otherwise land between a patch's fetch and apply.
	ctx, release, err := h.lockConfigWrite(ctx, "talos_apply_config")
	if err != nil {
		return nil, OutcomeRefusedWriteBusy, err
	}
	defer release()

	notifyProgress(ctx, req, applyMsg, 1, 2)

	resp, err := h.Client.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data:   configData,
		Mode:   mode,
		DryRun: dryRun,
	})
	if err != nil {
		unknown, wrapped := configWriteError(ctx, "talos_apply_config", dryRun, err)
		h.mcpLogError("talos_apply_config", wrapped)
		if unknown {
			return nil, OutcomeUnknown, wrapped
		}
		return nil, OutcomeRPCError, wrapped
	}

	notifyProgress(ctx, req, doneMsg, 2, 2)

	return resp, OutcomeOK, nil
}

// applyConfigInsecure skips the config blocklist and the cluster allowlist; see .claude/rules/insecure-mode.md.
func (h *Handlers) applyConfigInsecure(ctx context.Context, req *mcp.CallToolRequest, args ApplyConfigArgs) (*machineapi.ApplyConfigurationResponse, string, error) {
	canonicalEndpoint, fp, gateOutcome, err := h.canonicalizeAndCheckInsecure(args.Endpoint, args.CertFingerprint, len(args.Nodes) > 0)
	if err != nil {
		return nil, gateOutcome, err
	}

	if !resolveDryRun(args.DryRun) && !args.Confirm {
		return nil, OutcomeRefusedConfirm, fmt.Errorf("apply_config refused: confirm must be explicitly set to true when dry_run is false (insecure mode)")
	}

	configData, err := readConfigFile(args.ConfigFile)
	if err != nil {
		return nil, OutcomeRefusedArgs, err
	}

	mode, err := talos.ParseApplyMode(args.Mode)
	if err != nil {
		return nil, OutcomeRefusedArgs, err
	}

	dryRun := resolveDryRun(args.DryRun)
	applyMsg, doneMsg := applyProgressMessages(dryRun, true)

	mu := h.insecureApplyMu(canonicalEndpoint)
	mu.Lock()
	defer mu.Unlock()

	notifyProgress(ctx, req, applyMsg, 1, 2)

	insecureClient, err := h.dialInsecure(ctx, canonicalEndpoint, fp)
	if err != nil {
		h.mcpLogError("talos_apply_config", err)
		return nil, OutcomeDialError, err
	}
	defer func() { _ = insecureClient.Close() }()

	resp, err := insecureClient.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data:   configData,
		Mode:   mode,
		DryRun: dryRun,
	})
	if err != nil {
		h.mcpLogError("talos_apply_config", err)
		return nil, OutcomeRPCError, fmt.Errorf("apply configuration (insecure): %w", err)
	}

	notifyProgress(ctx, req, doneMsg, 2, 2)

	return resp, OutcomeOK, nil
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
func (h *Handlers) HandleReset(ctx context.Context, req *mcp.CallToolRequest, args ResetArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_reset", args, args.Nodes)

	if !args.Confirm {
		return nil, nil, fmt.Errorf("reset refused: confirm must be explicitly set to true")
	}

	if len(args.Nodes) == 0 {
		return nil, nil, fmt.Errorf("reset refused: nodes must be explicitly specified")
	}

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
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

	resetReq := &machineapi.ResetRequest{
		Graceful:               resolveGraceful(args.Graceful),
		Reboot:                 args.Reboot,
		SystemPartitionsToWipe: partitions,
		Mode:                   machineapi.ResetRequest_SYSTEM_DISK,
	}

	notifyProgress(ctx, req, "Initiating reset", 1, 2)

	rs, out := talos.FanOut(ctx, targets, func(c context.Context) (*machineapi.ResetResponse, error) {
		return h.Client.ResetGenericWithResponse(c, resetReq)
	})
	if _, err := out.FailClosed(); err != nil {
		h.mcpLogError("talos_reset", err)
		return nil, nil, fmt.Errorf("reset: %w", err)
	}

	notifyProgress(ctx, req, "Reset initiated", 2, 2)

	return unaryResult(rs)
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
