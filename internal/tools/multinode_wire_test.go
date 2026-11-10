package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	commonapi "github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	talosmeta "github.com/siderolabs/talos/pkg/machinery/meta"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// RFC 5737 documentation addresses (spec.md A7).
const (
	wireNodeA = "192.0.2.10"
	wireNodeB = "192.0.2.11"
	wireNodeC = "192.0.2.12"
)

// Per-node gRPC error messages the fake endpoint returns for a failing node.
const (
	wireErrA = "node-a-stream-unavailable"
	wireErrB = "node-b-stream-unavailable"
	wireErrC = "node-c-stream-unavailable"
)

const wireCallTimeout = 30 * time.Second

// sun_path holds 104 bytes on macOS and 108 on Linux, including the NUL.
const wireMaxSocketPath = 100

// UnmarshalEvent matches this exact type URL (machinery client/events.go).
const wireSequenceEventType = "talos/runtime/machine.SequenceEvent"

// wireMetaKey is a META key talos_meta writes and deletes without a
// privileged-key entry (UserReserved1).
var wireMetaKey = strconv.Itoa(talosmeta.UserReserved1)

// wireExactlyOneNode matches an error stating that exactly one node is required.
var wireExactlyOneNode = regexp.MustCompile(`(?i)exactly one (target )?node`)

// wireRequest is one request as the fake endpoint received it.
type wireRequest struct {
	method string
	node   []string
	nodes  []string
}

// wireEndpoint answers like apid (spec.md A7): "nodes" requests one-to-many, a
// failed node as a metadata-only message; "node" requests one-to-one, a failed
// node as a gRPC error. The recorder is safe for concurrent per-node requests.
type wireEndpoint struct {
	failures map[string]string // node → gRPC error message; read-only after start

	mu       sync.Mutex
	requests []wireRequest
}

// wireUnaryResponses maps the unary RPCs the tools call to their response type.
var wireUnaryResponses = map[string]func() proto.Message{
	machineapi.MachineService_Version_FullMethodName:            func() proto.Message { return &machineapi.VersionResponse{} },
	machineapi.MachineService_ServiceList_FullMethodName:        func() proto.Message { return &machineapi.ServiceListResponse{} },
	machineapi.MachineService_Containers_FullMethodName:         func() proto.Message { return &machineapi.ContainersResponse{} },
	machineapi.MachineService_Processes_FullMethodName:          func() proto.Message { return &machineapi.ProcessesResponse{} },
	machineapi.MachineService_EtcdMemberList_FullMethodName:     func() proto.Message { return &machineapi.EtcdMemberListResponse{} },
	machineapi.MachineService_EtcdStatus_FullMethodName:         func() proto.Message { return &machineapi.EtcdStatusResponse{} },
	machineapi.MachineService_ServiceStart_FullMethodName:       func() proto.Message { return &machineapi.ServiceStartResponse{} },
	machineapi.MachineService_ServiceStop_FullMethodName:        func() proto.Message { return &machineapi.ServiceStopResponse{} },
	machineapi.MachineService_ServiceRestart_FullMethodName:     func() proto.Message { return &machineapi.ServiceRestartResponse{} },
	machineapi.MachineService_Reboot_FullMethodName:             func() proto.Message { return &machineapi.RebootResponse{} },
	machineapi.MachineService_Upgrade_FullMethodName:            func() proto.Message { return &machineapi.UpgradeResponse{} },
	machineapi.MachineService_Rollback_FullMethodName:           func() proto.Message { return &machineapi.RollbackResponse{} },
	machineapi.MachineService_Reset_FullMethodName:              func() proto.Message { return &machineapi.ResetResponse{} },
	machineapi.MachineService_MetaWrite_FullMethodName:          func() proto.Message { return &machineapi.MetaWriteResponse{} },
	machineapi.MachineService_MetaDelete_FullMethodName:         func() proto.Message { return &machineapi.MetaDeleteResponse{} },
	machineapi.MachineService_ApplyConfiguration_FullMethodName: func() proto.Message { return &machineapi.ApplyConfigurationResponse{} },
}

