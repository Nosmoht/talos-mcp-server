package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	commonapi "github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// Suite rows of ac.md (AC-14 to AC-24, AC-27, AC-35 to AC-37, AC-41 to AC-45).
// They reuse the endpoint helpers of multinode_wire_test.go.

const (
	suiteBarrierLimit = 5 * time.Second
	suiteCancelRounds = 200
	suiteNodeErrorTag = "[node errors: "
)

var (
	errSuiteOpen        = errors.New("suite-open-failure")
	errSuiteLateFailure = errors.New("suite-late-failure")
)

// suiteNode returns the single "node" metadata value of an outgoing request.
func suiteNode(ctx context.Context) string {
	md, _ := metadata.FromOutgoingContext(ctx)
	if v := md.Get("node"); len(v) > 0 {
		return v[0]
	}
	return ""
}

// suiteToolFn is one tool invocation whose result the test inspects.
type suiteToolFn func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error)

func suiteCase(id string, fn suiteToolFn) wireToolCase {
	return wireToolCase{id: id, call: func(ctx context.Context, h *Handlers) error {
		_, _, err := fn(ctx, h)
		return err
	}}
}

func suiteLogs(nodes []string, tail int32) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleLogs(ctx, nil, LogsArgs{ServiceName: "kubelet", TailLines: tail, Nodes: nodes})
	}
}

func suiteDmesg(nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleDmesg(ctx, nil, DmesgArgs{Nodes: nodes})
	}
}

func suiteListFiles(nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleListFiles(ctx, nil, ListFilesArgs{Path: "/etc", Nodes: nodes})
	}
}

func suiteEvents(nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleEvents(ctx, nil, EventsArgs{Nodes: nodes})
	}
}

func suiteHealth(nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleHealth(ctx, nil, HealthArgs{Nodes: nodes})
	}
}

func suiteVersion(nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleVersion(ctx, nil, VersionArgs{Nodes: nodes})
	}
}

func suiteServices(nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleServices(ctx, nil, NodesOnlyArgs{Nodes: nodes})
	}
}

func suiteContainers(nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleContainers(ctx, nil, ContainersArgs{Nodes: nodes})
	}
}

func suiteProcesses(nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleProcesses(ctx, nil, NodesOnlyArgs{Nodes: nodes})
	}
}

func suiteEtcd(subcommand string, nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleEtcd(ctx, nil, EtcdArgs{Subcommand: subcommand, Nodes: nodes})
	}
}

func suiteServiceAction(nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleServiceAction(ctx, nil, ServiceActionArgs{ServiceName: "kubelet", Action: "restart", Confirm: true, Nodes: nodes})
	}
}

func suiteReboot(nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleReboot(ctx, nil, RebootArgs{Nodes: nodes, Confirm: true})
	}
}

func suiteUpgrade(nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleUpgrade(ctx, nil, UpgradeArgs{Nodes: nodes, Image: "ghcr.io/siderolabs/installer:v1.14.5", Confirm: true})
	}
}

func suiteRollback(nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleRollback(ctx, nil, RollbackArgs{Nodes: nodes, Confirm: true})
	}
}

func suiteReset(nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleReset(ctx, nil, ResetArgs{Nodes: nodes, Confirm: true})
	}
}

func suiteMeta(action string, nodes []string) suiteToolFn {
	return func(ctx context.Context, h *Handlers) (*mcp.CallToolResult, any, error) {
		return h.HandleMeta(ctx, nil, MetaArgs{Action: action, Key: wireMetaKey, Value: "v", Confirm: true, Nodes: nodes})
	}
}

// suiteUnaryReadCases are the unary read tools of AC-14, AC-15 and AC-37.
func suiteUnaryReadCases() []wireToolCase {
	nodes := wireTwoNodes()
	return []wireToolCase{
		suiteCase("version", suiteVersion(nodes)),
		suiteCase("services", suiteServices(nodes)),
		suiteCase("containers", suiteContainers(nodes)),
		suiteCase("processes", suiteProcesses(nodes)),
		suiteCase("etcd_members", suiteEtcd("members", nodes)),
		suiteCase("etcd_status", suiteEtcd("status", nodes)),
	}
}

// suiteMutatingCases are the mutating tools of AC-22 and AC-24.
func suiteMutatingCases() []wireToolCase {
	nodes := wireTwoNodes()
	return []wireToolCase{
		suiteCase("service_action", suiteServiceAction(nodes)),
		suiteCase("reboot", suiteReboot(nodes)),
		suiteCase("upgrade", suiteUpgrade(nodes)),
		suiteCase("rollback", suiteRollback(nodes)),
		suiteCase("reset", suiteReset(nodes)),
		suiteCase("meta_write", suiteMeta("write", nodes)),
		suiteCase("meta_delete", suiteMeta("delete", nodes)),
	}
}

func suiteText(t *testing.T, ac string, res *mcp.CallToolResult) string {
	t.Helper()

	if res == nil || len(res.Content) == 0 {
		t.Fatalf("spec ac.md %s: expected=text content got=%v", ac, res)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("spec ac.md %s: expected=text content got=%T", ac, res.Content[0])
	}
	return tc.Text
}

