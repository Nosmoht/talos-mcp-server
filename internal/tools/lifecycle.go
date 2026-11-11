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

	var mode machineapi.RebootRequest_Mode

	switch args.Mode {
	case "powercycle":
		mode = machineapi.RebootRequest_POWERCYCLE
	case "force":
		mode = machineapi.RebootRequest_FORCE
	case "default", "":
		mode = machineapi.RebootRequest_DEFAULT
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

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	reboot := func() error {
		_, out := talos.FanOut(ctx, targets, func(c context.Context) (struct{}, error) {
			return struct{}{}, h.Client.RebootNodes(c, mode)
		})
		_, err := out.FailClosed()

		return err
	}

	if !args.Wait {
		if err := reboot(); err != nil {
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

	if err := reboot(); err != nil {
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
			res, err := h.waitForNodeReboot(timeoutCtx, req, node, preID, startTime, rebootProbeInterval)
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

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	if !h.SkipVersionCheck {
		if len(args.Nodes) > 1 {
			h.mcpLogWarn("talos_upgrade", "multiple nodes targeted; validating upgrade path against the first node only — upgrade nodes one at a time", fmt.Errorf("%d nodes", len(args.Nodes)))
		}

		if err := h.checkUpgradePath(ctx, args.Nodes[0], args.Image); err != nil {
			return nil, nil, fmt.Errorf("upgrade refused: %w (set TALOS_MCP_SKIP_VERSION_CHECK=true to override)", err)
		}
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

	// Blocklist guard: reject patches that touch operator-restricted config paths.
	// Applied before the network round-trip so misuse is caught cheaply.
	if err := checkBlockedPaths([]byte(args.Patch), h.BlockedConfigPaths); err != nil {
		return nil, nil, err
	}

	ctx, err := talos.WithSingleNode(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	mode, err := parseApplyConfigMode(args.Mode)
	if err != nil {
		return nil, nil, err
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
	// talos.WithSingleNode already set the single-node context, so COSI.Get uses one-to-one proxying.
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
	// AUDIT-FIRST: emit the audit log BEFORE any guard runs so refused-at-gate
	// attempts still produce an audit trail. Paired with auditOutcome (deferred
	// below) so defenders can distinguish refused / success / dial-error /
	// rpc-error from the log alone.
	h.auditLog("talos_apply_config", struct {
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
	}, args.Nodes)

	var (
		outcome  = OutcomeOK
		finalErr error
	)
	defer func() { h.auditOutcome("talos_apply_config", outcome, finalErr) }()

	// Consistency check FIRST (before format / file I/O / blocklist):
	// surface fingerprint-without-insecure cleanly.
	if args.CertFingerprint != "" && !args.Insecure {
		outcome = OutcomeRefusedFPWithoutInsec
		finalErr = fmt.Errorf("apply_config refused: cert_fingerprint requires insecure=true")
		return nil, nil, finalErr
	}

	if args.Insecure {
		return h.handleApplyConfigInsecure(ctx, req, args, &outcome, &finalErr)
	}

	// ── Authenticated (mTLS) path — pre-existing behaviour ─────────────────

	// Blocklist guard: talos_apply_config replaces the entire machine config document,
	// so it cannot be safely allowed when a path blocklist is active — any blocked path
	// would be silently overwritten. Direct the caller to talos_patch_config instead,
	// which supports targeted modifications that the blocklist can inspect.
	if len(h.BlockedConfigPaths) > 0 {
		outcome = OutcomeRefusedArgs
		finalErr = fmt.Errorf("talos_apply_config is disabled while TALOS_MCP_BLOCKED_CONFIG_PATHS is set: use talos_patch_config for targeted changes that respect the blocklist")
		return nil, nil, finalErr
	}

	// Require exactly one node: each node has a unique machine config.
	// Applying the same full config to multiple nodes risks overwriting node-specific settings
	// (e.g. hostname, network interface names, node-specific certificates).
	if len(args.Nodes) > 1 {
		outcome = OutcomeRefusedArgs
		finalErr = fmt.Errorf("talos_apply_config requires exactly one target node (got %d); apply each node individually", len(args.Nodes))
		return nil, nil, finalErr
	}

	// Confirm guard: require explicit confirmation for non-dry-run applies.
	// Dry-run mode (the default) does not require confirmation.
	if !resolveDryRun(args.DryRun) && !args.Confirm {
		outcome = OutcomeRefusedConfirm
		finalErr = fmt.Errorf("apply_config refused: confirm must be explicitly set to true when dry_run is false")
		return nil, nil, finalErr
	}

	var err error
	ctx, err = talos.WithSingleNode(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		outcome = OutcomeRefusedAllowlist
		finalErr = err
		return nil, nil, finalErr
	}

	// Read config file after all guard checks pass — guards are cheap structural
	// validations; file I/O should not run until the call is authorized.
	configData, err := readConfigFile(args.ConfigFile)
	if err != nil {
		outcome = OutcomeRefusedArgs
		finalErr = err
		return nil, nil, finalErr
	}

	mode, err := parseApplyConfigMode(args.Mode)
	if err != nil {
		outcome = OutcomeRefusedArgs
		finalErr = err
		return nil, nil, finalErr
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
		Data:   configData,
		Mode:   mode,
		DryRun: dryRun,
	}

	resp, err := h.Client.ApplyConfiguration(ctx, applyReq)
	if err != nil {
		outcome = OutcomeRPCError
		finalErr = fmt.Errorf("apply configuration: %w", err)
		h.mcpLogError("talos_apply_config", err)
		return nil, nil, finalErr
	}

	notifyProgress(ctx, req, doneMsg, 2, 2)

	return jsonResult(resp)
}

// parseApplyConfigMode maps the public string mode to the protobuf enum.
// Returns an error for unknown modes; empty string defaults to AUTO.
// Mode "reboot" is refused: it is deprecated in machinery v1.14.
func parseApplyConfigMode(mode string) (machineapi.ApplyConfigurationRequest_Mode, error) {
	switch mode {
	case "reboot":
		return 0, fmt.Errorf("mode \"reboot\" is not supported: apply with mode \"no_reboot\", then reboot the node with talos_reboot")
	case "no_reboot":
		return machineapi.ApplyConfigurationRequest_NO_REBOOT, nil
	case "staged":
		return machineapi.ApplyConfigurationRequest_STAGED, nil
	case "try":
		return machineapi.ApplyConfigurationRequest_TRY, nil
	case "auto", "":
		return machineapi.ApplyConfigurationRequest_AUTO, nil
	default:
		return 0, fmt.Errorf("unknown mode %q: must be 'auto', 'no_reboot', 'staged', or 'try'", mode)
	}
}

// handleApplyConfigInsecure executes talos_apply_config against a maintenance-
// mode node via the insecure transport. The blocklist and cluster allowlist
// gates do NOT apply: maintenance-mode bootstrap necessarily replaces the
// entire config (there is no post-bootstrap state to protect) and the fresh
// node is not in TALOS_MCP_ALLOWED_NODES (which gates the configured cluster).
// The insecure allowlist (TALOS_MCP_INSECURE_ALLOWED_NODES) and bare-IP
// canonical-form check substitute for those defences.
func (h *Handlers) handleApplyConfigInsecure(ctx context.Context, req *mcp.CallToolRequest, args ApplyConfigArgs, outcome *string, finalErr *error) (*mcp.CallToolResult, any, error) {
	canonicalEndpoint, fp, gateOutcome, err := h.canonicalizeAndCheckInsecure(args.Endpoint, args.CertFingerprint, len(args.Nodes) > 0)
	if err != nil {
		*outcome = gateOutcome
		*finalErr = err
		return nil, nil, err
	}

	// Confirm-vs-dry_run mirrors the authenticated path: confirm only required
	// when dry_run=false. Maintenance-mode ApplyConfiguration honours DryRun
	// per the talosctl source (verified against apply-config.go).
	if !resolveDryRun(args.DryRun) && !args.Confirm {
		*outcome = OutcomeRefusedConfirm
		*finalErr = fmt.Errorf("apply_config refused: confirm must be explicitly set to true when dry_run is false (insecure mode)")
		return nil, nil, *finalErr
	}

	configData, err := readConfigFile(args.ConfigFile)
	if err != nil {
		*outcome = OutcomeRefusedArgs
		*finalErr = err
		return nil, nil, err
	}

	mode, err := parseApplyConfigMode(args.Mode)
	if err != nil {
		*outcome = OutcomeRefusedArgs
		*finalErr = err
		return nil, nil, err
	}

	dryRun := resolveDryRun(args.DryRun)

	// Per-endpoint lock keyed on the canonical IP — different string forms
	// (::ffff:1.2.3.4 vs 1.2.3.4) of the same physical address collapse to a
	// single key so concurrent calls actually serialise.
	mu := h.nodePatchMu("insecure:" + canonicalEndpoint)
	mu.Lock()
	defer mu.Unlock()

	applyMsg := "Applying configuration (maintenance mode)"
	doneMsg := "Configuration applied (maintenance mode)"
	if dryRun {
		applyMsg = "Validating configuration (maintenance mode, dry run)"
		doneMsg = "Configuration validated (maintenance mode, dry run)"
	}
	notifyProgress(ctx, req, applyMsg, 1, 2)

	insecureClient, err := h.dialInsecure(ctx, canonicalEndpoint, fp)
	if err != nil {
		*outcome = OutcomeDialError
		*finalErr = err
		h.mcpLogError("talos_apply_config", err)
		return nil, nil, err
	}
	defer func() { _ = insecureClient.Close() }()

	resp, err := insecureClient.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data:   configData,
		Mode:   mode,
		DryRun: dryRun,
	})
	if err != nil {
		*outcome = OutcomeRPCError
		*finalErr = fmt.Errorf("apply configuration (insecure): %w", err)
		h.mcpLogError("talos_apply_config", err)
		return nil, nil, *finalErr
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
