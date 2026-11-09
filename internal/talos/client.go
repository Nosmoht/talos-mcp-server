// Package talos wraps the Talos Linux machinery client with helpers for the MCP server.
package talos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cosi-project/runtime/pkg/state"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/machinery/client/multiplex"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"

	"github.com/Nosmoht/talos-mcp-server/internal/version"
)

// dialTimeout is the maximum time allowed for the initial gRPC connection
// setup in NewClient. It prevents an indefinite hang when the Talos cluster
// is unreachable at startup.
const dialTimeout = 30 * time.Second

// grpcKeepalive configures TCP keepalive probes on the gRPC connection.
// Probes are sent every 30 s on idle connections; the connection is closed
// if no response arrives within 10 s. PermitWithoutStream ensures that idle
// connections (no active RPCs) are also probed, which is the common case for
// the MCP server between tool calls.
//
// This enables gRPC's built-in reconnect logic to trigger quickly after a
// network blip, firewall idle-timeout drop, or Talos endpoint restart — rather
// than silently failing all subsequent tool calls until the server is restarted.
var grpcKeepalive = grpc.WithKeepaliveParams(keepalive.ClientParameters{
	Time:                30 * time.Second,
	Timeout:             10 * time.Second,
	PermitWithoutStream: true,
})

// Client wraps the Talos machinery client.
// Client is safe for concurrent use by multiple goroutines.
type Client struct {
	*talosclient.Client

	versionMu     sync.RWMutex
	versionCached *version.TalosVersion
}

// NewClient creates a new Talos client from the default or env-configured talosconfig.
// Auth (mTLS / basic / SideroV1) is handled transparently by the client library.
func NewClient(ctx context.Context) (*Client, error) {
	configPath := os.Getenv("TALOSCONFIG") // empty → library uses default ~/.talos/config

	cfg, err := clientconfig.Open(configPath)
	if err != nil {
		return nil, err
	}

	opts := []talosclient.OptionFunc{
		talosclient.WithConfig(cfg),
		talosclient.WithDefaultGRPCDialOptions(),
		talosclient.WithGRPCDialOptions(grpcKeepalive),
	}

	if ctxName := os.Getenv("TALOS_CONTEXT"); ctxName != "" {
		opts = append(opts, talosclient.WithContextName(ctxName))
	}

	if eps := os.Getenv("TALOS_ENDPOINTS"); eps != "" {
		opts = append(opts, talosclient.WithEndpoints(strings.Split(eps, ",")...))
	}

	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	c, err := talosclient.New(dialCtx, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial Talos gRPC (timeout %s): %w", dialTimeout, err)
	}

	return &Client{Client: c}, nil
}

// GetClusterVersion fetches and caches the Talos version from the default
// cluster endpoint. Subsequent calls return the cached value. On fetch
// failure the cache is not updated, allowing retry.
//
// This is suitable for informational use (startup log, prompts). For upgrade
// path validation use GetNodeVersion to query a specific target node.
func (c *Client) GetClusterVersion(ctx context.Context) (*version.TalosVersion, error) {
	// Fast path: read lock allows concurrent cache hits without serialisation.
	c.versionMu.RLock()
	if c.versionCached != nil {
		v := c.versionCached
		c.versionMu.RUnlock()
		return v, nil
	}
	c.versionMu.RUnlock()

	// Slow path: acquire write lock and re-check (double-checked locking).
	// A concurrent goroutine may have fetched the version between the two lock
	// acquisitions, so the nil check is repeated to avoid a redundant fetch.
	c.versionMu.Lock()
	defer c.versionMu.Unlock()

	if c.versionCached != nil {
		return c.versionCached, nil
	}

	v, err := c.fetchVersion(ctx)
	if err != nil {
		return nil, err
	}

	c.versionCached = v

	return v, nil
}