// suiteNodeErrorsBlock extracts the JSON of the "[node errors: ...]" text block.
func suiteNodeErrorsBlock(text string) (string, bool) {
	i := strings.Index(text, suiteNodeErrorTag)
	if i < 0 {
		return "", false
	}
	rest := text[i+len(suiteNodeErrorTag):]
	end := strings.LastIndex(rest, "]")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// suiteStream serves msgs, then err (io.EOF when nil), and ends with the
// context's error once the stream's context is done.
type suiteStream[T any] struct {
	grpc.ClientStream

	ctx  context.Context
	msgs []*T
	err  error
	next int
}

func (s *suiteStream[T]) Recv() (*T, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if s.next < len(s.msgs) {
		m := s.msgs[s.next]
		s.next++
		return m, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	return nil, io.EOF
}

// suiteStubClient is a Client whose stream RPCs are supplied per test; the
// embedded mockClient panics on every other RPC.
type suiteStubClient struct {
	mockClient

	logs   func(ctx context.Context, node string) (machineapi.MachineService_LogsClient, error)
	dmesg  func(ctx context.Context, node string) (machineapi.MachineService_DmesgClient, error)
	health func(ctx context.Context, node string) (clusterapi.ClusterService_HealthCheckClient, error)
	ls     func(ctx context.Context, node string) (machineapi.MachineService_ListClient, error)
}

var _ Client = (*suiteStubClient)(nil)

func (c *suiteStubClient) Logs(ctx context.Context, _ string, _ commonapi.ContainerDriver, _ string, _ bool, _ int32) (machineapi.MachineService_LogsClient, error) {
	return c.logs(ctx, suiteNode(ctx))
}

func (c *suiteStubClient) Dmesg(ctx context.Context, _, _ bool) (machineapi.MachineService_DmesgClient, error) {
	return c.dmesg(ctx, suiteNode(ctx))
}

func (c *suiteStubClient) ClusterHealthCheck(ctx context.Context, _ time.Duration, _ *clusterapi.ClusterInfo) (clusterapi.ClusterService_HealthCheckClient, error) {
	return c.health(ctx, suiteNode(ctx))
}

func (c *suiteStubClient) LS(ctx context.Context, _ *machineapi.ListRequest) (machineapi.MachineService_ListClient, error) {
	return c.ls(ctx, suiteNode(ctx))
}

// suiteOpenFailureClient fails every stream open with errSuiteOpen.
func suiteOpenFailureClient() *suiteStubClient {
	return &suiteStubClient{
		logs: func(context.Context, string) (machineapi.MachineService_LogsClient, error) {
			return nil, errSuiteOpen
		},
		dmesg: func(context.Context, string) (machineapi.MachineService_DmesgClient, error) {
			return nil, errSuiteOpen
		},
		health: func(context.Context, string) (clusterapi.ClusterService_HealthCheckClient, error) {
			return nil, errSuiteOpen
		},
	}
}

// suitePayloadClient answers each unary read with a payload that names the
// node the request was routed to.
type suitePayloadClient struct {
	mockClient
}

var _ Client = (*suitePayloadClient)(nil)

const (
	suiteTagPrefix     = "tag-"
	suiteServicePrefix = "svc-"
	suiteCtrPrefix     = "ctr-"
	suiteProcPrefix    = "proc-"
	suiteMemberPrefix  = "member-"
	suiteProtoPrefix   = "proto-"
)

func (c *suitePayloadClient) Version(ctx context.Context, _ ...grpc.CallOption) (*machineapi.VersionResponse, error) {
	return &machineapi.VersionResponse{Messages: []*machineapi.Version{{Version: &machineapi.VersionInfo{Tag: suiteTagPrefix + suiteNode(ctx)}}}}, nil
}

func (c *suitePayloadClient) ServiceList(ctx context.Context, _ ...grpc.CallOption) (*machineapi.ServiceListResponse, error) {
	return &machineapi.ServiceListResponse{Messages: []*machineapi.ServiceList{{Services: []*machineapi.ServiceInfo{{Id: suiteServicePrefix + suiteNode(ctx)}}}}}, nil
}

func (c *suitePayloadClient) Containers(ctx context.Context, _ string, _ commonapi.ContainerDriver, _ ...grpc.CallOption) (*machineapi.ContainersResponse, error) {
	return &machineapi.ContainersResponse{Messages: []*machineapi.Container{{Containers: []*machineapi.ContainerInfo{{Id: suiteCtrPrefix + suiteNode(ctx)}}}}}, nil
}

func (c *suitePayloadClient) Processes(ctx context.Context, _ ...grpc.CallOption) (*machineapi.ProcessesResponse, error) {
	return &machineapi.ProcessesResponse{Messages: []*machineapi.Process{{Processes: []*machineapi.ProcessInfo{{Command: suiteProcPrefix + suiteNode(ctx)}}}}}, nil
}

func (c *suitePayloadClient) EtcdMemberList(ctx context.Context, _ *machineapi.EtcdMemberListRequest, _ ...grpc.CallOption) (*machineapi.EtcdMemberListResponse, error) {
	return &machineapi.EtcdMemberListResponse{Messages: []*machineapi.EtcdMembers{{Members: []*machineapi.EtcdMember{{Id: 1 << 63, Hostname: suiteMemberPrefix + suiteNode(ctx)}}}}}, nil
}

func (c *suitePayloadClient) EtcdStatus(ctx context.Context, _ ...grpc.CallOption) (*machineapi.EtcdStatusResponse, error) {
	return &machineapi.EtcdStatusResponse{Messages: []*machineapi.EtcdStatus{{MemberStatus: &machineapi.EtcdMemberStatus{ProtocolVersion: suiteProtoPrefix + suiteNode(ctx)}}}}, nil
}

// suiteCancelClient answers every mutating RPC; its second call cancels the
// parent context before returning, and with failSecond set returns
// errSuiteLateFailure for the node that made that call.
type suiteCancelClient struct {
	mockClient

	calls      atomic.Int32
	cancel     context.CancelFunc
	failSecond bool
	failedNode atomic.Value
}

var _ Client = (*suiteCancelClient)(nil)

func (c *suiteCancelClient) hit(ctx context.Context) error {
	if c.calls.Add(1) != 2 {
		return nil
	}

	c.cancel()

	if !c.failSecond {
		return nil
	}

	c.failedNode.Store(suiteNode(ctx))

	return errSuiteLateFailure
}

func (c *suiteCancelClient) RebootNodes(ctx context.Context, _ machineapi.RebootRequest_Mode) error {
	return c.hit(ctx)
}

func (c *suiteCancelClient) UpgradeNodes(ctx context.Context, _ *machineapi.UpgradeRequest) (*machineapi.UpgradeResponse, error) {
	if err := c.hit(ctx); err != nil {
		return nil, err
	}

	return &machineapi.UpgradeResponse{}, nil
}

func (c *suiteCancelClient) ResetGenericWithResponse(ctx context.Context, _ *machineapi.ResetRequest) (*machineapi.ResetResponse, error) {
	if err := c.hit(ctx); err != nil {
		return nil, err
	}

	return &machineapi.ResetResponse{}, nil
}

func (c *suiteCancelClient) Rollback(ctx context.Context) error {
	return c.hit(ctx)
}

func (c *suiteCancelClient) MetaWrite(ctx context.Context, _ uint8, _ []byte, _ ...grpc.CallOption) error {
	return c.hit(ctx)
}

func (c *suiteCancelClient) MetaDelete(ctx context.Context, _ uint8, _ ...grpc.CallOption) error {
	return c.hit(ctx)
}

func (c *suiteCancelClient) ServiceRestart(ctx context.Context, _ string, _ ...grpc.CallOption) (*machineapi.ServiceRestartResponse, error) {
	if err := c.hit(ctx); err != nil {
		return nil, err
	}

	return &machineapi.ServiceRestartResponse{}, nil
}

func (c *suiteCancelClient) InvalidateVersionCache() {}

// startSuiteBarrierEndpoint holds requests for methods until parties have
// arrived and fails any that waits longer than suiteBarrierLimit.
func startSuiteBarrierEndpoint(t *testing.T, parties int, methods ...string) (*wireEndpoint, *talos.Client) {
	t.Helper()

	e := &wireEndpoint{}
	held := map[string]bool{}
	for _, m := range methods {
		held[m] = true
	}

	var (
		mu      sync.Mutex
		arrived int
		ready   = make(chan struct{})
	)

	handler := func(srv any, stream grpc.ServerStream) error {
		method, _ := grpc.MethodFromServerStream(stream)
		if held[method] {
			mu.Lock()
			arrived++
			if arrived == parties {
				close(ready)
			}
			mu.Unlock()

			select {
			case <-ready:
			case <-time.After(suiteBarrierLimit):
				return status.Errorf(codes.DeadlineExceeded, "fewer than %d requests in flight within %s", parties, suiteBarrierLimit)
			}
		}
		return e.serve(srv, stream)
	}

	socket := wireSocketPath(t)
	lis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("setup: listen on %s: %v", socket, err)
	}
	srv := grpc.NewServer(grpc.UnknownServiceHandler(handler))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	c, err := talosclient.New(context.Background(),
		talosclient.WithUnixSocket(socket),
		talosclient.WithGRPCDialOptions(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	if err != nil {
		t.Fatalf("setup: dial barrier endpoint: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	return e, &talos.Client{Client: c}
}

// TestSpecAC14_GivenUnaryReadToolWithTwoNodes_WhenCallSucceeds_ThenMessagesCarryEachNodesPayloadAndHostname
// covers ac.md AC-14: each node's payload sits under messages with
// metadata.hostname set to the requested node.
func TestSpecAC14_GivenUnaryReadToolWithTwoNodes_WhenCallSucceeds_ThenMessagesCarryEachNodesPayloadAndHostname(t *testing.T) {
	nodes := wireTwoNodes()
	cases := []struct {
		id     string
		call   suiteToolFn
		marker string
	}{
		{"version", suiteVersion(nodes), suiteTagPrefix},
		{"services", suiteServices(nodes), suiteServicePrefix},
		{"containers", suiteContainers(nodes), suiteCtrPrefix},
		{"processes", suiteProcesses(nodes), suiteProcPrefix},
		{"etcd_members", suiteEtcd("members", nodes), suiteMemberPrefix},
		{"etcd_status", suiteEtcd("status", nodes), suiteProtoPrefix},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			h := &Handlers{Client: &suitePayloadClient{}, SkipVersionCheck: true}
			_, out, err := tc.call(wireCtx(t), h)
			if err != nil {
				t.Fatalf("spec ac.md AC-14: case=%s expected=call succeeds got=%v", tc.id, err)
			}

			raw, err := json.Marshal(out)
			if err != nil {
				t.Fatalf("setup: marshal result: %v", err)
			}
			var res struct {
				Messages []json.RawMessage `json:"messages"`
			}
			if err := json.Unmarshal(raw, &res); err != nil {
				t.Fatalf("setup: decode result %s: %v", raw, err)
			}
			if len(res.Messages) != len(nodes) {
				t.Fatalf("spec ac.md AC-14: case=%s expected=%d messages got=%s", tc.id, len(nodes), raw)
			}

			byHost := map[string]string{}
			for _, m := range res.Messages {
				var meta struct {
					Metadata struct {
						Hostname string `json:"hostname"`
					} `json:"metadata"`
				}
				if err := json.Unmarshal(m, &meta); err != nil {
					t.Fatalf("setup: decode message %s: %v", m, err)
				}
				byHost[meta.Metadata.Hostname] = string(m)
			}
			for _, node := range nodes {
				msg, ok := byHost[node]
				if !ok || !strings.Contains(msg, tc.marker+node) {
					t.Errorf("spec ac.md AC-14: case=%s expected=message with metadata.hostname %q holding %q got=%v", tc.id, node, tc.marker+node, byHost)
				}
			}
		})
	}
}

// TestSpecAC15_GivenUnaryReadToolWithFailingSecondNode_WhenCalled_ThenFailsNamingNodeAndMessage
// covers ac.md AC-15: a node's failure fails the call and the error names it.
func TestSpecAC15_GivenUnaryReadToolWithFailingSecondNode_WhenCalled_ThenFailsNamingNodeAndMessage(t *testing.T) {
	checkOrderedFailure(t, "AC-15", map[string]string{wireNodeB: wireErrB}, [][2]string{{wireNodeB, wireErrB}}, suiteUnaryReadCases())
}

// TestSpecAC37_GivenUnaryReadToolWithEveryNodeFailing_WhenCalled_ThenFailsWithNodeErrorsInRequestOrder
// covers ac.md AC-37: the per-node errors appear in request order.
func TestSpecAC37_GivenUnaryReadToolWithEveryNodeFailing_WhenCalled_ThenFailsWithNodeErrorsInRequestOrder(t *testing.T) {
	failures := map[string]string{wireNodeA: wireErrA, wireNodeB: wireErrB}
	order := [][2]string{{wireNodeA, wireErrA}, {wireNodeB, wireErrB}}

	checkOrderedFailure(t, "AC-37", failures, order, suiteUnaryReadCases())
}

// TestSpecAC16_GivenListFilesWithFailingSecondNode_WhenCalled_ThenKeepsSurvivorEntry_AND_ReportsNodeError
// covers ac.md AC-16.
func TestSpecAC16_GivenListFilesWithFailingSecondNode_WhenCalled_ThenKeepsSurvivorEntry_AND_ReportsNodeError(t *testing.T) {
	_, client := startWireEndpoint(t, map[string]string{wireNodeB: wireErrB})

	_, out, err := suiteListFiles(wireTwoNodes())(wireCtx(t), wireHandlers(client))
	if err != nil {
		t.Fatalf("spec ac.md AC-16: expected=call succeeds got=%v", err)
	}
	res := wireJSON(t, out)

	entries, _ := res["entries"].([]any)
	found := false
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		if entry["name"] == wireEntryName(wireNodeA) {
			found = true
		}
	}
	if !found {
		t.Errorf("spec ac.md AC-16: expected=entry %q got=entries %v", wireEntryName(wireNodeA), res["entries"])
	}
	suiteExpectNodeError(t, "AC-16", res, wireNodeB, wireErrB)
}

// TestSpecAC17_GivenLogsWithFailingSecondNode_WhenCalled_ThenKeepsSurvivorLine_AND_ReportsNodeError
// covers ac.md AC-17.
func TestSpecAC17_GivenLogsWithFailingSecondNode_WhenCalled_ThenKeepsSurvivorLine_AND_ReportsNodeError(t *testing.T) {
	_, client := startWireEndpoint(t, map[string]string{wireNodeB: wireErrB})

	_, out, err := suiteLogs(wireTwoNodes(), 0)(wireCtx(t), wireHandlers(client))
	if err != nil {
		t.Fatalf("spec ac.md AC-17: expected=call succeeds got=%v", err)
	}
	res := wireJSON(t, out)

	if !suiteHasLine(res, "log line from "+wireNodeA) {
		t.Errorf("spec ac.md AC-17: expected=line %q got=lines %v", "log line from "+wireNodeA, res["lines"])
	}
	suiteExpectNodeError(t, "AC-17", res, wireNodeB, wireErrB)
}

// TestSpecAC18_GivenDmesgWithFailingSecondNode_WhenCalled_ThenKeepsSurvivorLine_AND_ReportsNodeError
// covers ac.md AC-18.
func TestSpecAC18_GivenDmesgWithFailingSecondNode_WhenCalled_ThenKeepsSurvivorLine_AND_ReportsNodeError(t *testing.T) {
	_, client := startWireEndpoint(t, map[string]string{wireNodeB: wireErrB})

	_, out, err := suiteDmesg(wireTwoNodes())(wireCtx(t), wireHandlers(client))
	if err != nil {
		t.Fatalf("spec ac.md AC-18: expected=call succeeds got=%v", err)
	}
	res := wireJSON(t, out)

	if !suiteHasLine(res, "kernel line from "+wireNodeA) {
		t.Errorf("spec ac.md AC-18: expected=line %q got=lines %v", "kernel line from "+wireNodeA, res["lines"])
	}
	suiteExpectNodeError(t, "AC-18", res, wireNodeB, wireErrB)
}

// TestSpecAC20_GivenEventsWithTwoNodes_WhenWindowEnds_ThenEachEventCarriesItsRequestedNode
// covers ac.md AC-20.
func TestSpecAC20_GivenEventsWithTwoNodes_WhenWindowEnds_ThenEachEventCarriesItsRequestedNode(t *testing.T) {
	_, client := startWireEndpoint(t, nil)

	_, out, err := suiteEvents(wireTwoNodes())(wireCtx(t), wireHandlers(client))
	if err != nil {
		t.Fatalf("spec ac.md AC-20: expected=call succeeds got=%v", err)
	}
	res := wireJSON(t, out)

	events, _ := res["events"].([]any)
	for _, node := range wireTwoNodes() {
		found := false
		for _, ev := range events {
			entry, _ := ev.(map[string]any)
			if entry["id"] == wireEventID(node) && entry["node"] == node {
				found = true
			}
		}
		if !found {
			t.Errorf("spec ac.md AC-20: expected=event %q with node %q got=events %v", wireEventID(node), node, res["events"])
		}
	}
}

func suiteHasLine(res map[string]any, want string) bool {
	lines, _ := res["lines"].([]any)
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func suiteExpectNodeError(t *testing.T, ac string, res map[string]any, node, msg string) {
	t.Helper()

	nodeErrs, _ := res["node_errors"].(map[string]any)
	if got, _ := nodeErrs[node].(string); !strings.Contains(got, msg) {
		t.Errorf("spec ac.md %s: expected=node_errors[%q] containing %q got=node_errors %v", ac, node, msg, res["node_errors"])
	}
}

// TestSpecAC19_GivenStreamToolWithoutNodes_WhenEndpointStreamFails_ThenCallFailsWithItsMessage
// covers ac.md AC-19.
func TestSpecAC19_GivenStreamToolWithoutNodes_WhenEndpointStreamFails_ThenCallFailsWithItsMessage(t *testing.T) {
	const msg = "default-context-stream-unavailable"

	for _, tc := range []wireToolCase{
		suiteCase("logs", suiteLogs(nil, 0)),
		suiteCase("dmesg", suiteDmesg(nil)),
		suiteCase("list_files", suiteListFiles(nil)),
	} {
		t.Run(tc.id, func(t *testing.T) {
			_, client := startWireEndpoint(t, map[string]string{"": msg})

			err := tc.call(wireCtx(t), wireHandlers(client))
			if err == nil || !strings.Contains(err.Error(), msg) {
				t.Errorf("spec ac.md AC-19: case=%s expected=error containing %q got=%v", tc.id, msg, err)
			}
		})
	}
}

// TestSpecAC21_GivenMutatingToolReturningResponseWithTwoNodes_WhenCallSucceeds_ThenOneMessagePerNodeWithHostname
// covers ac.md AC-21.
func TestSpecAC21_GivenMutatingToolReturningResponseWithTwoNodes_WhenCallSucceeds_ThenOneMessagePerNodeWithHostname(t *testing.T) {
	nodes := wireTwoNodes()
	cases := []struct {
		id   string
		call suiteToolFn
	}{
		{"service_action", suiteServiceAction(nodes)},
		{"upgrade", suiteUpgrade(nodes)},
		{"reset", suiteReset(nodes)},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			_, client := startWireEndpoint(t, nil)

			_, out, err := tc.call(wireCtx(t), wireHandlers(client))
			if err != nil {
				t.Fatalf("spec ac.md AC-21: case=%s expected=call succeeds got=%v", tc.id, err)
			}

			messages, _ := wireJSON(t, out)["messages"].([]any)
			var hostnames []string
			for _, m := range messages {
				msg, _ := m.(map[string]any)
				meta, _ := msg["metadata"].(map[string]any)
				hostname, _ := meta["hostname"].(string)
				hostnames = append(hostnames, hostname)
			}
			if !reflect.DeepEqual(hostnames, nodes) {
				t.Errorf("spec ac.md AC-21: case=%s expected=one message per node with metadata.hostname %v got=%v", tc.id, nodes, hostnames)
			}
		})
	}
}

// TestSpecAC22_GivenMutatingToolWithFailingSecondNode_WhenCalled_ThenFailsNamingNodeAndMessage
// covers ac.md AC-22.
func TestSpecAC22_GivenMutatingToolWithFailingSecondNode_WhenCalled_ThenFailsNamingNodeAndMessage(t *testing.T) {
	checkOrderedFailure(t, "AC-22", map[string]string{wireNodeB: wireErrB}, [][2]string{{wireNodeB, wireErrB}}, suiteMutatingCases())
}

// TestSpecAC23_GivenConfirmedLifecycleToolWithTwoNodes_WhenEndpointHoldsRequestsUntilBothArrive_ThenBothAreInFlightAndCallSucceeds
// covers ac.md AC-23: the per-node requests run concurrently.
func TestSpecAC23_GivenConfirmedLifecycleToolWithTwoNodes_WhenEndpointHoldsRequestsUntilBothArrive_ThenBothAreInFlightAndCallSucceeds(t *testing.T) {
	nodes := wireTwoNodes()
	cases := []struct {
		id     string
		method string
		call   suiteToolFn
	}{
		{"reboot", machineapi.MachineService_Reboot_FullMethodName, suiteReboot(nodes)},
		{"upgrade", machineapi.MachineService_Upgrade_FullMethodName, suiteUpgrade(nodes)},
		{"reset", machineapi.MachineService_Reset_FullMethodName, suiteReset(nodes)},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			endpoint, client := startSuiteBarrierEndpoint(t, len(nodes), tc.method)

			if _, _, err := tc.call(wireCtx(t), wireHandlers(client)); err != nil {
				t.Fatalf("spec ac.md AC-23: case=%s expected=call succeeds with both requests in flight got=%v", tc.id, err)
			}

			held := 0
			for _, r := range endpoint.recorded() {
				if r.method == tc.method {
					held++
				}
			}
			if held != len(nodes) {
				t.Errorf("spec ac.md AC-23: case=%s expected=%d %s requests got=%d", tc.id, len(nodes), tc.method, held)
			}
		})
	}
}