// wireStreamPayloads maps the server-streaming RPCs the tools call to the
// payload message a healthy node sends.
var wireStreamPayloads = map[string]func(node string) (proto.Message, error){
	machineapi.MachineService_Logs_FullMethodName: func(node string) (proto.Message, error) {
		return &commonapi.Data{Bytes: []byte("log line from " + node + "\n")}, nil
	},
	machineapi.MachineService_Dmesg_FullMethodName: func(node string) (proto.Message, error) {
		return &commonapi.Data{Bytes: []byte("kernel line from " + node + "\n")}, nil
	},
	machineapi.MachineService_Read_FullMethodName: func(node string) (proto.Message, error) {
		return &commonapi.Data{Bytes: []byte("file content from " + node)}, nil
	},
	machineapi.MachineService_List_FullMethodName: func(node string) (proto.Message, error) {
		return &machineapi.FileInfo{Name: wireEntryName(node), Size: 1}, nil
	},
	machineapi.MachineService_Events_FullMethodName: wireEvent,
	clusterapi.ClusterService_HealthCheck_FullMethodName: func(node string) (proto.Message, error) {
		return &clusterapi.HealthCheckProgress{Message: "health check passed on " + node}, nil
	},
}

// wireEntryName is the file entry a healthy node lists.
func wireEntryName(node string) string { return "/etc/entry-from-" + node }

// wireEventID is the ID of the event a healthy node emits.
func wireEventID(node string) string { return "event-from-" + node }

func wireEvent(node string) (proto.Message, error) {
	value, err := proto.Marshal(&machineapi.SequenceEvent{Sequence: "boot"})
	if err != nil {
		return nil, err
	}
	return &machineapi.Event{Id: wireEventID(node), Data: &anypb.Any{TypeUrl: wireSequenceEventType, Value: value}}, nil
}

