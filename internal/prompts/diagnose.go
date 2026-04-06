package prompts

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func diagnoseNodePrompt() *mcp.Prompt {
	return &mcp.Prompt{
		Name:        "diagnose-node",
		Description: "Diagnose why a Talos node is unhealthy. Guides a systematic investigation: services → logs → events → MachineStatus → dmesg.",
		Arguments: []*mcp.PromptArgument{
			{Name: "node", Description: "IP address or hostname of the node to diagnose.", Required: true},
		},
	}
}

func handleDiagnoseNode(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	node, err := requireArg(req, "node")
	if err != nil {
		return nil, err
	}

	msg := fmt.Sprintf(`Diagnose the Talos node at %s.

Work through these steps in order. Stop and report your findings as soon as you find a clear root cause.

Step 1 — Service health
Start by listing all services on the node using talos_services. Look for any service not in a running state or marked unhealthy. Note every failing service name.

Step 2 — Logs from failing services
For each failing service identified in Step 1, retrieve recent logs using talos_logs. Look for crash reasons, error messages, or stack traces that explain why the service is not running.

Step 3 — Runtime events
Check recent runtime events using talos_events. Look for lifecycle changes, config errors, or repeated service restart cycles.

Step 4 — Machine status
Get the MachineStatus resource using talos_get with resource_type="MachineStatus". Check the node stage, ready condition, and any unmet conditions listed.

Step 5 — Kernel messages
Read kernel ring buffer messages using talos_dmesg. Look for hardware errors, OOM kills, disk I/O errors, kernel panics, or network driver failures.

Summarise your findings: state the likely root cause, which step revealed it, and recommend a specific remediation action.`, node)

	return textMsg(msg), nil
}