// TestSpecAC24_GivenAllowlistMissingSecondNode_WhenMultiNodeToolCalled_ThenFailsNodeNotAllowed_AND_SendsNoRequest
// covers ac.md AC-24: the allowlist is enforced before any request leaves.
func TestSpecAC24_GivenAllowlistMissingSecondNode_WhenMultiNodeToolCalled_ThenFailsNodeNotAllowed_AND_SendsNoRequest(t *testing.T) {
	nodes := wireTwoNodes()
	cases := append(suiteUnaryReadCases(), suiteMutatingCases()...)
	cases = append(cases,
		suiteCase("health", suiteHealth(nodes)),
		suiteCase("list_files", suiteListFiles(nodes)),
		suiteCase("logs", suiteLogs(nodes, 0)),
		suiteCase("dmesg", suiteDmesg(nodes)),
		suiteCase("events", suiteEvents(nodes)),
	)

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			endpoint, client := startWireEndpoint(t, nil)
			h := wireHandlers(client)
			h.AllowedNodes = wireAllowlist(t, wireNodeA)

			err := tc.call(wireCtx(t), h)

			if err == nil || !strings.Contains(err.Error(), "not in the allowed nodes") || !strings.Contains(err.Error(), wireNodeB) {
				t.Errorf("spec ac.md AC-24: case=%s expected=node-not-allowed error naming %s got=%v", tc.id, wireNodeB, err)
			}
			if reqs := endpoint.recorded(); len(reqs) != 0 {
				t.Errorf("spec ac.md AC-24: case=%s expected=zero requests at the Talos API endpoint got=%d %v", tc.id, len(reqs), reqs)
			}
		})
	}
}