// startWireEndpoint serves a wireEndpoint on a unix socket and returns it with
// a real *talos.Client dialled to it (spec.md A7).
func startWireEndpoint(t *testing.T, failures map[string]string) (*wireEndpoint, *talos.Client) {
	t.Helper()

	e := &wireEndpoint{failures: failures}
	socket := wireSocketPath(t)

	lis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("setup: listen on %s: %v", socket, err)
	}

	srv := grpc.NewServer(grpc.UnknownServiceHandler(e.serve))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	c, err := talosclient.New(context.Background(),
		talosclient.WithUnixSocket(socket),
		talosclient.WithGRPCDialOptions(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	if err != nil {
		t.Fatalf("setup: dial fake endpoint: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	return e, &talos.Client{Client: c}
}

func wireSocketPath(t *testing.T) string {
	t.Helper()

	for _, base := range []string{os.TempDir(), "/tmp"} {
		dir, err := os.MkdirTemp(base, "tmcp")
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		if p := filepath.Join(dir, "apid.sock"); len(p) < wireMaxSocketPath {
			return p
		}
	}
	t.Fatalf("setup: no temp directory yields a unix socket path shorter than %d bytes", wireMaxSocketPath)
	return ""
}

func (e *wireEndpoint) record(r wireRequest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.requests = append(e.requests, r)
}

// recorded returns a snapshot of the requests received so far.
func (e *wireEndpoint) recorded() []wireRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.requests)
}

func (e *wireEndpoint) serve(_ any, stream grpc.ServerStream) error {
	method, _ := grpc.MethodFromServerStream(stream)
	md, _ := metadata.FromIncomingContext(stream.Context())
	e.record(wireRequest{method: method, node: slices.Clone(md.Get("node")), nodes: slices.Clone(md.Get("nodes"))})

	if err := stream.RecvMsg(&emptypb.Empty{}); err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	if newResp, ok := wireUnaryResponses[method]; ok {
		return e.answerUnary(stream, md, newResp)
	}
	if payload, ok := wireStreamPayloads[method]; ok {
		return e.answerStream(stream, md, payload)
	}
	return status.Errorf(codes.Unimplemented, "fake endpoint does not serve %s", method)
}

// wireTargets returns the nodes a request addresses and whether apid answers
// it one-to-many ("nodes" key) rather than one-to-one ("node" key or none).
func wireTargets(md metadata.MD) (targets []string, aggregated bool) {
	if nodes := md.Get("nodes"); len(nodes) > 0 {
		return nodes, true
	}
	if node := md.Get("node"); len(node) > 0 {
		return node[:1], false
	}
	return []string{""}, false
}

func (e *wireEndpoint) answerUnary(stream grpc.ServerStream, md metadata.MD, newResp func() proto.Message) error {
	targets, aggregated := wireTargets(md)
	if !aggregated {
		if msg, failed := e.failures[targets[0]]; failed {
			return status.Error(codes.Unavailable, msg)
		}
		return wireSendJSON(stream, newResp(), `{"messages":[{}]}`)
	}

	parts := make([]string, 0, len(targets))
	for _, node := range targets {
		parts = append(parts, `{"metadata":`+wireMetadataJSON(node, e.failures[node])+`}`)
	}
	return wireSendJSON(stream, newResp(), `{"messages":[`+strings.Join(parts, ",")+`]}`)
}

func (e *wireEndpoint) answerStream(stream grpc.ServerStream, md metadata.MD, payload func(node string) (proto.Message, error)) error {
	targets, aggregated := wireTargets(md)
	if !aggregated {
		if msg, failed := e.failures[targets[0]]; failed {
			return status.Error(codes.Unavailable, msg)
		}
		msg, err := payload(targets[0])
		if err != nil {
			return status.Errorf(codes.Internal, "fake endpoint: build payload: %v", err)
		}
		return stream.SendMsg(msg)
	}

	for _, node := range targets {
		msg, err := payload(node)
		if err != nil {
			return status.Errorf(codes.Internal, "fake endpoint: build payload: %v", err)
		}
		tagged, err := wireWithMetadata(msg, node, e.failures[node])
		if err != nil {
			return status.Errorf(codes.Internal, "fake endpoint: tag payload: %v", err)
		}
		if err := stream.SendMsg(tagged); err != nil {
			return err
		}
	}
	return nil
}

// wireMetadataJSON renders apid's per-node metadata as JSON for protojson,
// because common.Metadata is deprecated in machinery v1.14 (SA1019).
func wireMetadataJSON(hostname, errMsg string) string {
	if errMsg == "" {
		return fmt.Sprintf(`{"hostname":%q}`, hostname)
	}
	return fmt.Sprintf(`{"hostname":%q,"error":%q}`, hostname, errMsg)
}

// wireWithMetadata returns payload stamped with hostname, or a metadata-only
// message of the same type when errMsg is set (apid's failed-node message).
func wireWithMetadata(payload proto.Message, hostname, errMsg string) (proto.Message, error) {
	tagged := payload.ProtoReflect().New().Interface()
	if err := protojson.Unmarshal([]byte(`{"metadata":`+wireMetadataJSON(hostname, errMsg)+`}`), tagged); err != nil {
		return nil, err
	}
	if errMsg == "" {
		proto.Merge(tagged, payload)
	}
	return tagged, nil
}

func wireSendJSON(stream grpc.ServerStream, msg proto.Message, js string) error {
	if err := protojson.Unmarshal([]byte(js), msg); err != nil {
		return status.Errorf(codes.Internal, "fake endpoint: build reply: %v", err)
	}
	return stream.SendMsg(msg)
}

func wireHandlers(c *talos.Client) *Handlers {
	return &Handlers{Client: c, SkipVersionCheck: true}
}

func wireCtx(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), wireCallTimeout)
	t.Cleanup(cancel)
	return ctx
}

func wireAllowlist(t *testing.T, nodes ...string) *talos.NodeAllowlist {
	t.Helper()

	allow, err := talos.ParseNodeAllowlist(strings.Join(nodes, ","))
	if err != nil {
		t.Fatalf("setup: parse allowlist: %v", err)
	}
	return allow
}

// wireJSON reads a tool's structured result through its JSON form.
func wireJSON(t *testing.T, out any) map[string]any {
	t.Helper()

	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("setup: marshal tool result: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("setup: decode tool result %s: %v", b, err)
	}
	return m
}

// wireInOrder reports whether text contains each (node, message) pair, the
// node before its message, with the pairs in the given order.
func wireInOrder(text string, pairs ...[2]string) bool {
	pos := 0
	for _, pair := range pairs {
		for _, part := range pair {
			i := strings.Index(text[pos:], part)
			if i < 0 {
				return false
			}
			pos += i + len(part)
		}
	}
	return true
}

func wireTwoNodes() []string { return []string{wireNodeA, wireNodeB} }

// wireToolCase is one tool invocation against the fake endpoint.
type wireToolCase struct {
	id   string
	call func(ctx context.Context, h *Handlers) error
}

