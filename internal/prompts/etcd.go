package prompts

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func investigateEtcdPrompt() *mcp.Prompt {
	return &mcp.Prompt{
		Name:        "investigate-etcd",
		Description: "Investigate etcd health issues. Covers status, member list, logs, control-plane services, and dmesg.",
		Arguments: []*mcp.PromptArgument{
			{Name: "node", Description: "Control plane node IP to focus on. Omit to query all nodes in the active talosconfig context.", Required: false},
		},
	}
}

func handleInvestigateEtcd(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	node := optionalArg(req, "node", "")

	var nodeClause string
	if node != "" {
		nodeClause = fmt.Sprintf("Focusing on node: %s.", node)
	} else {
		nodeClause = "Querying all nodes in the active talosconfig context."
	}

	msg := fmt.Sprintf(`Investigate etcd health on the Talos cluster.
%s

Step 1 — Etcd status
Check etcd status using talos_etcd with subcommand="status". Look for: no leader elected (quorum loss), raftApplied far behind raftCommitted (lagging member), unusually large dbSize (compaction issues), or a non-empty errors field.

Step 2 — Etcd member list
Check etcd members using talos_etcd with subcommand="members". Verify the expected number of members are present. A missing member indicates a lost node. An "unstarted" member means etcd failed to start on that node.

Step 3 — Etcd service logs
Retrieve etcd logs using talos_logs with service_name="etcd". Look for: "raft: failed to" messages, TLS certificate errors, "context deadline exceeded", "leader changed", or "etcdserver: failed to" entries.

Step 4 — Control-plane services
List services using talos_services. Confirm etcd, kubelet, and containerd are all running. A stopped kubelet or containerd can cause cascading etcd failures.

Step 5 — Kernel messages
Read kernel messages using talos_dmesg. Look for disk I/O errors (etcd is I/O-sensitive), OOM kills, or network errors that could explain membership loss.

Summarise: state the etcd health (healthy / degraded / quorum loss), identify the root cause, and recommend a remediation. If quorum is lost, note that recovery requires etcd member removal or restore from snapshot and should not be attempted without a backup.`, nodeClause)

	return textMsg(msg), nil
}