// TestSpecAC27_GivenReadFileWithOneNode_WhenCalled_ThenReturnsContentRoutedToThatNode
// covers ac.md AC-27.
func TestSpecAC27_GivenReadFileWithOneNode_WhenCalled_ThenReturnsContentRoutedToThatNode(t *testing.T) {
	endpoint, client := startWireEndpoint(t, nil)

	_, out, err := wireHandlers(client).HandleReadFile(wireCtx(t), nil, ReadFileArgs{Path: "/etc/os-release", Nodes: []string{wireNodeA}})
	if err != nil {
		t.Fatalf("spec ac.md AC-27: expected=call succeeds got=%v", err)
	}

	want := "file content from " + wireNodeA
	if got := wireJSON(t, out)["content"]; got != want {
		t.Errorf("spec ac.md AC-27: expected=content %q got=%v", want, got)
	}

	reads := 0
	for _, r := range endpoint.recorded() {
		if r.method != machineapi.MachineService_Read_FullMethodName {
			continue
		}
		reads++
		if !reflect.DeepEqual(r.node, []string{wireNodeA}) || len(r.nodes) != 0 {
			t.Errorf("spec ac.md AC-27: expected=Read routed with node %q and no nodes got=node=%q nodes=%q", wireNodeA, r.node, r.nodes)
		}
	}
	if reads == 0 {
		t.Errorf("spec ac.md AC-27: expected=at least one Read request got=none")
	}
}