// checkSingleNodeRouting runs each case against a healthy endpoint and checks
// that every request carries exactly one "node" value and no "nodes" value, and
// that both wireNodeA and wireNodeB are targeted.
func checkSingleNodeRouting(t *testing.T, ac string, cases []wireToolCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			endpoint, client := startWireEndpoint(t, nil)
			callErr := tc.call(wireCtx(t), wireHandlers(client))

			reqs := endpoint.recorded()
			if len(reqs) == 0 {
				t.Fatalf("spec ac.md %s: case=%s expected=at least one request at the Talos API endpoint got=none (call error: %v)", ac, tc.id, callErr)
			}

			targeted := map[string]bool{}
			for _, r := range reqs {
				if len(r.node) != 1 || len(r.nodes) != 0 {
					t.Errorf("spec ac.md %s: case=%s expected=exactly one node metadata value and no nodes metadata got=%s node=%q nodes=%q", ac, tc.id, r.method, r.node, r.nodes)
					continue
				}
				targeted[r.node[0]] = true
			}
			for _, node := range wireTwoNodes() {
				if !targeted[node] {
					t.Errorf("spec ac.md %s: case=%s expected=%s targeted by at least one single-node request got=targets %v", ac, tc.id, node, targeted)
				}
			}
		})
	}
}

// checkOrderedFailure runs each case against an endpoint where the given nodes
// fail and checks that the call fails with the per-node errors in order.
func checkOrderedFailure(t *testing.T, ac string, failures map[string]string, order [][2]string, cases []wireToolCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			_, client := startWireEndpoint(t, failures)
			err := tc.call(wireCtx(t), wireHandlers(client))
			if err == nil {
				t.Fatalf("spec ac.md %s: case=%s expected=tool call fails got=success", ac, tc.id)
			}
			if !wireInOrder(err.Error(), order...) {
				t.Errorf("spec ac.md %s: case=%s expected=error text with node/message pairs in order %v got=%q", ac, tc.id, order, err.Error())
			}
		})
	}
}

// TestSpecAC12_GivenReadToolWithTwoNodes_WhenEndpointReceivesRequests_ThenEachRequestTargetsExactlyOneNode
// covers one predicate (ac.md AC-12): a read tool called with nodes=[A,B] reaches
// the endpoint only through single-node routing, and both nodes are targeted.
func TestSpecAC12_GivenReadToolWithTwoNodes_WhenEndpointReceivesRequests_ThenEachRequestTargetsExactlyOneNode(t *testing.T) {
	checkSingleNodeRouting(t, "AC-12", []wireToolCase{
		{id: "version", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleVersion(ctx, nil, VersionArgs{Nodes: wireTwoNodes()})
			return err
		}},
		{id: "services", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleServices(ctx, nil, NodesOnlyArgs{Nodes: wireTwoNodes()})
			return err
		}},
		{id: "containers", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleContainers(ctx, nil, ContainersArgs{Nodes: wireTwoNodes()})
			return err
		}},
		{id: "processes", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleProcesses(ctx, nil, NodesOnlyArgs{Nodes: wireTwoNodes()})
			return err
		}},
		{id: "etcd_members", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleEtcd(ctx, nil, EtcdArgs{Subcommand: "members", Nodes: wireTwoNodes()})
			return err
		}},
		{id: "etcd_status", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleEtcd(ctx, nil, EtcdArgs{Subcommand: "status", Nodes: wireTwoNodes()})
			return err
		}},
		{id: "list_files", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleListFiles(ctx, nil, ListFilesArgs{Path: "/etc", Nodes: wireTwoNodes()})
			return err
		}},
		{id: "logs", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleLogs(ctx, nil, LogsArgs{ServiceName: "kubelet", Nodes: wireTwoNodes()})
			return err
		}},
		{id: "dmesg", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleDmesg(ctx, nil, DmesgArgs{Nodes: wireTwoNodes()})
			return err
		}},
		{id: "events", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleEvents(ctx, nil, EventsArgs{Nodes: wireTwoNodes()})
			return err
		}},
		{id: "health", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleHealth(ctx, nil, HealthArgs{Nodes: wireTwoNodes()})
			return err
		}},
	})
}

