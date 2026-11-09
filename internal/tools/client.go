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

// Client is the Talos API surface the tool handlers call through Handlers.Client.
type Client interface {
	Version(ctx context.Context, callOptions ...grpc.CallOption) (*machineapi.VersionResponse, error)
	ServiceList(ctx context.Context, callOptions ...grpc.CallOption) (*machineapi.ServiceListResponse, error)
	ServiceStart(ctx context.Context, id string, callOptions ...grpc.CallOption) (*machineapi.ServiceStartResponse, error)
	ServiceStop(ctx context.Context, id string, callOptions ...grpc.CallOption) (*machineapi.ServiceStopResponse, error)
	ServiceRestart(ctx context.Context, id string, callOptions ...grpc.CallOption) (*machineapi.ServiceRestartResponse, error)
	Containers(ctx context.Context, namespace string, driver commonapi.ContainerDriver, callOptions ...grpc.CallOption) (*machineapi.ContainersResponse, error)
	Processes(ctx context.Context, callOptions ...grpc.CallOption) (*machineapi.ProcessesResponse, error)
	ClusterHealthCheck(ctx context.Context, waitTimeout time.Duration, clusterInfo *clusterapi.ClusterInfo) (clusterapi.ClusterService_HealthCheckClient, error)
	RebootNodes(ctx context.Context, mode machineapi.RebootRequest_Mode) error
	UpgradeNodes(ctx context.Context, req *machineapi.UpgradeRequest) (*machineapi.UpgradeResponse, error)
	Rollback(ctx context.Context) error
	ApplyConfiguration(ctx context.Context, req *machineapi.ApplyConfigurationRequest, callOptions ...grpc.CallOption) (*machineapi.ApplyConfigurationResponse, error)
	MetaWrite(ctx context.Context, key uint8, value []byte, callOptions ...grpc.CallOption) error
	MetaDelete(ctx context.Context, key uint8, callOptions ...grpc.CallOption) error
	ResetGenericWithResponse(ctx context.Context, req *machineapi.ResetRequest) (*machineapi.ResetResponse, error)
	Logs(ctx context.Context, namespace string, driver commonapi.ContainerDriver, id string, follow bool, tailLines int32) (machineapi.MachineService_LogsClient, error)
	Dmesg(ctx context.Context, follow, tail bool) (machineapi.MachineService_DmesgClient, error)
	WatchEvents(ctx context.Context, tail int32, watch func(<-chan talosclient.Event)) error
	LS(ctx context.Context, req *machineapi.ListRequest) (machineapi.MachineService_ListClient, error)
	Read(ctx context.Context, path string) (io.ReadCloser, error)
	EtcdMemberList(ctx context.Context, req *machineapi.EtcdMemberListRequest, callOptions ...grpc.CallOption) (*machineapi.EtcdMemberListResponse, error)
	EtcdStatus(ctx context.Context, callOptions ...grpc.CallOption) (*machineapi.EtcdStatusResponse, error)
	EtcdSnapshot(ctx context.Context, req *machineapi.EtcdSnapshotRequest, callOptions ...grpc.CallOption) (io.ReadCloser, error)
	ResolveResourceKind(ctx context.Context, resourceNamespace *resource.Namespace, resourceType resource.Type) (*meta.ResourceDefinition, error)
	COSIState() state.State
	GetNodeVersion(ctx context.Context, node string) (*version.TalosVersion, error)
	InvalidateVersionCache()
}

// maintenanceClient is the surface the insecure (maintenance-mode) paths call
// on a client returned by Handlers.dialInsecure.
type maintenanceClient interface {
	Close() error
	Version(ctx context.Context, callOptions ...grpc.CallOption) (*machineapi.VersionResponse, error)
	ApplyConfiguration(ctx context.Context, req *machineapi.ApplyConfigurationRequest, callOptions ...grpc.CallOption) (*machineapi.ApplyConfigurationResponse, error)
	MetaWrite(ctx context.Context, key uint8, value []byte, callOptions ...grpc.CallOption) error
	MetaDelete(ctx context.Context, key uint8, callOptions ...grpc.CallOption) error
	ResolveResourceKind(ctx context.Context, resourceNamespace *resource.Namespace, resourceType resource.Type) (*meta.ResourceDefinition, error)
	COSIState() state.State
}

var (
	_ Client            = (*talos.Client)(nil)
	_ maintenanceClient = (*talos.Client)(nil)
)