// --- AC-35 / AC-36: zero- and one-node stream error texts ---

func suiteSingleOrNoNode() map[string][]string {
	return map[string][]string{"one_node": {wireNodeA}, "no_node": nil}
}

// TestSpecAC35_GivenStreamToolWithOneOrNoNode_WhenOpeningStreamFails_ThenErrorKeepsOpenFailurePrefix
// covers ac.md AC-35: an open failure keeps its own text.
func TestSpecAC35_GivenStreamToolWithOneOrNoNode_WhenOpeningStreamFails_ThenErrorKeepsOpenFailurePrefix(t *testing.T) {
	for name, nodes := range suiteSingleOrNoNode() {
		cases := []struct {
			id     string
			call   suiteToolFn
			prefix string
		}{
			{"logs", suiteLogs(nodes, 0), `logs for "kubelet": `},
			{"dmesg", suiteDmesg(nodes), "dmesg: "},
			{"health", suiteHealth(nodes), "health check: "},
		}

		for _, tc := range cases {
			t.Run(name+"/"+tc.id, func(t *testing.T) {
				h := &Handlers{Client: suiteOpenFailureClient(), SkipVersionCheck: true}

				_, _, err := tc.call(wireCtx(t), h)

				if err == nil || !strings.HasPrefix(err.Error(), tc.prefix) || !strings.Contains(err.Error(), errSuiteOpen.Error()) {
					t.Errorf("spec ac.md AC-35: case=%s expected=error beginning %q containing %q got=%v", tc.id, tc.prefix, errSuiteOpen, err)
				}
			})
		}
	}
}