// TestSpecAC13_GivenConfirmedMutatingToolWithTwoNodes_WhenEndpointReceivesRequests_ThenEachRequestTargetsExactlyOneNode
// covers one predicate (ac.md AC-13): a confirmed mutating tool called with
// nodes=[A,B] reaches the endpoint only through single-node routing to both nodes.
func TestSpecAC13_GivenConfirmedMutatingToolWithTwoNodes_WhenEndpointReceivesRequests_ThenEachRequestTargetsExactlyOneNode(t *testing.T) {
	checkSingleNodeRouting(t, "AC-13", []wireToolCase{
		{id: "service_action", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleServiceAction(ctx, nil, ServiceActionArgs{ServiceName: "kubelet", Action: "restart", Confirm: true, Nodes: wireTwoNodes()})
			return err
		}},
		{id: "reboot", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleReboot(ctx, nil, RebootArgs{Nodes: wireTwoNodes(), Confirm: true})
			return err
		}},
		{id: "upgrade", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleUpgrade(ctx, nil, UpgradeArgs{Nodes: wireTwoNodes(), Image: "ghcr.io/siderolabs/installer:v1.14.5", Confirm: true})
			return err
		}},
		{id: "rollback", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleRollback(ctx, nil, RollbackArgs{Nodes: wireTwoNodes(), Confirm: true})
			return err
		}},
		{id: "reset", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleReset(ctx, nil, ResetArgs{Nodes: wireTwoNodes(), Confirm: true})
			return err
		}},
		{id: "meta_write", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleMeta(ctx, nil, MetaArgs{Action: "write", Key: wireMetaKey, Value: "v", Confirm: true, Nodes: wireTwoNodes()})
			return err
		}},
		{id: "meta_delete", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleMeta(ctx, nil, MetaArgs{Action: "delete", Key: wireMetaKey, Confirm: true, Nodes: wireTwoNodes()})
			return err
		}},
	})
}

// TestSpecAC25_GivenMetaReadWithTwoAllowedNodes_WhenCalled_ThenFailsWithoutRequest_AND_AuditsArgsInvalid
// covers ac.md AC-25: the multi-node COSI read is refused as an argument error
// (spec.md A9) before any request leaves, and its paired AUDIT_OUTCOME says so.
func TestSpecAC25_GivenMetaReadWithTwoAllowedNodes_WhenCalled_ThenFailsWithoutRequest_AND_AuditsArgsInvalid(t *testing.T) {
	logs := captureDefaultSlog(t)
	endpoint, client := startWireEndpoint(t, nil)
	h := wireHandlers(client)
	h.AllowedNodes = wireAllowlist(t, wireNodeA, wireNodeB)

	_, _, err := h.HandleMeta(wireCtx(t), nil, MetaArgs{Action: "read", Key: wireMetaKey, Nodes: wireTwoNodes()})

	if err == nil {
		t.Errorf("spec ac.md AC-25: expected=talos_meta read with nodes=[A,B] fails got=success")
	}
	if reqs := endpoint.recorded(); len(reqs) != 0 {
		t.Errorf("spec ac.md AC-25: expected=zero requests at the Talos API endpoint got=%d %v", len(reqs), reqs)
	}
	if got := auditOutcomeOf(logs.String(), "talos_meta"); got != OutcomeRefusedArgs {
		t.Errorf("spec ac.md AC-25: expected=AUDIT_OUTCOME outcome %q got=%q", OutcomeRefusedArgs, got)
	}
}

// TestSpecAC26_GivenReadFileWithTwoNodes_WhenCalled_ThenFailsRequiringExactlyOneNode_AND_SendsNoRequest
// covers ac.md AC-26: talos_read_file refuses more than one node before any
// request leaves.
func TestSpecAC26_GivenReadFileWithTwoNodes_WhenCalled_ThenFailsRequiringExactlyOneNode_AND_SendsNoRequest(t *testing.T) {
	endpoint, client := startWireEndpoint(t, nil)

	_, _, err := wireHandlers(client).HandleReadFile(wireCtx(t), nil, ReadFileArgs{Path: "/etc/os-release", Nodes: wireTwoNodes()})

	if err == nil || !wireExactlyOneNode.MatchString(err.Error()) {
		t.Errorf("spec ac.md AC-26: expected=error stating exactly one node is required got=%v", err)
	}
	if reqs := endpoint.recorded(); len(reqs) != 0 {
		t.Errorf("spec ac.md AC-26: expected=zero requests at the Talos API endpoint got=%d %v", len(reqs), reqs)
	}
}

