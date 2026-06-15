package tools

import "github.com/modelcontextprotocol/go-sdk/mcp"

// Tool descriptions are centralised in this const block (rather than inline at
// each AddTool call) so the agent-facing "which tool for what" routing contract
// is mechanically testable — register_test.go asserts that the overlap-cluster
// tools keep their disambiguation cross-references.
//
// Authoring rules (evidence: Anthropic "Writing effective tools for agents";
// arXiv:2602.14878 "MCP Tool Descriptions Are Smelly"):
//   - Purpose first — state what the tool answers before any qualifier.
//   - Add a "prefer X over Y" disambiguation clause ONLY where a sibling tool
//     overlaps (get↔services/etcd/version, logs↔dmesg↔events, patch↔apply).
//   - Add a limitation clause ONLY where it actually bites.
//   - Keep each description tight; over-long descriptions measurably hurt
//     tool selection on smaller models.
//   - Do NOT weaken the confirm / nodes / dry_run safety wording on the
//     mutating tools — that text is load-bearing, not cosmetic.
const (
	descResourceDefinitions = "List every Talos COSI resource type and its aliases. " +
		"Call this to discover the type names that talos_get accepts."

	descGet = "Read any Talos COSI resource by type — the low-level catch-all for node and cluster state. " +
		"For common needs prefer the dedicated tool: service state → talos_services; etcd membership → talos_etcd; version info → talos_version. " +
		"Use talos_get for network state (NodeAddress, AddressStatus, Route, LinkStatus), MachineStatus, Extension, and anything else. " +
		"Query ONE node at a time — passing multiple nodes is rejected for COSI resource reads (one-to-many proxying is not supported). " +
		"Call talos_resource_definitions to list every available type."

	descVersion = "Report Talos version information for the target nodes. Use this for versions rather than talos_get."

	descServices = "List Talos system services with their current state and health (running, stopped). " +
		"Use this for service status rather than talos_get type=Service."

	descContainers = "List containers running on the target nodes in a namespace (defaults to 'k8s.io', the Kubernetes workloads)."

	descProcesses = "List the host processes running on the target nodes."

	descHealth = "Check overall Talos cluster health — etcd, Kubernetes API, and node readiness — " +
		"waiting up to wait_timeout for the checks to pass. Use as a go/no-go gate before upgrades or config changes."

	descLogs = "Read recent log lines for ONE named service or container on the target nodes (service_name, e.g. kubelet, etcd, containerd). " +
		"For kernel messages use talos_dmesg; for node/service lifecycle events use talos_events."

	descDmesg = "Read the kernel ring buffer (dmesg) from the target nodes — hardware, kernel, and boot messages. " +
		"For a service's own logs use talos_logs; for runtime/lifecycle events use talos_events."

	descEvents = "List recent Talos runtime events from the target nodes (node boot/shutdown, service state changes, config changes). " +
		"For a service's full logs use talos_logs; for kernel messages use talos_dmesg."

	descEtcd = "Query etcd cluster membership and health from a control-plane node. " +
		"Use this for etcd rather than talos_get type=Member. subcommand='members' (default) or 'status'."

	descEtcdSnapshot = "Take an etcd backup snapshot from a single control-plane node and write it to a local file. " +
		"Requires exactly one control-plane node in nodes[]. " +
		"Returns the file path and byte count on success. " +
		"May take up to 5 minutes on large clusters."

	descListFiles = "List files and directories at a path on a target node's filesystem. " +
		"To read a file's contents use talos_read_file."

	descReadFile = "Read the contents of a single file from a target node's filesystem (e.g. /etc/os-release, /etc/machine-config.yaml). " +
		"To browse directories first use talos_list_files."

	descValidate = "Validate a Talos machine config (YAML or JSON) offline — no cluster connection required. " +
		"Use mode='metal' (default), 'cloud', or 'container'. " +
		"Set strict=true to treat warnings as errors. " +
		"Returns {valid, mode, strict, warnings} and on failure also {errors}."

	descServiceAction = "Start, stop, or restart a Talos service on the target nodes. " +
		"Requires confirm=true. " +
		"Without explicit nodes it targets ALL default nodes in the active talosconfig context — pass nodes to scope the action to one node. " +
		"NOTE: restarting 'etcd' is not supported by the Talos API and will return an error; " +
		"use talos_reboot or the investigate-etcd prompt to recover etcd."

	descReboot = "Reboot the specified nodes. Requires explicit nodes and confirm=true. " +
		"All listed nodes are rebooted simultaneously — reboot one node at a time to avoid a full cluster outage. " +
		"Use mode='powercycle' for a full power cycle or mode='force' to skip graceful shutdown on stuck nodes. " +
		"Set wait=true to block until all node(s) complete reboot and are back up (verified via boot ID change). " +
		"Use timeout to control max wait time (default: '5m')."

	descUpgrade = "Upgrade Talos on the specified nodes. Requires explicit nodes, an installer image reference, and confirm=true. " +
		"Set preserve=true (default) to keep the EPHEMERAL partition intact. " +
		"Use stage=true to defer the upgrade to the next reboot. " +
		"Use reboot_mode='powercycle' for a full power cycle after upgrade. " +
		"Use talos_health after upgrade to verify cluster state."

	descRollback = "Roll back the last Talos upgrade on the specified nodes, reverting to the previous boot asset. " +
		"Requires explicit nodes and confirm=true. " +
		"Only works if the previous installation is still intact (i.e. no second upgrade was performed). " +
		"Use talos_health after rollback to verify cluster state."

	descPatchConfig = "Make a TARGETED change to a node's machine config via a patch — a strategic-merge patch OR an RFC 6902 JSON Patch array. " +
		"Prefer this for edits; to replace the entire config from a file use talos_apply_config. " +
		"Defaults to dry_run=true — set dry_run=false to actually apply. " +
		"Requires confirm=true when dry_run=false. " +
		"Targets exactly one node. " +
		"Note: Talos may reject an RFC 6902 patch against a multi-document machine config — prefer a strategic-merge patch there."

	descReset = "Wipe and factory-reset the specified nodes. IRREVERSIBLE: all data on the system disk is permanently destroyed. " +
		"Requires explicit nodes and confirm=true. " +
		"All listed nodes are reset simultaneously — reset one node at a time to avoid a full cluster outage. " +
		"Set graceful=false only on nodes that are already unresponsive. " +
		"Provide system_labels_to_wipe to wipe only specific partitions (e.g. ['EPHEMERAL']) instead of the full system disk. " +
		"Set reboot=true to have nodes come back up automatically after wiping."

	descApplyConfig = "Apply a complete machine config document to a single target node. " +
		"config_file must be an absolute path to a local YAML/JSON file — the server reads it " +
		"directly so secrets (CA keys, tokens, encryption keys) never enter the conversation. " +
		"Reads from the local host filesystem (not Talos nodes); TALOS_MCP_ALLOWED_PATHS does not apply. " +
		"Use this to deliver a full config (e.g. output of talosctl gen config); for targeted edits prefer talos_patch_config. " +
		"Defaults to dry_run=true — set dry_run=false to actually apply. " +
		"Requires confirm=true when dry_run=false. " +
		"Config must target exactly one node — each node has a unique machine config. " +
		"In the authenticated path, disabled when TALOS_MCP_BLOCKED_CONFIG_PATHS is set (use talos_patch_config for targeted, blocklist-checked changes); the maintenance-mode insecure path bypasses the blocklist. " +
		"For bootstrapping a fresh node in maintenance mode, set insecure=true + endpoint=<node-IP>; " +
		"requires TALOS_MCP_ENABLE_INSECURE=true and an entry in TALOS_MCP_INSECURE_ALLOWED_NODES."

	descMeta = "Read, write, or delete META partition key/value pairs. " +
		"action ∈ {read, write, delete}. write/delete require confirm=true. " +
		"Reading is unrestricted; write/delete are restricted to meta.UserReserved1/2/3 " +
		"unless the key is enumerated in TALOS_MCP_META_PRIVILEGED_KEYS. " +
		"Supports maintenance-mode (insecure=true + endpoint) for fresh nodes — " +
		"requires TALOS_MCP_ENABLE_INSECURE=true."
)

