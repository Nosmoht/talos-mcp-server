package tools

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/state"
	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	commonapi "github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc"

	"github.com/Nosmoht/talos-mcp-server/internal/version"
)

// mockClient is a test double for Client whose methods panic, so a guard test
// that reaches the gRPC layer fails. A record* flag makes the matching call
// (Upgrade includes InvalidateVersionCache) store its argument in got* instead.
type mockClient struct {
	recordReboot  bool
	recordUpgrade bool
	recordEvents  bool

	gotRebootMode machineapi.RebootRequest_Mode
	gotUpgradeReq *machineapi.UpgradeRequest
	gotEventsTail int32
}

var _ Client = (*mockClient)(nil)

func (m *mockClient) Version(_ context.Context, _ ...grpc.CallOption) (*machineapi.VersionResponse, error) {
	panic("mockClient.Version called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) ServiceList(_ context.Context, _ ...grpc.CallOption) (*machineapi.ServiceListResponse, error) {
	panic("mockClient.ServiceList called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) ServiceStart(_ context.Context, _ string, _ ...grpc.CallOption) (*machineapi.ServiceStartResponse, error) {
	panic("mockClient.ServiceStart called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) ServiceStop(_ context.Context, _ string, _ ...grpc.CallOption) (*machineapi.ServiceStopResponse, error) {
	panic("mockClient.ServiceStop called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) ServiceRestart(_ context.Context, _ string, _ ...grpc.CallOption) (*machineapi.ServiceRestartResponse, error) {
	panic("mockClient.ServiceRestart called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) Containers(_ context.Context, _ string, _ commonapi.ContainerDriver, _ ...grpc.CallOption) (*machineapi.ContainersResponse, error) {
	panic("mockClient.Containers called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) Processes(_ context.Context, _ ...grpc.CallOption) (*machineapi.ProcessesResponse, error) {
	panic("mockClient.Processes called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) ClusterHealthCheck(_ context.Context, _ time.Duration, _ *clusterapi.ClusterInfo) (clusterapi.ClusterService_HealthCheckClient, error) {
	panic("mockClient.ClusterHealthCheck called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) Reboot(_ context.Context, mode machineapi.RebootRequest_Mode) error {
	if !m.recordReboot {
		panic("mockClient.Reboot called — test reached gRPC layer unexpectedly")
	}
	m.gotRebootMode = mode
	return nil
}

func (m *mockClient) Upgrade(_ context.Context, req *machineapi.UpgradeRequest) (*machineapi.UpgradeResponse, error) {
	if !m.recordUpgrade {
		panic("mockClient.Upgrade called — test reached gRPC layer unexpectedly")
	}
	m.gotUpgradeReq = req
	return &machineapi.UpgradeResponse{}, nil
}

func (m *mockClient) Rollback(_ context.Context) error {
	panic("mockClient.Rollback called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) ApplyConfiguration(_ context.Context, _ *machineapi.ApplyConfigurationRequest, _ ...grpc.CallOption) (*machineapi.ApplyConfigurationResponse, error) {
	panic("mockClient.ApplyConfiguration called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) MetaWrite(_ context.Context, _ uint8, _ []byte, _ ...grpc.CallOption) error {
	panic("mockClient.MetaWrite called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) MetaDelete(_ context.Context, _ uint8, _ ...grpc.CallOption) error {
	panic("mockClient.MetaDelete called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) ResetGenericWithResponse(_ context.Context, _ *machineapi.ResetRequest) (*machineapi.ResetResponse, error) {
	panic("mockClient.ResetGenericWithResponse called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) Logs(_ context.Context, _ string, _ commonapi.ContainerDriver, _ string, _ bool, _ int32) (machineapi.MachineService_LogsClient, error) {
	panic("mockClient.Logs called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) Dmesg(_ context.Context, _, _ bool) (machineapi.MachineService_DmesgClient, error) {
	panic("mockClient.Dmesg called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) EventsWatch(_ context.Context, tail int32, _ func(<-chan talosclient.Event)) error {
	if !m.recordEvents {
		panic("mockClient.EventsWatch called — test reached gRPC layer unexpectedly")
	}
	m.gotEventsTail = tail
	return nil
}

func (m *mockClient) LS(_ context.Context, _ *machineapi.ListRequest) (machineapi.MachineService_ListClient, error) {
	panic("mockClient.LS called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) Read(_ context.Context, _ string) (io.ReadCloser, error) {
	panic("mockClient.Read called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) EtcdMemberList(_ context.Context, _ *machineapi.EtcdMemberListRequest, _ ...grpc.CallOption) (*machineapi.EtcdMemberListResponse, error) {
	panic("mockClient.EtcdMemberList called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) EtcdStatus(_ context.Context, _ ...grpc.CallOption) (*machineapi.EtcdStatusResponse, error) {
	panic("mockClient.EtcdStatus called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) EtcdSnapshot(_ context.Context, _ *machineapi.EtcdSnapshotRequest, _ ...grpc.CallOption) (io.ReadCloser, error) {
	panic("mockClient.EtcdSnapshot called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) ResolveResourceKind(_ context.Context, _ *resource.Namespace, _ resource.Type) (*meta.ResourceDefinition, error) {
	panic("mockClient.ResolveResourceKind called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) COSIState() state.State {
	panic("mockClient.COSIState called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) GetNodeVersion(_ context.Context, _ string) (*version.TalosVersion, error) {
	panic("mockClient.GetNodeVersion called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) InvalidateVersionCache() {
	if !m.recordUpgrade {
		panic("mockClient.InvalidateVersionCache called — test reached gRPC layer unexpectedly")
	}
}

// fakeMaintenanceClient is the maintenanceClient a recordingDialer hands to
// the insecure paths. It records the requests it receives and returns rpcErr
// from every RPC when set.
type fakeMaintenanceClient struct {
	rpcErr  error
	version *machineapi.VersionResponse
	state   state.State
	kinds   map[resource.Type]*meta.ResourceDefinition

	gotApply          *machineapi.ApplyConfigurationRequest
	gotMetaWriteKey   *uint8
	gotMetaWriteValue []byte
	gotMetaDeleteKey  *uint8
	closed            bool
}

var _ maintenanceClient = (*fakeMaintenanceClient)(nil)

func (f *fakeMaintenanceClient) Close() error {
	f.closed = true
	return nil
}

func (f *fakeMaintenanceClient) Version(_ context.Context, _ ...grpc.CallOption) (*machineapi.VersionResponse, error) {
	if f.rpcErr != nil {
		return nil, f.rpcErr
	}
	return f.version, nil
}

func (f *fakeMaintenanceClient) ApplyConfiguration(_ context.Context, req *machineapi.ApplyConfigurationRequest, _ ...grpc.CallOption) (*machineapi.ApplyConfigurationResponse, error) {
	f.gotApply = req
	if f.rpcErr != nil {
		return nil, f.rpcErr
	}
	return &machineapi.ApplyConfigurationResponse{}, nil
}

func (f *fakeMaintenanceClient) MetaWrite(_ context.Context, key uint8, value []byte, _ ...grpc.CallOption) error {
	f.gotMetaWriteKey = &key
	f.gotMetaWriteValue = value
	return f.rpcErr
}

func (f *fakeMaintenanceClient) MetaDelete(_ context.Context, key uint8, _ ...grpc.CallOption) error {
	f.gotMetaDeleteKey = &key
	return f.rpcErr
}

func (f *fakeMaintenanceClient) ResolveResourceKind(_ context.Context, _ *resource.Namespace, resourceType resource.Type) (*meta.ResourceDefinition, error) {
	if rd, ok := f.kinds[resourceType]; ok {
		return rd, nil
	}
	return nil, fmt.Errorf("unknown resource type %q", resourceType)
}

func (f *fakeMaintenanceClient) COSIState() state.State { return f.state }

// recordingDialer replaces Handlers.dialMaintenance and records what the
// insecure paths dial.
type recordingDialer struct {
	client      maintenanceClient
	calls       int
	endpoint    string
	fingerprint []byte
}

func (d *recordingDialer) dial(_ context.Context, endpoint string, fingerprint []byte) (maintenanceClient, error) {
	d.calls++
	d.endpoint = endpoint
	d.fingerprint = fingerprint
	return d.client, nil
}