// TestSpecAC36_GivenStreamToolWithOneOrNoNode_WhenReceiveFails_ThenErrorKeepsReceiveFailurePrefix
// covers ac.md AC-36: a receive failure keeps its own text.
func TestSpecAC36_GivenStreamToolWithOneOrNoNode_WhenReceiveFails_ThenErrorKeepsReceiveFailurePrefix(t *testing.T) {
	const msg = "receive-failure-unavailable"

	for name, nodes := range suiteSingleOrNoNode() {
		failing := ""
		if len(nodes) > 0 {
			failing = nodes[0]
		}
		cases := []struct {
			id     string
			call   suiteToolFn
			prefix string
		}{
			{"logs", suiteLogs(nodes, 0), `logs stream for "kubelet": `},
			{"dmesg", suiteDmesg(nodes), "dmesg stream: "},
			{"health", suiteHealth(nodes), "health check failed: "},
		}

		for _, tc := range cases {
			t.Run(name+"/"+tc.id, func(t *testing.T) {
				_, client := startWireEndpoint(t, map[string]string{failing: msg})

				_, _, err := tc.call(wireCtx(t), wireHandlers(client))

				if err == nil || !strings.HasPrefix(err.Error(), tc.prefix) || !strings.Contains(err.Error(), msg) {
					t.Errorf("spec ac.md AC-36: case=%s expected=error beginning %q containing %q got=%v", tc.id, tc.prefix, msg, err)
				}
			})
		}
	}
}

