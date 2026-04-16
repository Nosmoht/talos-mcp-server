package tools

import "github.com/modelcontextprotocol/go-sdk/mcp"

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
		Description:  "List all available Talos resource types with their aliases. Call this first to discover what resources can be queried with talos_get.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ResourceDefinitionsOutputSchema(),
	}, h.HandleResourceDefinitions)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_get",
		Description:  "Get Talos resources by type. Use talos_resource_definitions to discover available types. Examples: MachineStatus, Member, NodeAddress, LinkStatus, Route, Service, Extension.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: GetResourceOutputSchema(),
	}, h.HandleGetResource)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_version",
		Description:  "Get Talos version information from the target nodes.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: VersionOutputSchema(),
	}, h.HandleVersion)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_services",
		Description:  "List all Talos services and their current state (running, stopped, health status).",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ServicesOutputSchema(),
	}, h.HandleServices)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_containers",
		Description:  "List containers running in the specified namespace. Defaults to the 'k8s.io' namespace (Kubernetes containers).",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ContainersOutputSchema(),
	}, h.HandleContainers)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_processes",
		Description:  "List running processes on the target nodes.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ProcessesOutputSchema(),
	}, h.HandleProcesses)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_health",
		Description:  "Check the health of the Talos cluster (etcd, Kubernetes API, node readiness). Waits up to wait_timeout for all checks to pass.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: HealthOutputSchema(),
	}, h.HandleHealth)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_logs",
		Description:  "Stream recent service logs from the target nodes. Returns the last tail_lines lines without following.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: LogsOutputSchema(),
	}, h.HandleLogs)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_dmesg",
		Description:  "Read kernel ring buffer (dmesg) messages from the target nodes.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: DmesgOutputSchema(),
	}, h.HandleDmesg)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_events",
		Description:  "Fetch recent Talos runtime events (node lifecycle, service changes, config changes). Returns the last tail_count events.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: EventsOutputSchema(),
	}, h.HandleEvents)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_etcd",
		Description:  "Query etcd cluster information. Use subcommand='members' (default) or subcommand='status'.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: EtcdOutputSchema(),
	}, h.HandleEtcd)

	mcp.AddTool(server, &mcp.Tool{
		Name: "talos_etcd_snapshot",
		Description: "Take an etcd snapshot from a single control plane node and write it to a local file. " +
			"Returns the file path and byte count on success. " +
			"Requires exactly one control plane node in nodes[]. " +
			"Snapshot may take up to 5 minutes for large clusters.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: EtcdSnapshotOutputSchema(),
	}, h.HandleEtcdSnapshot)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_list_files",
		Description:  "List files and directories on a target node filesystem.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ListFilesOutputSchema(),
	}, h.HandleListFiles)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "talos_read_file",
		Description:  "Read the contents of a file from a target node filesystem (e.g. /etc/os-release, /etc/machine-config.yaml).",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ReadFileOutputSchema(),
	}, h.HandleReadFile)

	mcp.AddTool(server, &mcp.Tool{
		Name: "talos_validate",
		Description: "Validate a Talos machine config (YAML or JSON) offline — no cluster connection required. " +
			"Use mode='metal' (default), 'cloud', or 'container'. " +
			"Set strict=true to treat warnings as errors. " +
			"Returns {valid, mode, strict, warnings} and on failure also {errors}.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
		OutputSchema: ValidateOutputSchema(),
	}, h.HandleValidate)

	// ── Write / mutating tools ───────────────────────────────────────────────
	// Skipped when TALOS_MCP_READ_ONLY=true.

	if !readOnly {
		destructive := boolPtr(true)

		mcp.AddTool(server, &mcp.Tool{
			Name: "talos_service_action",
			Description: "Start, stop, or restart a Talos service on the target nodes. " +
				"NOTE: restarting 'etcd' is not supported by the Talos API and will return an error; " +
				"use talos_reboot or the investigate-etcd prompt to recover etcd.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleServiceAction)

		mcp.AddTool(server, &mcp.Tool{
			Name: "talos_reboot",
			Description: "Reboot the specified nodes. Requires explicit nodes and confirm=true. " +
				"All listed nodes are rebooted simultaneously — reboot one node at a time to avoid a full cluster outage. " +
				"Use mode='powercycle' for a full power cycle or mode='force' to skip graceful shutdown on stuck nodes. " +
				"Set wait=true to block until all node(s) complete reboot and are back up (verified via boot ID change). " +
				"Use timeout to control max wait time (default: '5m').",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleReboot)

		mcp.AddTool(server, &mcp.Tool{
			Name: "talos_upgrade",
			Description: "Upgrade Talos on the specified nodes. Requires explicit nodes, an installer image reference, and confirm=true. " +
				"Set preserve=true (default) to keep the EPHEMERAL partition intact. " +
				"Use stage=true to defer the upgrade to the next reboot. " +
				"Use reboot_mode='powercycle' for a full power cycle after upgrade. " +
				"Use talos_health after upgrade to verify cluster state.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleUpgrade)

		mcp.AddTool(server, &mcp.Tool{
			Name: "talos_rollback",
			Description: "Roll back the last Talos upgrade on the specified nodes, reverting to the previous boot asset. " +
				"Requires explicit nodes and confirm=true. " +
				"Only works if the previous installation is still intact (i.e. no second upgrade was performed). " +
				"Use talos_health after rollback to verify cluster state.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleRollback)

		mcp.AddTool(server, &mcp.Tool{
			Name: "talos_patch_config",
			Description: "Apply a machine config patch to the target nodes. " +
				"Defaults to dry_run=true — set dry_run=false to actually apply. " +
				"Requires confirm=true when dry_run=false. " +
				"Patch can be a JSON or YAML strategic merge patch.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandlePatchConfig)

		mcp.AddTool(server, &mcp.Tool{
			Name: "talos_reset",
			Description: "Wipe and factory-reset the specified nodes. IRREVERSIBLE: all data on the system disk is permanently destroyed. " +
				"Requires explicit nodes and confirm=true. " +
				"All listed nodes are reset simultaneously — reset one node at a time to avoid a full cluster outage. " +
				"Set graceful=false only on nodes that are already unresponsive. " +
				"Provide system_labels_to_wipe to wipe only specific partitions (e.g. ['EPHEMERAL']) instead of the full system disk. " +
				"Set reboot=true to have nodes come back up automatically after wiping.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleReset)

		mcp.AddTool(server, &mcp.Tool{
			Name: "talos_apply_config",
			Description: "Apply a complete machine config document to a single target node. " +
				"config_file must be an absolute path to a local YAML/JSON file — the server reads it " +
				"directly so secrets (CA keys, tokens, encryption keys) never enter the conversation. " +
				"Reads from the local host filesystem (not Talos nodes); TALOS_MCP_ALLOWED_PATHS does not apply. " +
				"Use this to deliver a full config (e.g. output of talosctl gen config) rather than a patch. " +
				"Defaults to dry_run=true — set dry_run=false to actually apply. " +
				"Requires confirm=true when dry_run=false. " +
				"Config must target exactly one node — each node has a unique machine config.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleApplyConfig)
	}
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}