// GetNodeVersion fetches the Talos version from a specific node without caching.
// Use this for upgrade path validation to ensure fresh per-node data.
func (c *Client) GetNodeVersion(ctx context.Context, node string) (*version.TalosVersion, error) {
	// nil allowlist: node was already validated by the calling tool handler.
	ctx, err := WithSingleNode(ctx, []string{node}, nil)
	if err != nil {
		return nil, err
	}

	return c.fetchVersion(ctx)
}

// Ping verifies gRPC connectivity to the default endpoint by issuing a Version
// RPC. Unlike GetClusterVersion, Ping never uses a cached result, making it
// suitable for liveness probes that must detect a lost connection.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Version(ctx)
	return err
}

// InvalidateVersionCache clears the cached cluster version.
// Call this after a successful upgrade so the next GetClusterVersion call
// fetches fresh data.
func (c *Client) InvalidateVersionCache() {
	c.versionMu.Lock()
	defer c.versionMu.Unlock()

	c.versionCached = nil
}

// COSIState returns the COSI state accessor for resource queries.
// The underlying state.State on talosclient.Client is a field, not a method,
// so consumer-declared interfaces reach it through this accessor.
func (c *Client) COSIState() state.State {
	return c.COSI
}

// RebootNodes reboots the nodes targeted by ctx using mode.
func (c *Client) RebootNodes(ctx context.Context, mode machineapi.RebootRequest_Mode) error {
	opts, err := rebootOptions(mode)
	if err != nil {
		return err
	}

	return c.Reboot(ctx, opts...)
}

// UpgradeNodes initiates a Talos upgrade on the nodes targeted by ctx as described by req.
func (c *Client) UpgradeNodes(ctx context.Context, req *machineapi.UpgradeRequest) (*machineapi.UpgradeResponse, error) {
	opts, err := upgradeOptions(req)
	if err != nil {
		return nil, err
	}

	return c.UpgradeWithOptions(ctx, opts...) //nolint:staticcheck // LifecycleService migration is a separate compatibility decision
}

// WatchEvents streams runtime events to watch, starting with the last tail
// events (negative tail: all available past events).
func (c *Client) WatchEvents(ctx context.Context, tail int32, watch func(<-chan talosclient.Event)) error {
	return c.EventsWatch(ctx, watch, eventsOptions(tail)...)
}

func rebootOptions(mode machineapi.RebootRequest_Mode) ([]talosclient.RebootMode, error) {
	switch mode {
	case machineapi.RebootRequest_DEFAULT:
		return nil, nil
	case machineapi.RebootRequest_POWERCYCLE:
		return []talosclient.RebootMode{talosclient.WithPowerCycle}, nil
	case machineapi.RebootRequest_FORCE:
		return []talosclient.RebootMode{talosclient.WithForce}, nil
	default:
		return nil, fmt.Errorf("unsupported reboot mode %d", mode)
	}
}

func upgradeOptions(req *machineapi.UpgradeRequest) ([]talosclient.UpgradeOption, error) {
	if req == nil {
		return nil, fmt.Errorf("upgrade request must not be nil")
	}

	switch req.GetRebootMode() {
	case machineapi.UpgradeRequest_DEFAULT, machineapi.UpgradeRequest_POWERCYCLE:
	default:
		return nil, fmt.Errorf("unsupported upgrade reboot mode %d", req.GetRebootMode())
	}

	return []talosclient.UpgradeOption{
		talosclient.WithUpgradeImage(req.GetImage()),
		talosclient.WithUpgradePreserve(req.GetPreserve()),
		talosclient.WithUpgradeStage(req.GetStage()),
		talosclient.WithUpgradeForce(req.GetForce()),
		talosclient.WithUpgradeRebootMode(req.GetRebootMode()),
	}, nil
}

func eventsOptions(tail int32) []talosclient.EventsOptionFunc {
	return []talosclient.EventsOptionFunc{talosclient.WithTailEvents(tail)}
}