// TestSpecAC33_GivenEventsWithOneFailingNode_WhenWindowEnds_ThenSurvivorEventKept_AND_FailureInNodeErrors
// covers ac.md AC-33: talos_events keeps the surviving node's event attributed
// to it and reports the failing node under node_errors (spec.md A2).
func TestSpecAC33_GivenEventsWithOneFailingNode_WhenWindowEnds_ThenSurvivorEventKept_AND_FailureInNodeErrors(t *testing.T) {
	_, client := startWireEndpoint(t, map[string]string{wireNodeB: wireErrB})

	_, out, err := wireHandlers(client).HandleEvents(wireCtx(t), nil, EventsArgs{Nodes: wireTwoNodes()})
	if err != nil {
		t.Fatalf("spec ac.md AC-33: expected=call succeeds got=%v", err)
	}
	res := wireJSON(t, out)

	events, _ := res["events"].([]any)
	found := false
	for _, ev := range events {
		entry, _ := ev.(map[string]any)
		if entry["id"] == wireEventID(wireNodeA) && entry["node"] == wireNodeA {
			found = true
		}
	}
	if !found {
		t.Errorf("spec ac.md AC-33: expected=event %q with node %q got=events %v", wireEventID(wireNodeA), wireNodeA, res["events"])
	}

	nodeErrs, _ := res["node_errors"].(map[string]any)
	if got, _ := nodeErrs[wireNodeB].(string); !strings.Contains(got, wireErrB) {
		t.Errorf("spec ac.md AC-33: expected=node_errors[%q] containing %q got=node_errors %v", wireNodeB, wireErrB, res["node_errors"])
	}
}

// TestSpecAC34_GivenStreamToolWithEveryNodeFailing_WhenCalled_ThenFailsWithNodeErrorsInRequestOrder
// covers one predicate (ac.md AC-34; spec.md A2 "every node fails"): the call
// fails and names A with MA before B with MB.
func TestSpecAC34_GivenStreamToolWithEveryNodeFailing_WhenCalled_ThenFailsWithNodeErrorsInRequestOrder(t *testing.T) {
	failures := map[string]string{wireNodeA: wireErrA, wireNodeB: wireErrB}
	order := [][2]string{{wireNodeA, wireErrA}, {wireNodeB, wireErrB}}

	checkOrderedFailure(t, "AC-34", failures, order, []wireToolCase{
		{id: "logs", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleLogs(ctx, nil, LogsArgs{ServiceName: "kubelet", Nodes: wireTwoNodes()})
			return err
		}},
		{id: "dmesg", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleDmesg(ctx, nil, DmesgArgs{Nodes: wireTwoNodes()})
			return err
		}},
		{id: "list_files", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleListFiles(ctx, nil, ListFilesArgs{Path: "/etc", Nodes: wireTwoNodes()})
			return err
		}},
		{id: "events", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleEvents(ctx, nil, EventsArgs{Nodes: wireTwoNodes()})
			return err
		}},
		{id: "health", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleHealth(ctx, nil, HealthArgs{Nodes: wireTwoNodes()})
			return err
		}},
	})
}

// TestSpecAC40_GivenHealthWithTwoOfThreeNodesFailing_WhenCalled_ThenFailsClosedWithNodeErrorsInRequestOrder
// covers one predicate (ac.md AC-40; spec.md A8): talos_health fails when any
// requested node's stream fails, naming B with MB before C with MC.
func TestSpecAC40_GivenHealthWithTwoOfThreeNodesFailing_WhenCalled_ThenFailsClosedWithNodeErrorsInRequestOrder(t *testing.T) {
	failures := map[string]string{wireNodeB: wireErrB, wireNodeC: wireErrC}
	order := [][2]string{{wireNodeB, wireErrB}, {wireNodeC, wireErrC}}

	checkOrderedFailure(t, "AC-40", failures, order, []wireToolCase{
		{id: "health_one_of_three_passes", call: func(ctx context.Context, h *Handlers) error {
			_, _, err := h.HandleHealth(ctx, nil, HealthArgs{Nodes: []string{wireNodeA, wireNodeB, wireNodeC}})
			return err
		}},
	})
}