// TestSpecAC41_GivenLogsWithTwoNodesServingMoreThanTailLines_WhenCalled_ThenSucceedsWithExactlyTailLines_AND_NoNodeErrors
// covers ac.md AC-41: stopping at the cap is not a node error.
func TestSpecAC41_GivenLogsWithTwoNodesServingMoreThanTailLines_WhenCalled_ThenSucceedsWithExactlyTailLines_AND_NoNodeErrors(t *testing.T) {
	const tail = 5

	stub := &suiteStubClient{
		logs: func(ctx context.Context, node string) (machineapi.MachineService_LogsClient, error) {
			msgs := make([]*commonapi.Data, 0, 4*tail)
			for range 4 * tail {
				msgs = append(msgs, &commonapi.Data{Bytes: []byte("line from " + node + "\n")})
			}
			return &suiteStream[commonapi.Data]{ctx: ctx, msgs: msgs}, nil
		},
	}

	res, out, err := suiteLogs(wireTwoNodes(), tail)(wireCtx(t), &Handlers{Client: stub, SkipVersionCheck: true})
	if err != nil {
		t.Fatalf("spec ac.md AC-41: expected=call succeeds got=%v", err)
	}

	got := wireJSON(t, out)
	if lines, _ := got["lines"].([]any); len(lines) != tail {
		t.Errorf("spec ac.md AC-41: expected=exactly %d lines got=%d", tail, len(lines))
	}
	if _, present := got["node_errors"]; present {
		t.Errorf("spec ac.md AC-41: expected=no node_errors got=%v", got["node_errors"])
	}
	if text := suiteText(t, "AC-41", res); strings.Contains(text, suiteNodeErrorTag) {
		t.Errorf("spec ac.md AC-41: expected=no node errors block got=%q", text)
	}
}

// TestSpecAC42_GivenListFilesWithTwoNodesServingMoreThanMaxEntries_WhenCalled_ThenSucceedsTruncatedWithMaxEntries_AND_NoNodeErrors
// covers ac.md AC-42: stopping at maxListEntries is not a node error.
func TestSpecAC42_GivenListFilesWithTwoNodesServingMoreThanMaxEntries_WhenCalled_ThenSucceedsTruncatedWithMaxEntries_AND_NoNodeErrors(t *testing.T) {
	stub := &suiteStubClient{
		ls: func(ctx context.Context, node string) (machineapi.MachineService_ListClient, error) {
			msgs := make([]*machineapi.FileInfo, 0, maxListEntries+10)
			for range maxListEntries + 10 {
				msgs = append(msgs, &machineapi.FileInfo{Name: "/data/" + node})
			}
			return &suiteStream[machineapi.FileInfo]{ctx: ctx, msgs: msgs}, nil
		},
	}

	res, out, err := suiteListFiles(wireTwoNodes())(wireCtx(t), &Handlers{Client: stub, SkipVersionCheck: true})
	if err != nil {
		t.Fatalf("spec ac.md AC-42: expected=call succeeds got=%v", err)
	}

	got := wireJSON(t, out)
	if entries, _ := got["entries"].([]any); len(entries) != maxListEntries {
		t.Errorf("spec ac.md AC-42: expected=exactly %d entries got=%d", maxListEntries, len(entries))
	}
	if got["truncated"] != true {
		t.Errorf("spec ac.md AC-42: expected=truncated true got=%v", got["truncated"])
	}
	if _, present := got["node_errors"]; present {
		t.Errorf("spec ac.md AC-42: expected=no node_errors got=%v", got["node_errors"])
	}
	if text := suiteText(t, "AC-42", res); strings.Contains(text, suiteNodeErrorTag) {
		t.Errorf("spec ac.md AC-42: expected=no node errors block got=%q", text[len(text)-80:])
	}
}

// TestSpecAC43_GivenMutatingToolWithTwoNodes_WhenContextEndsAfterBothResponses_ThenReportsSuccess
// covers ac.md AC-43: a fan-out whose nodes all answered succeeds although
// the parent context ended.
func TestSpecAC43_GivenMutatingToolWithTwoNodes_WhenContextEndsAfterBothResponses_ThenReportsSuccess(t *testing.T) {
	nodes := wireTwoNodes()
	cases := []struct {
		id   string
		call suiteToolFn
	}{
		{"reboot", suiteReboot(nodes)},
		{"upgrade", suiteUpgrade(nodes)},
		{"reset", suiteReset(nodes)},
		{"meta_write", suiteMeta("write", nodes)},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			for round := range suiteCancelRounds {
				ctx, cancel := context.WithCancel(t.Context())
				stub := &suiteCancelClient{cancel: cancel}

				_, _, err := tc.call(ctx, &Handlers{Client: stub, SkipVersionCheck: true})

				if ctxErr := ctx.Err(); ctxErr == nil {
					t.Fatalf("setup: round %d: the stub did not end the context", round)
				}
				if got := int(stub.calls.Load()); got != len(nodes) {
					t.Fatalf("spec ac.md AC-43: case=%s round=%d expected=%d node calls got=%d", tc.id, round, len(nodes), got)
				}
				if err != nil {
					t.Fatalf("spec ac.md AC-43: case=%s round=%d expected=success after both responses got=%v", tc.id, round, err)
				}
				cancel()
			}
		})
	}
}

