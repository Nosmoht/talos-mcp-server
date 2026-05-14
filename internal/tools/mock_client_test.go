package tools

import (
	"context"
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

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
	"github.com/Nosmoht/talos-mcp-server/internal/version"
)

// mockClient is a test double for talos.ClientInterface.
// All methods panic with a descriptive message — guard-logic tests must
// return before any gRPC call; if a test reaches a method here it indicates
// the guard condition is missing or wrong.
type mockClient struct{}

var _ talos.ClientInterface = (*mockClient)(nil)

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

func (m *mockClient) Reboot(_ context.Context, _ ...talosclient.RebootMode) error {
	panic("mockClient.Reboot called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) UpgradeWithOptions(_ context.Context, _ ...talosclient.UpgradeOption) (*machineapi.UpgradeResponse, error) {
	panic("mockClient.UpgradeWithOptions called — test reached gRPC layer unexpectedly")
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

func (m *mockClient) EventsWatch(_ context.Context, _ func(<-chan talosclient.Event), _ ...talosclient.EventsOptionFunc) error {
	panic("mockClient.EventsWatch called — test reached gRPC layer unexpectedly")
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

func (m *mockClient) GetClusterVersion(_ context.Context) (*version.TalosVersion, error) {
	panic("mockClient.GetClusterVersion called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) InvalidateVersionCache() {
	panic("mockClient.InvalidateVersionCache called — test reached gRPC layer unexpectedly")
}

func (m *mockClient) Ping(_ context.Context) error {
	panic("mockClient.Ping called — test reached gRPC layer unexpectedly")
}
