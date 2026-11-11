package tools

import (
	"context"
	"errors"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	votingLearnerNode      = "192.0.2.44"
	votingDistinctVoters   = 3
	votingRemovedMembers   = 1
	votingUnreachableVoter = "192.0.2.43"
)

var (
	errVotingProbe = errors.New("voting stub: EtcdStatus unreachable")

	votingClusterNodes = []string{"192.0.2.41", "192.0.2.42", "192.0.2.43", votingLearnerNode}

	votingMemberIDs = map[string]uint64{
		"192.0.2.41":      1,
		"192.0.2.42":      2,
		"192.0.2.43":      3,
		votingLearnerNode: 4,
	}
)

type votingMembersClient struct {
	*mockClient

	failingNode string
}

func (c *votingMembersClient) EtcdMemberList(_ context.Context, _ *machineapi.EtcdMemberListRequest, _ ...grpc.CallOption) (*machineapi.EtcdMemberListResponse, error) {
	return &machineapi.EtcdMemberListResponse{
		Messages: []*machineapi.EtcdMembers{{Members: []*machineapi.EtcdMember{
			{Id: 1, Hostname: "cp-1"},
			{Id: 2, Hostname: "cp-2"},
			{Id: 3, Hostname: "cp-3"},
			{Id: 4, Hostname: "cp-4", IsLearner: true},
		}}},
	}, nil
}

func (c *votingMembersClient) EtcdStatus(ctx context.Context, _ ...grpc.CallOption) (*machineapi.EtcdStatusResponse, error) {
	node := votingTargetNode(ctx)
	if node == c.failingNode {
		return nil, errVotingProbe
	}
	return &machineapi.EtcdStatusResponse{
		Messages: []*machineapi.EtcdStatus{{MemberStatus: &machineapi.EtcdMemberStatus{
			MemberId:  votingMemberIDs[node],
			IsLearner: node == votingLearnerNode,
		}}},
	}, nil
}

func votingTargetNode(ctx context.Context) string {
	md, _ := metadata.FromOutgoingContext(ctx)
	if v := md.Get("node"); len(v) > 0 {
		return v[0]
	}
	return ""
}

func votingQuorum(t *testing.T, failingNode string) (configured, healthy int) {
	t.Helper()

	h := &Handlers{Client: &votingMembersClient{mockClient: &mockClient{}, failingNode: failingNode}}
	configured, healthy, err := h.preflightEtcdQuorum(context.Background(), votingClusterNodes)
	if err != nil {
		t.Fatalf("setup: etcd quorum preflight: %v", err)
	}
	return configured, healthy
}

func TestEtcdMemberCountExcludesLearners(t *testing.T) {
	configured, _ := votingQuorum(t, "")
	if configured != votingDistinctVoters {
		t.Errorf("spec AC-24: expected configured member count %d got %d", votingDistinctVoters, configured)
	}
}

func TestEtcdHealthyCountExcludesLearners(t *testing.T) {
	_, healthy := votingQuorum(t, "")
	if healthy != votingDistinctVoters {
		t.Errorf("spec AC-31: expected healthy count %d got %d", votingDistinctVoters, healthy)
	}
}

func TestEtcdQuorumRuleRefusesRemovalWithLearnerAndVoterDown(t *testing.T) {
	configured, healthy := votingQuorum(t, votingUnreachableVoter)
	if (healthy - votingRemovedMembers) > configured/2 {
		t.Errorf("spec AC-32: expected (healthy - %d) > configured/2 to be false got healthy=%d configured=%d",
			votingRemovedMembers, healthy, configured)
	}
}