// TestSpecAC43_GivenMutatingToolWithTwoNodes_WhenContextEndsAfterOneNodeFailed_ThenFailsNamingThatNode
// covers ac.md AC-43 (spec A2): a failure delivered as the context ends is
// not dropped. Each round has a chance of losing it, so the rounds repeat.
func TestSpecAC43_GivenMutatingToolWithTwoNodes_WhenContextEndsAfterOneNodeFailed_ThenFailsNamingThatNode(t *testing.T) {
	nodes := wireTwoNodes()
	cases := []struct {
		id   string
		call suiteToolFn
	}{
		{"service_action", suiteServiceAction(nodes)},
		{"reboot", suiteReboot(nodes)},
		{"upgrade", suiteUpgrade(nodes)},
		{"rollback", suiteRollback(nodes)},
		{"reset", suiteReset(nodes)},
		{"meta_write", suiteMeta("write", nodes)},
		{"meta_delete", suiteMeta("delete", nodes)},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			for round := range suiteCancelRounds {
				ctx, cancel := context.WithCancel(t.Context())
				stub := &suiteCancelClient{cancel: cancel, failSecond: true}

				_, _, err := tc.call(ctx, &Handlers{Client: stub, SkipVersionCheck: true})

				if ctxErr := ctx.Err(); ctxErr == nil {
					t.Fatalf("setup: round %d: the stub did not end the context", round)
				}
				failed, _ := stub.failedNode.Load().(string)
				if failed == "" {
					t.Fatalf("setup: case=%s round=%d: the stub failed no node", tc.id, round)
				}
				if err == nil || !strings.Contains(err.Error(), failed) || !strings.Contains(err.Error(), errSuiteLateFailure.Error()) {
					t.Fatalf("spec ac.md AC-43: case=%s round=%d expected=error naming %s with %q got=%v", tc.id, round, failed, errSuiteLateFailure, err)
				}
				cancel()
			}
		})
	}
}

// TestSpecAC44_GivenStreamToolWithFailingSecondNode_WhenCalled_ThenStructuredAndTextNodeErrorsAgree
// covers ac.md AC-44: both node_errors channels carry the same entries.
func TestSpecAC44_GivenStreamToolWithFailingSecondNode_WhenCalled_ThenStructuredAndTextNodeErrorsAgree(t *testing.T) {
	nodes := wireTwoNodes()
	cases := []struct {
		id   string
		call suiteToolFn
	}{
		{"logs", suiteLogs(nodes, 0)},
		{"dmesg", suiteDmesg(nodes)},
		{"list_files", suiteListFiles(nodes)},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			_, client := startWireEndpoint(t, map[string]string{wireNodeB: wireErrB})

			res, out, err := tc.call(wireCtx(t), wireHandlers(client))
			if err != nil {
				t.Fatalf("spec ac.md AC-44: case=%s expected=call succeeds got=%v", tc.id, err)
			}

			structured := map[string]string{}
			if m, _ := wireJSON(t, out)["node_errors"].(map[string]any); m != nil {
				for k, v := range m {
					structured[k], _ = v.(string)
				}
			}

			block, ok := suiteNodeErrorsBlock(suiteText(t, "AC-44", res))
			if !ok {
				t.Fatalf("spec ac.md AC-44: case=%s expected=%s...] block in text got=%q", tc.id, suiteNodeErrorTag, suiteText(t, "AC-44", res))
			}
			fromText := map[string]string{}
			if err := json.Unmarshal([]byte(block), &fromText); err != nil {
				t.Fatalf("spec ac.md AC-44: case=%s expected=JSON node errors block got=%q (%v)", tc.id, block, err)
			}

			if len(structured) == 0 || !reflect.DeepEqual(structured, fromText) {
				t.Errorf("spec ac.md AC-44: case=%s expected=identical non-empty node errors in structured field and text got=structured %v text %v", tc.id, structured, fromText)
			}
		})
	}
}

// TestSpecAC45_GivenStreamToolWithOneOrNoNode_WhenNodeSucceeds_ThenNoNodeErrorsInEitherChannel
// covers ac.md AC-45: success carries no node_errors key and no text block.
func TestSpecAC45_GivenStreamToolWithOneOrNoNode_WhenNodeSucceeds_ThenNoNodeErrorsInEitherChannel(t *testing.T) {
	for name, nodes := range suiteSingleOrNoNode() {
		cases := []struct {
			id   string
			call suiteToolFn
		}{
			{"logs", suiteLogs(nodes, 0)},
			{"dmesg", suiteDmesg(nodes)},
			{"list_files", suiteListFiles(nodes)},
		}

		for _, tc := range cases {
			t.Run(name+"/"+tc.id, func(t *testing.T) {
				_, client := startWireEndpoint(t, nil)

				res, out, err := tc.call(wireCtx(t), wireHandlers(client))
				if err != nil {
					t.Fatalf("spec ac.md AC-45: case=%s expected=call succeeds got=%v", tc.id, err)
				}

				if _, present := wireJSON(t, out)["node_errors"]; present {
					t.Errorf("spec ac.md AC-45: case=%s expected=no node_errors key got=%v", tc.id, wireJSON(t, out)["node_errors"])
				}
				if text := suiteText(t, "AC-45", res); strings.Contains(text, suiteNodeErrorTag) {
					t.Errorf("spec ac.md AC-45: case=%s expected=no node errors block got=%q", tc.id, text)
				}
			})
		}
	}
}
