package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"
)

// preflightMock wraps mockClient and overrides only the two methods the
// quorum helper actually calls. Every other method still panics via the
// embedded mock — a regression that calls anything else will be caught
// immediately.
type preflightMock struct {
	*mockClient
	memberList func(ctx context.Context, req *machineapi.EtcdMemberListRequest) (*machineapi.EtcdMemberListResponse, error)
	status     func(ctx context.Context) (*machineapi.EtcdStatusResponse, error)
}

func (m *preflightMock) EtcdMemberList(ctx context.Context, req *machineapi.EtcdMemberListRequest, _ ...grpc.CallOption) (*machineapi.EtcdMemberListResponse, error) {
	return m.memberList(ctx, req)
}

func (m *preflightMock) EtcdStatus(ctx context.Context, _ ...grpc.CallOption) (*machineapi.EtcdStatusResponse, error) {
	return m.status(ctx)
}

// membersResponse builds an EtcdMemberListResponse carrying `count` members.
func membersResponse(count int) *machineapi.EtcdMemberListResponse {
	members := make([]*machineapi.EtcdMember, count)
	for i := range members {
		members[i] = &machineapi.EtcdMember{Id: uint64(i + 1)}
	}
	return &machineapi.EtcdMemberListResponse{
		Messages: []*machineapi.EtcdMembers{{Members: members}},
	}
}

// Test fixtures use RFC 5737 TEST-NET-1 addresses reserved for documentation.
var fakeCPNodes = []string{"cp-a", "cp-b", "cp-c"}

func TestPreflightEtcdQuorum_EmptyApiNodes(t *testing.T) {
	h := &Handlers{Client: &preflightMock{mockClient: &mockClient{}}}
	_, _, err := h.preflightEtcdQuorum(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "apiNodes is required") {
		t.Fatalf("want apiNodes-required error, got %v", err)
	}
}

func TestPreflightEtcdQuorum_MemberListFailsEverywhere(t *testing.T) {
	h := &Handlers{Client: &preflightMock{
		mockClient: &mockClient{},
		memberList: func(_ context.Context, _ *machineapi.EtcdMemberListRequest) (*machineapi.EtcdMemberListResponse, error) {
			return nil, errors.New("boom")
		},
		status: func(_ context.Context) (*machineapi.EtcdStatusResponse, error) {
			t.Fatal("EtcdStatus must not be called when member-list fetch fails on every node")
			return nil, nil
		},
	}}
	_, _, err := h.preflightEtcdQuorum(context.Background(), fakeCPNodes[:2])
	if err == nil || !strings.Contains(err.Error(), "no apiNode returned a non-empty member list") {
		t.Fatalf("want no-member-list error, got %v", err)
	}
}

func TestPreflightEtcdQuorum_AllNodesHealthy(t *testing.T) {
	h := &Handlers{Client: &preflightMock{
		mockClient: &mockClient{},
		memberList: func(_ context.Context, _ *machineapi.EtcdMemberListRequest) (*machineapi.EtcdMemberListResponse, error) {
			return membersResponse(3), nil
		},
		status: func(_ context.Context) (*machineapi.EtcdStatusResponse, error) {
			return &machineapi.EtcdStatusResponse{}, nil
		},
	}}
	configured, healthy, err := h.preflightEtcdQuorum(context.Background(), fakeCPNodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configured != 3 {
		t.Errorf("configured = %d, want 3", configured)
	}
	if healthy != 3 {
		t.Errorf("healthy = %d, want 3", healthy)
	}
}

func TestPreflightEtcdQuorum_OneUnhealthy(t *testing.T) {
	calls := 0
	h := &Handlers{Client: &preflightMock{
		mockClient: &mockClient{},
		memberList: func(_ context.Context, _ *machineapi.EtcdMemberListRequest) (*machineapi.EtcdMemberListResponse, error) {
			return membersResponse(3), nil
		},
		status: func(_ context.Context) (*machineapi.EtcdStatusResponse, error) {
			calls++
			if calls == 2 {
				return nil, errors.New("timeout")
			}
			return &machineapi.EtcdStatusResponse{}, nil
		},
	}}
	configured, healthy, err := h.preflightEtcdQuorum(context.Background(), fakeCPNodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configured != 3 {
		t.Errorf("configured = %d, want 3", configured)
	}
	if healthy != 2 {
		t.Errorf("healthy = %d, want 2", healthy)
	}
}

// TestPreflightEtcdQuorum_DedupByID verifies that fetchEtcdMemberCount
// deduplicates EtcdMembers by their raft Id. A Talos API proxy can
// emit the same member across Messages[] entries — without dedup the
// reported `configured` count is inflated and the strict-majority
// check (healthy - N) > configured/2 can pass when it must fail.
func TestPreflightEtcdQuorum_DedupByID(t *testing.T) {
	// Three distinct members (IDs 1, 2, 3) emitted twice: once in a
	// single Messages[] entry with duplicates, once split across two
	// Messages[] entries. Naive len(Members) = 6; dedup = 3.
	dup := &machineapi.EtcdMemberListResponse{
		Messages: []*machineapi.EtcdMembers{
			{Members: []*machineapi.EtcdMember{
				{Id: 1}, {Id: 2}, {Id: 3}, {Id: 1},
			}},
			{Members: []*machineapi.EtcdMember{
				{Id: 2}, {Id: 3},
			}},
		},
	}
	h := &Handlers{Client: &preflightMock{
		mockClient: &mockClient{},
		memberList: func(_ context.Context, _ *machineapi.EtcdMemberListRequest) (*machineapi.EtcdMemberListResponse, error) {
			return dup, nil
		},
		status: func(_ context.Context) (*machineapi.EtcdStatusResponse, error) {
			return &machineapi.EtcdStatusResponse{}, nil
		},
	}}
	configured, _, err := h.preflightEtcdQuorum(context.Background(), fakeCPNodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configured != 3 {
		t.Errorf("configured = %d, want 3 (unique IDs)", configured)
	}
}

// TestPreflightEtcdQuorum_MemberListFirstFailsSecondSucceeds verifies the
// fallback loop: if the first apiNode rejects member-list, the helper tries
// the next one instead of bailing out.
func TestPreflightEtcdQuorum_MemberListFirstFailsSecondSucceeds(t *testing.T) {
	var listCalls int
	h := &Handlers{Client: &preflightMock{
		mockClient: &mockClient{},
		memberList: func(_ context.Context, _ *machineapi.EtcdMemberListRequest) (*machineapi.EtcdMemberListResponse, error) {
			listCalls++
			if listCalls == 1 {
				return nil, errors.New("unreachable")
			}
			return membersResponse(5), nil
		},
		status: func(_ context.Context) (*machineapi.EtcdStatusResponse, error) {
			return &machineapi.EtcdStatusResponse{}, nil
		},
	}}
	configured, healthy, err := h.preflightEtcdQuorum(context.Background(), fakeCPNodes[:2])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configured != 5 {
		t.Errorf("configured = %d, want 5", configured)
	}
	if healthy != 2 {
		t.Errorf("healthy = %d, want 2", healthy)
	}
	if listCalls != 2 {
		t.Errorf("member list called %d times, want 2", listCalls)
	}
}