// fetchVersion calls the Talos Version gRPC method and parses the first response message.
func (c *Client) fetchVersion(ctx context.Context) (*version.TalosVersion, error) {
	resp, err := c.Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch Talos version: %w", err)
	}

	if len(resp.Messages) == 0 {
		return nil, fmt.Errorf("fetch Talos version: empty response")
	}

	tag := resp.Messages[0].Version.GetTag()

	v, err := version.Parse(tag)
	if err != nil {
		return nil, fmt.Errorf("parse Talos version tag %q: %w", tag, err)
	}

	return &v, nil
}

// WithSingleNode returns a context targeting one node after validating it
// against allow. If allow is nil, all nodes are permitted. If nodes is empty,
// the context is returned unchanged (uses config default). More than one node
// is an error. The single node uses WithNode (singular, "node" gRPC metadata
// key) for one-to-one proxying, which COSI State methods and byte streams
// require.
func WithSingleNode(ctx context.Context, nodes []string, allow *NodeAllowlist) (context.Context, error) {
	if len(nodes) == 0 {
		return ctx, nil
	}
	if err := allow.CheckNodes(nodes); err != nil {
		return ctx, err
	}
	if len(nodes) > 1 {
		return ctx, fmt.Errorf("this operation targets exactly one node (got %d); call it once per node", len(nodes))
	}
	return talosclient.WithNode(ctx, nodes[0]), nil
}

// Targets is an allowlist-checked node list. CheckTargets is the only constructor
// that carries nodes, so an unchecked list cannot reach multi-node dispatch. The
// zero value carries no node routing: the request goes to the connected endpoint.
type Targets struct {
	nodes []string
}

// CheckTargets validates nodes against allow before any request can be sent.
func CheckTargets(nodes []string, allow *NodeAllowlist) (Targets, error) {
	if len(nodes) == 0 {
		return Targets{}, nil
	}
	if err := allow.CheckNodes(nodes); err != nil {
		return Targets{}, err
	}
	return Targets{nodes: slices.Clone(nodes)}, nil
}

// Len returns the number of requested nodes.
func (t Targets) Len() int { return len(t.nodes) }

// NodeResult is one node's successful response.
type NodeResult[R any] struct {
	Node    string
	Payload R
}

// NodeError is one node's failure.
type NodeError struct {
	Node string
	Err  error
}

func (e NodeError) Error() string { return e.Node + ": " + e.Err.Error() }

func (e NodeError) Unwrap() error { return e.Err }

// NodeErrors lists node failures in request order.
type NodeErrors []NodeError

// Err joins the errors in order, or returns nil when there are none.
func (e NodeErrors) Err() error {
	if len(e) == 0 {
		return nil
	}
	errs := make([]error, len(e))
	for i := range e {
		errs[i] = e[i]
	}
	return errors.Join(errs...)
}

// Outcome is the failure state of a FanOut or StreamEach call. Handlers read
// it through FailClosed or KeepSurvivors, never by node count.
type Outcome struct {
	multi     bool
	openErr   error
	err       error
	nodeErrs  NodeErrors
	allFailed bool
}

// FailClosed is for calls where any failure fails the tool. open is a stream
// open failure and err any other failure; with two or more nodes only err is
// set, as the per-node errors in request order.
func (o Outcome) FailClosed() (open, err error) {
	if !o.multi {
		return o.openErr, o.err
	}
	return nil, o.nodeErrs.Err()
}

// KeepSurvivors is for calls that return the surviving nodes' results. With
// two or more nodes each failure is listed in nodeErrs; when every node
// failed, err carries them all and nodeErrs is nil.
func (o Outcome) KeepSurvivors() (nodeErrs NodeErrors, open, err error) {
	if !o.multi {
		return nil, o.openErr, o.err
	}
	if o.allFailed {
		return nil, nil, o.nodeErrs.Err()
	}
	return o.nodeErrs, nil, nil
}

func newMultiOutcome(nodes []string, errs NodeErrors) Outcome {
	order := firstPositions(nodes)
	sort.SliceStable(errs, func(i, j int) bool { return order[errs[i].Node] < order[errs[j].Node] })

	failed := make(map[string]struct{}, len(errs))
	for _, e := range errs {
		failed[e.Node] = struct{}{}
	}

	return Outcome{multi: true, nodeErrs: errs, allFailed: len(errs) > 0 && len(failed) == len(order)}
}

