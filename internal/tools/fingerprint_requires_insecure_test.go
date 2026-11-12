package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	commonapi "github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc"

	"github.com/Nosmoht/talos-mcp-server/internal/version"
)

const (
	fingerprintRefusalText = "cert_fingerprint requires insecure=true"
	fingerprintProperty    = "cert_fingerprint"
)

var (
	errFingerprintProbe = errors.New("fingerprint probe: Talos client reached")

	fingerprintToolsNamedBySpec = []string{"talos_apply_config", "talos_get", "talos_meta", "talos_version"}
)

type fingerprintProbeClient struct {
	mu      sync.Mutex
	reached []string
	st      state.State
}

var _ Client = (*fingerprintProbeClient)(nil)

func (c *fingerprintProbeClient) hit(method string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reached = append(c.reached, method)
}

func (c *fingerprintProbeClient) reachedSince(n int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.reached[n:]...)
}

func (c *fingerprintProbeClient) reachedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reached)
}

func (c *fingerprintProbeClient) Version(_ context.Context, _ ...grpc.CallOption) (*machineapi.VersionResponse, error) {
	c.hit("Version")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) ServiceList(_ context.Context, _ ...grpc.CallOption) (*machineapi.ServiceListResponse, error) {
	c.hit("ServiceList")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) ServiceStart(_ context.Context, _ string, _ ...grpc.CallOption) (*machineapi.ServiceStartResponse, error) {
	c.hit("ServiceStart")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) ServiceStop(_ context.Context, _ string, _ ...grpc.CallOption) (*machineapi.ServiceStopResponse, error) {
	c.hit("ServiceStop")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) ServiceRestart(_ context.Context, _ string, _ ...grpc.CallOption) (*machineapi.ServiceRestartResponse, error) {
	c.hit("ServiceRestart")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) Containers(_ context.Context, _ string, _ commonapi.ContainerDriver, _ ...grpc.CallOption) (*machineapi.ContainersResponse, error) {
	c.hit("Containers")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) Processes(_ context.Context, _ ...grpc.CallOption) (*machineapi.ProcessesResponse, error) {
	c.hit("Processes")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) ClusterHealthCheck(_ context.Context, _ time.Duration, _ *clusterapi.ClusterInfo) (clusterapi.ClusterService_HealthCheckClient, error) {
	c.hit("ClusterHealthCheck")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) RebootNodes(_ context.Context, _ machineapi.RebootRequest_Mode) error {
	c.hit("RebootNodes")
	return errFingerprintProbe
}

func (c *fingerprintProbeClient) UpgradeNodes(_ context.Context, _ *machineapi.UpgradeRequest) (*machineapi.UpgradeResponse, error) {
	c.hit("UpgradeNodes")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) Rollback(_ context.Context) error {
	c.hit("Rollback")
	return errFingerprintProbe
}

func (c *fingerprintProbeClient) ApplyConfiguration(_ context.Context, _ *machineapi.ApplyConfigurationRequest, _ ...grpc.CallOption) (*machineapi.ApplyConfigurationResponse, error) {
	c.hit("ApplyConfiguration")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) MetaWrite(_ context.Context, _ uint8, _ []byte, _ ...grpc.CallOption) error {
	c.hit("MetaWrite")
	return errFingerprintProbe
}

func (c *fingerprintProbeClient) MetaDelete(_ context.Context, _ uint8, _ ...grpc.CallOption) error {
	c.hit("MetaDelete")
	return errFingerprintProbe
}

func (c *fingerprintProbeClient) ResetGenericWithResponse(_ context.Context, _ *machineapi.ResetRequest) (*machineapi.ResetResponse, error) {
	c.hit("ResetGenericWithResponse")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) Logs(_ context.Context, _ string, _ commonapi.ContainerDriver, _ string, _ bool, _ int32) (machineapi.MachineService_LogsClient, error) {
	c.hit("Logs")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) Dmesg(_ context.Context, _, _ bool) (machineapi.MachineService_DmesgClient, error) {
	c.hit("Dmesg")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) WatchEvents(_ context.Context, _ int32, _ func(<-chan talosclient.Event)) error {
	c.hit("WatchEvents")
	return errFingerprintProbe
}

