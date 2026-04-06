package prompts

import (
	"context"
	"fmt"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func debugServicePrompt() *mcp.Prompt {
	return &mcp.Prompt{
		Name:        "debug-service",
		Description: "Debug a crashing or failing Talos service. Retrieves service state, logs, events, processes, and dmesg.",
		Arguments: []*mcp.PromptArgument{
			{Name: "service", Description: "Service name to debug, e.g. kubelet, containerd, etcd.", Required: true},
			{Name: "node", Description: "Target node IP or hostname.", Required: true},
			{Name: "tail_lines", Description: "Number of log lines to retrieve. Defaults to 200.", Required: false},
		},
	}
}

func handleDebugService(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	service, err := requireArg(req, "service")
	if err != nil {
		return nil, err
	}
	node, err := requireArg(req, "node")
	if err != nil {
		return nil, err
	}

	tailLinesStr := optionalArg(req, "tail_lines", "200")
	tailLines, err := strconv.Atoi(tailLinesStr)
	if err != nil || tailLines <= 0 {
		return nil, fmt.Errorf("prompt %q: tail_lines must be a positive integer, got %q", req.Params.Name, tailLinesStr)
	}

	msg := fmt.Sprintf(`Debug the "%s" service on Talos node %s.

Step 1 — Service state
List all services using talos_services targeting this node. Find the "%s" entry and record its current state (running/stopped/failed), health status, and restart count.

Step 2 — Service logs
Retrieve the last %d lines of logs for "%s" using talos_logs. Scan for: fatal or panic-level messages, "permission denied" or "no such file or directory" (misconfiguration), OOM kill markers ("signal: killed"), TLS or certificate errors, and "failed to connect" or "connection refused" (dependency unavailable).

Step 3 — Runtime events
Check recent runtime events using talos_events targeting this node. Look for events related to "%s" — service restart cycles, lifecycle changes, and associated error messages.

Step 4 — Running processes
List running processes using talos_processes targeting this node. Check whether the %s process appears in the list. If absent despite the service showing as started, the process has crashed. Note CPU and memory usage if the process is present.

Step 5 — Kernel messages
Read kernel messages using talos_dmesg targeting this node. Look for OOM kills referencing %s, disk errors if the service is storage-related, or capability denial messages.

Report your findings: state whether the service is crashing (and why), mis-configured, blocked by a missing dependency, or resource-starved. Recommend a specific remediation action.`,
		service, node,
		service,
		tailLines, service,
		service,
		service,
		service)

	return textMsg(msg), nil
}