// toolDescriptions maps every registered tool name to its description constant.
// register_test.go iterates this map to enforce the disambiguation contract, so
// it MUST stay in sync with the AddTool calls in Register — adding or removing a
// tool without updating this map fails the test.
var toolDescriptions = map[string]string{
	"talos_resource_definitions": descResourceDefinitions,
	"talos_get":                  descGet,
	"talos_version":              descVersion,
	"talos_services":             descServices,
	"talos_containers":           descContainers,
	"talos_processes":            descProcesses,
	"talos_health":               descHealth,
	"talos_logs":                 descLogs,
	"talos_dmesg":                descDmesg,
	"talos_events":               descEvents,
	"talos_etcd":                 descEtcd,
	"talos_etcd_snapshot":        descEtcdSnapshot,
	"talos_list_files":           descListFiles,
	"talos_read_file":            descReadFile,
	"talos_validate":             descValidate,
	"talos_service_action":       descServiceAction,
	"talos_reboot":               descReboot,
	"talos_upgrade":              descUpgrade,
	"talos_rollback":             descRollback,
	"talos_patch_config":         descPatchConfig,
	"talos_reset":                descReset,
	"talos_apply_config":         descApplyConfig,
	"talos_meta":                 descMeta,
}

// Register adds every talos_* tool handler provided by this package to the
// supplied MCP server. Mutating tools are skipped when readOnly is true.
//
// The server caller is still responsible for setting the server's Instructions,
// completion handlers, and subscribe/unsubscribe handlers; Register only wires
// the tool handlers.
//
// The readOnly bool signature is a P0 constraint. Phases D (cluster CA / K8s
// rotation) and E (offline PKI gen) will widen it to *config.SafetyProfile so
// AllowClusterWide and EnableGen can also gate registration. Do not depend on
// the bool shape as stable API.
func Register(server *mcp.Server, h *Handlers, readOnly bool) {
	// All tools operate on a specific configured Talos cluster (closed world).
	closedWorld := boolPtr(false)

	// ── Read-only tools ──────────────────────────────────────────────────────

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_resource_definitions",
		Description:  descResourceDefinitions,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ResourceDefinitionsOutputSchema(),
	}, h.HandleResourceDefinitions)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_get",
		Description:  descGet,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: GetResourceOutputSchema(),
	}, h.HandleGetResource)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_version",
		Description:  descVersion,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: VersionOutputSchema(),
	}, h.HandleVersion)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_services",
		Description:  descServices,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ServicesOutputSchema(),
	}, h.HandleServices)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_containers",
		Description:  descContainers,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ContainersOutputSchema(),
	}, h.HandleContainers)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_processes",
		Description:  descProcesses,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ProcessesOutputSchema(),
	}, h.HandleProcesses)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_health",
		Description:  descHealth,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: HealthOutputSchema(),
	}, h.HandleHealth)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_logs",
		Description:  descLogs,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: LogsOutputSchema(),
	}, h.HandleLogs)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_dmesg",
		Description:  descDmesg,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: DmesgOutputSchema(),
	}, h.HandleDmesg)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_events",
		Description:  descEvents,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: EventsOutputSchema(),
	}, h.HandleEvents)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_etcd",
		Description:  descEtcd,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: EtcdOutputSchema(),
	}, h.HandleEtcd)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_etcd_snapshot",
		Description:  descEtcdSnapshot,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: EtcdSnapshotOutputSchema(),
	}, h.HandleEtcdSnapshot)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_list_files",
		Description:  descListFiles,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ListFilesOutputSchema(),
	}, h.HandleListFiles)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_read_file",
		Description:  descReadFile,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ReadFileOutputSchema(),
	}, h.HandleReadFile)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_validate",
		Description:  descValidate,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ValidateOutputSchema(),
	}, h.HandleValidate)

	// ── Write / mutating tools ───────────────────────────────────────────────
	// Skipped when TALOS_MCP_READ_ONLY=true.

	if !readOnly {
		destructive := boolPtr(true)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "talos_service_action",
			Description: descServiceAction,
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleServiceAction)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "talos_reboot",
			Description: descReboot,
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleReboot)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "talos_upgrade",
			Description: descUpgrade,
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleUpgrade)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "talos_rollback",
			Description: descRollback,
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleRollback)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "talos_patch_config",
			Description: descPatchConfig,
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandlePatchConfig)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "talos_reset",
			Description: descReset,
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleReset)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "talos_apply_config",
			Description: descApplyConfig,
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleApplyConfig)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "talos_meta",
			Description: descMeta,
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleMeta)
	}
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}