func firstPositions(nodes []string) map[string]int {
	order := make(map[string]int, len(nodes))
	for i, n := range nodes {
		if _, ok := order[n]; !ok {
			order[n] = i
		}
	}
	return order
}

// perNodeContext returns parent carrying the outgoing metadata that multiplex
// attached to nodeCtx, so the RPC runs on parent while multiplex's sends run
// on a context that never ends.
func perNodeContext(parent, nodeCtx context.Context) context.Context {
	md, _ := metadata.FromOutgoingContext(nodeCtx)
	return metadata.NewOutgoingContext(parent, md)
}

// FanOut runs call once per node; with fewer than two nodes it runs inline and
// returns the error unwrapped. results lists the successes in request order.
// Every response is delivered even after ctx ends, so no result is lost.
func FanOut[R any](ctx context.Context, t Targets, call func(context.Context) (R, error)) (results []NodeResult[R], out Outcome) {
	switch len(t.nodes) {
	case 0:
		payload, err := call(ctx)
		if err != nil {
			return nil, Outcome{err: err}
		}
		return []NodeResult[R]{{Payload: payload}}, Outcome{}
	case 1:
		payload, err := call(talosclient.WithNode(ctx, t.nodes[0]))
		if err != nil {
			return nil, Outcome{err: err}
		}
		return []NodeResult[R]{{Node: t.nodes[0], Payload: payload}}, Outcome{}
	}

	var errs NodeErrors

	responses := multiplex.Unary(context.WithoutCancel(ctx), t.nodes, func(nodeCtx context.Context) (R, error) {
		return call(perNodeContext(ctx, nodeCtx))
	})
	for r := range responses {
		if r.Err != nil {
			errs = append(errs, NodeError{Node: r.Node, Err: r.Err})
			continue
		}
		results = append(results, NodeResult[R]{Node: r.Node, Payload: r.Payload})
	}

	order := firstPositions(t.nodes)
	sort.SliceStable(results, func(i, j int) bool { return order[results[i].Node] < order[results[j].Node] })

	return results, newMultiOutcome(t.nodes, errs)
}

// StreamEach passes each node's messages to onMsg on the caller's goroutine. When
// onMsg returns true it cancels the other streams; the Outcome holds only errors
// received before that. With under two nodes, open and receive errors stay apart.
func StreamEach[R any](
	ctx context.Context,
	t Targets,
	open func(context.Context) (grpc.ServerStreamingClient[R], error),
	onMsg func(node string, msg *R) (stop bool),
) Outcome {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	switch len(t.nodes) {
	case 0:
		return streamDirect(streamCtx, "", open, onMsg)
	case 1:
		return streamDirect(talosclient.WithNode(streamCtx, t.nodes[0]), t.nodes[0], open, onMsg)
	}

	var errs NodeErrors

	responses := multiplex.Streaming(context.WithoutCancel(ctx), t.nodes, func(nodeCtx context.Context) (grpc.ServerStreamingClient[R], error) {
		return open(perNodeContext(streamCtx, nodeCtx))
	})
	for r := range responses {
		if r.Err != nil {
			errs = append(errs, NodeError{Node: r.Node, Err: r.Err})
			continue
		}
		if onMsg(r.Node, r.Payload) {
			cancel()
			go func() {
				for range responses {
				}
			}()
			return newMultiOutcome(t.nodes, errs)
		}
	}

	return newMultiOutcome(t.nodes, errs)
}

func streamDirect[R any](
	ctx context.Context,
	node string,
	open func(context.Context) (grpc.ServerStreamingClient[R], error),
	onMsg func(node string, msg *R) (stop bool),
) Outcome {
	stream, err := open(ctx)
	if err != nil {
		return Outcome{openErr: err}
	}

	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return Outcome{}
			}
			return Outcome{err: err}
		}
		if onMsg(node, msg) {
			return Outcome{}
		}
	}
}
