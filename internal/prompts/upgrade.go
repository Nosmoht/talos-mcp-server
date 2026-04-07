package prompts

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func preUpgradePrompt() *mcp.Prompt {
	return &mcp.Prompt{
		Name:        "pre-upgrade-checklist",
		Description: "Verify the cluster is ready to upgrade to a target Talos version. Checks health, etcd, node status, services, and running containers.",
		Arguments: []*mcp.PromptArgument{
			{Name: "target_version", Description: "Target Talos version, e.g. v1.9.0.", Required: true},
			{Name: "nodes", Description: "Comma-separated node IPs to include in checks. Omit to check all nodes in the active talosconfig context.", Required: false},
		},
	}
}

func handlePreUpgrade(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	targetVersion, err := requireArg(req, "target_version")
	if err != nil {
		return nil, err
	}

	nodesRaw := optionalArg(req, "nodes", "")
	var nodeClause string
	var nodeList []string
	if nodesRaw != "" {
		for _, n := range strings.Split(nodesRaw, ",") {
			if t := strings.TrimSpace(n); t != "" {
				nodeList = append(nodeList, t)
			}
		}
	}
	if len(nodeList) > 0 {
		nodeClause = fmt.Sprintf("Targeting nodes: %s.", strings.Join(nodeList, ", "))
	} else {
		nodeClause = "Targeting all nodes in the active talosconfig context."
	}

	msg := fmt.Sprintf(`Pre-upgrade checklist for Talos %s.
%s

Work through each gate in order. If any gate fails, stop and report — do not proceed to later gates. A failed gate means the cluster is NOT ready to upgrade.

Gate 0 — Version compatibility check
Fetch the current Talos version from any node using talos_version. Compare with the target version %s.
Talos supports upgrades of at most one minor version at a time (e.g. v1.11.x → v1.12.x).
If the target skips a minor version or is a downgrade, stop and report — the upgrade path is not supported.
Confirm the current version and validate the upgrade path before proceeding to Gate 1.

Gate 1 — Cluster health
Check overall cluster health using talos_health. All checks must pass: etcd healthy, Kubernetes API reachable, all nodes ready. A timeout or any failure here is a hard blocker.

Gate 2 — Etcd members
Retrieve etcd member list using talos_etcd with subcommand="members". Confirm all expected control plane members are present and none show as unstarted or failed.

Gate 3 — Etcd status
Retrieve etcd status using talos_etcd with subcommand="status". Confirm a leader is elected, raftApplied and raftCommitted are in sync, and the errors field is empty.

Gate 4 — Machine status on all nodes
Get MachineStatus resources using talos_get with resource_type="MachineStatus". Every node should be in stage="Running" with ready=true and no unmet conditions.

Gate 5 — Services on all nodes
List services using talos_services. Confirm kubelet, containerd, and etcd (on control plane nodes) are all running and healthy. Flag any service in a non-running state.

Gate 6 — Running containers
Check containers using talos_containers with namespace="k8s.io". Confirm critical system pods (kube-apiserver, kube-controller-manager, kube-scheduler, coredns) have running containers.

Final report: state whether the cluster PASSES or FAILS the pre-upgrade checklist. List any failed gates with details. If all gates pass, confirm it is safe to proceed with upgrading one node at a time using talos_upgrade (starting with workers, then control plane nodes) targeting Talos %s.

When invoking talos_upgrade:
- Set preserve=true (the default) to keep the EPHEMERAL partition (/var — etcd data, kubelet state, containerd cache, logs) intact. Set preserve=false only when you intend to wipe ephemeral data.
- Use stage=true to defer the reboot if you need to coordinate rolling restarts manually.
- After each node upgrade completes, run talos_health before proceeding to the next node.`, targetVersion, nodeClause, targetVersion, targetVersion)

	return textMsg(msg), nil
}