func (c *fingerprintProbeClient) LS(_ context.Context, _ *machineapi.ListRequest) (machineapi.MachineService_ListClient, error) {
	c.hit("LS")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) Read(_ context.Context, _ string) (io.ReadCloser, error) {
	c.hit("Read")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) EtcdMemberList(_ context.Context, _ *machineapi.EtcdMemberListRequest, _ ...grpc.CallOption) (*machineapi.EtcdMemberListResponse, error) {
	c.hit("EtcdMemberList")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) EtcdStatus(_ context.Context, _ ...grpc.CallOption) (*machineapi.EtcdStatusResponse, error) {
	c.hit("EtcdStatus")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) EtcdSnapshot(_ context.Context, _ *machineapi.EtcdSnapshotRequest, _ ...grpc.CallOption) (io.ReadCloser, error) {
	c.hit("EtcdSnapshot")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) ResolveResourceKind(_ context.Context, _ *resource.Namespace, _ resource.Type) (*meta.ResourceDefinition, error) {
	c.hit("ResolveResourceKind")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) COSIState() state.State {
	c.hit("COSIState")
	return c.st
}

func (c *fingerprintProbeClient) GetNodeVersion(_ context.Context, _ string) (*version.TalosVersion, error) {
	c.hit("GetNodeVersion")
	return nil, errFingerprintProbe
}

func (c *fingerprintProbeClient) InvalidateVersionCache() {
	c.hit("InvalidateVersionCache")
}

func connectFingerprintSession(t *testing.T, client Client) *mcp.ClientSession {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "fingerprint-test", Version: "0.0.0"}, nil)
	Register(server, &Handlers{Client: client}, false)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_ = server.Run(ctx, serverTransport)
	}()

	session, err := mcp.NewClient(&mcp.Implementation{Name: "fingerprint-client", Version: "0.0.0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("setup: client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

type fingerprintToolSchema struct {
	Properties map[string]struct {
		Type any `json:"type"`
	} `json:"properties"`
	Required []string `json:"required"`
}

func decodeFingerprintToolSchema(t *testing.T, tool *mcp.Tool) fingerprintToolSchema {
	t.Helper()

	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("setup: marshal %s input schema: %v", tool.Name, err)
	}
	var s fingerprintToolSchema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("setup: decode %s input schema: %v", tool.Name, err)
	}
	return s
}

func placeholderForSchemaType(typ any) any {
	names := []string{}
	switch v := typ.(type) {
	case string:
		names = append(names, v)
	case []any:
		for _, n := range v {
			if s, ok := n.(string); ok && s != "null" {
				names = append(names, s)
			}
		}
	}
	if len(names) == 0 {
		return "x"
	}
	switch names[0] {
	case "array":
		return []any{}
	case "boolean":
		return false
	case "integer", "number":
		return 0
	case "object":
		return map[string]any{}
	default:
		return "x"
	}
}

func fingerprintCallArguments(s fingerprintToolSchema) map[string]any {
	args := map[string]any{}
	for _, name := range s.Required {
		args[name] = placeholderForSchemaType(s.Properties[name].Type)
	}
	args[fingerprintProperty] = strings.Repeat("ab", 32)
	args["insecure"] = false
	return args
}

func toolResultText(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func TestFingerprintWithoutInsecureRefusedByEveryTool(t *testing.T) {
	probe := &fingerprintProbeClient{st: state.WrapCore(namespaced.NewState(inmem.Build))}
	session := connectFingerprintSession(t, probe)

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("setup: tools/list: %v", err)
	}

	schemas := map[string]fingerprintToolSchema{}
	for _, tool := range listed.Tools {
		s := decodeFingerprintToolSchema(t, tool)
		if _, ok := s.Properties[fingerprintProperty]; ok {
			schemas[tool.Name] = s
		}
	}
	for _, name := range fingerprintToolsNamedBySpec {
		if _, ok := schemas[name]; !ok {
			t.Fatalf("spec AC-6: precondition expected %s to declare %s got tools %v", name, fingerprintProperty, sortedKeys(schemas))
		}
	}

	for _, name := range sortedKeys(schemas) {
		t.Run(name, func(t *testing.T) {
			before := probe.reachedCount()
			res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      name,
				Arguments: fingerprintCallArguments(schemas[name]),
			})
			if err != nil {
				t.Fatalf("spec AC-6: case=%s precondition expected a tool result got protocol error %v", name, err)
			}

			text := toolResultText(res)
			reached := probe.reachedSince(before)
			if !res.IsError || !strings.Contains(text, fingerprintRefusalText) || len(reached) != 0 {
				t.Errorf("spec AC-6: case=%s expected=error containing %q with no Talos client call got=isError:%t text:%q client calls:%v",
					name, fingerprintRefusalText, res.IsError, text, reached)
			}
		})
	}
}

func sortedKeys(m map[string]fingerprintToolSchema) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
