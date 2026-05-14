package talos

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

	"github.com/Nosmoht/talos-mcp-server/internal/version"
)

// ClientInterface defines the subset of Talos client methods used by the MCP tool
// handlers. It exists so that handler unit tests can inject a stub or mock client
// without requiring a live Talos cluster.
//
// All methods listed here are called by at least one handler in internal/tools.
// If new methods are added to handlers they must be added here and to any mocks
// used in tests.
type ClientInterface interface {
	// Version returns node version information.
	Version(ctx context.Context, callOptions ...grpc.CallOption) (*machineapi.VersionResponse, error)

	// ServiceList returns the list of services and their state.
	ServiceList(ctx context.Context, callOptions ...grpc.CallOption) (*machineapi.ServiceListResponse, error)

	// ServiceStart starts the named service.
	ServiceStart(ctx context.Context, id string, callOptions ...grpc.CallOption) (*machineapi.ServiceStartResponse, error)

	// ServiceStop stops the named service.
	ServiceStop(ctx context.Context, id string, callOptions ...grpc.CallOption) (*machineapi.ServiceStopResponse, error)

	// ServiceRestart restarts the named service.
	ServiceRestart(ctx context.Context, id string, callOptions ...grpc.CallOption) (*machineapi.ServiceRestartResponse, error)

	// Containers returns the list of CRI containers in the given namespace.
	Containers(ctx context.Context, namespace string, driver commonapi.ContainerDriver, callOptions ...grpc.CallOption) (*machineapi.ContainersResponse, error)

	// Processes returns the list of running processes.
	Processes(ctx context.Context, callOptions ...grpc.CallOption) (*machineapi.ProcessesResponse, error)

	// ClusterHealthCheck streams cluster health-check results.
	ClusterHealthCheck(ctx context.Context, waitTimeout time.Duration, clusterInfo *clusterapi.ClusterInfo) (clusterapi.ClusterService_HealthCheckClient, error)

	// Reboot reboots the target node(s).
	Reboot(ctx context.Context, opts ...talosclient.RebootMode) error

	// UpgradeWithOptions initiates a Talos upgrade with the given options.
	UpgradeWithOptions(ctx context.Context, opts ...talosclient.UpgradeOption) (*machineapi.UpgradeResponse, error)

	// Rollback rolls back to the previous Talos installation.
	Rollback(ctx context.Context) error

	// ApplyConfiguration applies a machine configuration to the target node.
	ApplyConfiguration(ctx context.Context, req *machineapi.ApplyConfigurationRequest, callOptions ...grpc.CallOption) (*machineapi.ApplyConfigurationResponse, error)

	// MetaWrite writes a value to the META partition under the given uint8 key.
	// Used by talos_meta in authenticated mode.
	MetaWrite(ctx context.Context, key uint8, value []byte, callOptions ...grpc.CallOption) error

	// MetaDelete deletes the META partition entry for the given uint8 key.
	// Used by talos_meta in authenticated mode.
	MetaDelete(ctx context.Context, key uint8, callOptions ...grpc.CallOption) error

	// ResetGenericWithResponse wipes and factory-resets the target node(s).
	ResetGenericWithResponse(ctx context.Context, req *machineapi.ResetRequest) (*machineapi.ResetResponse, error)

	// Logs streams service log lines.
	Logs(ctx context.Context, namespace string, driver commonapi.ContainerDriver, id string, follow bool, tailLines int32) (machineapi.MachineService_LogsClient, error)

	// Dmesg streams kernel ring-buffer messages.
	Dmesg(ctx context.Context, follow, tail bool) (machineapi.MachineService_DmesgClient, error)

	// EventsWatch streams Talos runtime events via a callback.
	EventsWatch(ctx context.Context, watchFunc func(<-chan talosclient.Event), opts ...talosclient.EventsOptionFunc) error

	// LS streams a directory listing from the node filesystem.
	LS(ctx context.Context, req *machineapi.ListRequest) (machineapi.MachineService_ListClient, error)

	// Read streams the contents of a file from the node filesystem.
	Read(ctx context.Context, path string) (io.ReadCloser, error)

	// EtcdMemberList returns the list of etcd cluster members.
	EtcdMemberList(ctx context.Context, req *machineapi.EtcdMemberListRequest, callOptions ...grpc.CallOption) (*machineapi.EtcdMemberListResponse, error)

	// EtcdStatus returns the etcd cluster status.
	EtcdStatus(ctx context.Context, opts ...grpc.CallOption) (*machineapi.EtcdStatusResponse, error)

	// EtcdSnapshot streams an etcd snapshot to the caller.
	EtcdSnapshot(ctx context.Context, req *machineapi.EtcdSnapshotRequest, callOptions ...grpc.CallOption) (io.ReadCloser, error)

	// ResolveResourceKind resolves a potentially-aliased resource type and fills in
	// the default namespace when resourceNamespace is empty.
	ResolveResourceKind(ctx context.Context, resourceNamespace *resource.Namespace, resourceType resource.Type) (*meta.ResourceDefinition, error)

	// COSIState returns the COSI state accessor used for resource queries.
	// Handlers use this to call Get and List on the COSI state.
	COSIState() state.State

	// GetNodeVersion fetches the Talos version from a specific node (no cache).
	GetNodeVersion(ctx context.Context, node string) (*version.TalosVersion, error)

	// GetClusterVersion returns the cached cluster version, fetching it on first call.
	GetClusterVersion(ctx context.Context) (*version.TalosVersion, error)

	// InvalidateVersionCache clears the cached cluster version.
	InvalidateVersionCache()

	// Ping verifies liveness of the gRPC connection to the default endpoint.
	Ping(ctx context.Context) error
}
