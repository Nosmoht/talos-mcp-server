package tools

import (
	"context"
	"fmt"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// etcdStatusProbeTimeout bounds the per-node EtcdStatus probe performed by
// preflightEtcdQuorum. Five seconds separates a reachable node from a stuck
// one without delaying the mutation decision for unreachable nodes.
const etcdStatusProbeTimeout = 5 * time.Second

// preflightEtcdQuorum probes etcd membership and per-node reachability prior
// to a mutating etcd operation.
//
// Returns:
//
//   - configured: the number of etcd members reported by the first responding
//     apiNode. Zero with an error if no apiNode responds with a non-empty
//     member list.
//   - healthy: the number of apiNodes that respond to EtcdStatus within
//     etcdStatusProbeTimeout. Nodes rejected by the configured allowlist count
//     as unhealthy — the operator already made that reachability decision.
//
// Callers apply the "strict majority" rule: a mutation that removes N members
// proceeds only if (healthy - N) > configured/2.
//
// apiNodes should be the set of control plane nodes the caller can legitimately
// reach — typically the invoking tool's `nodes` argument, or the full
// control-plane set for cluster-wide operations.
func (h *Handlers) preflightEtcdQuorum(ctx context.Context, apiNodes []string) (configured, healthy int, err error) {
	if len(apiNodes) == 0 {
		return 0, 0, fmt.Errorf("preflightEtcdQuorum: apiNodes is required")
	}

	configured, err = h.fetchEtcdMemberCount(ctx, apiNodes)
	if err != nil {
		return 0, 0, err
	}

	healthy = h.countHealthyEtcdPeers(ctx, apiNodes)

	return configured, healthy, nil
}

// fetchEtcdMemberCount polls apiNodes in order, returning the member count
// from the first node whose EtcdMemberList returns a non-empty view.
func (h *Handlers) fetchEtcdMemberCount(ctx context.Context, apiNodes []string) (int, error) {
	var lastErr error
	for _, node := range apiNodes {
		nodeCtx, wErr := talos.WithNodes(ctx, []string{node}, h.AllowedNodes)
		if wErr != nil {
			lastErr = wErr
			continue
		}
		listCtx, cancel := context.WithTimeout(nodeCtx, etcdStatusProbeTimeout)
		resp, rErr := h.Client.EtcdMemberList(listCtx, &machineapi.EtcdMemberListRequest{})
		cancel()
		if rErr != nil {
			lastErr = rErr
			continue
		}
		// Each EtcdMembers entry is one node's view of the cluster; etcd
		// membership is consistent across members, so any non-empty view is
		// authoritative. Take the max to survive a node that reports an
		// empty placeholder.
		maxMembers := 0
		for _, msg := range resp.GetMessages() {
			if n := len(msg.GetMembers()); n > maxMembers {
				maxMembers = n
			}
		}
		if maxMembers > 0 {
			return maxMembers, nil
		}
	}
	return 0, fmt.Errorf("preflightEtcdQuorum: no apiNode returned a non-empty member list: %w", lastErr)
}

// countHealthyEtcdPeers returns the number of apiNodes that respond
// successfully to EtcdStatus within etcdStatusProbeTimeout.
func (h *Handlers) countHealthyEtcdPeers(ctx context.Context, apiNodes []string) int {
	var healthy int
	for _, node := range apiNodes {
		nodeCtx, wErr := talos.WithNodes(ctx, []string{node}, h.AllowedNodes)
		if wErr != nil {
			continue
		}
		probeCtx, cancel := context.WithTimeout(nodeCtx, etcdStatusProbeTimeout)
		_, sErr := h.Client.EtcdStatus(probeCtx)
		cancel()
		if sErr == nil {
			healthy++
		}
	}
	return healthy
}
