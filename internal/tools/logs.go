package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	commonapi "github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

const (
	defaultLogsTailLines   int32 = 100
	defaultDmesgMaxLines         = 200
	defaultEventsTailCount int32 = 50
	defaultEventsTimeout         = 5 * time.Second
	defaultLogsNamespace         = "system"
)

// logsResult is the structured output for talos_logs.
type logsResult struct {
	Service    string            `json:"service"`
	Lines      []string          `json:"lines"`
	NodeErrors map[string]string `json:"node_errors,omitzero"`
}

// dmesgResult is the structured output for talos_dmesg.
type dmesgResult struct {
	Lines      []string          `json:"lines"`
	NodeErrors map[string]string `json:"node_errors,omitzero"`
}

// eventEntry is a single Talos runtime event.
type eventEntry struct {
	Node    string `json:"node"`
	TypeURL string `json:"type_url"`
	ID      string `json:"id"`
	Payload string `json:"payload"`
}

// eventsResult is the structured output for talos_events.
type eventsResult struct {
	Count      int               `json:"count"`
	Events     []eventEntry      `json:"events"`
	NodeErrors map[string]string `json:"node_errors,omitzero"`
}

var (
	logsOutputSchema   = mustDeriveSchema[logsResult]()
	dmesgOutputSchema  = mustDeriveSchema[dmesgResult]()
	eventsOutputSchema = mustDeriveSchema[eventsResult]()
)

// LogsOutputSchema returns the JSON schema for HandleLogs.
func LogsOutputSchema() *jsonschema.Schema { return logsOutputSchema }

// DmesgOutputSchema returns the JSON schema for HandleDmesg.
func DmesgOutputSchema() *jsonschema.Schema { return dmesgOutputSchema }

// EventsOutputSchema returns the JSON schema for HandleEvents.
func EventsOutputSchema() *jsonschema.Schema { return eventsOutputSchema }

// LogsArgs defines input for talos_logs.
type LogsArgs struct {
	ServiceName string   `json:"service_name" jsonschema:"Service or container name (e.g. 'kubelet'\\, 'containerd'\\, 'etcd')."`
	TailLines   int32    `json:"tail_lines,omitempty" jsonschema:"Number of log lines to return from the end. Defaults to 100. Note: talos_dmesg uses max_lines and talos_events uses tail_count for the equivalent parameter; these will be unified in a future major version."`
	Namespace   string   `json:"namespace,omitempty" jsonschema:"Container namespace. Defaults to 'system' for Talos services."`
	Nodes       []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// HandleLogs implements the talos_logs tool.
func (h *Handlers) HandleLogs(ctx context.Context, _ *mcp.CallToolRequest, args LogsArgs) (*mcp.CallToolResult, any, error) {
	if args.ServiceName == "" {
		return nil, nil, fmt.Errorf("service_name is required")
	}

	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	tailLines := args.TailLines
	if tailLines <= 0 {
		tailLines = defaultLogsTailLines
	}

	ns := args.Namespace
	if ns == "" {
		ns = defaultLogsNamespace
	}

	lines := []string{}

	// follow=false: finite stream capped by tailLines
	out := talos.StreamEach(ctx, targets,
		func(c context.Context) (machineapi.MachineService_LogsClient, error) {
			return h.Client.Logs(c, ns, commonapi.ContainerDriver_CONTAINERD, args.ServiceName, false, tailLines)
		},
		func(_ string, msg *commonapi.Data) bool {
			if msg.GetBytes() == nil {
				return false
			}

			lines = append(lines, strings.TrimRight(string(msg.GetBytes()), "\n"))

			// Defense-in-depth: stop collecting once we have tailLines entries.
			// The server-side cap should already enforce this, but a rogue
			// implementation could send more, causing unbounded memory growth.
			return len(lines) >= int(tailLines)
		},
	)

	nodeErrs, openErr, streamErr := out.KeepSurvivors()
	if openErr != nil {
		return nil, nil, fmt.Errorf("logs for %q: %w", args.ServiceName, openErr)
	}

	if streamErr != nil {
		return nil, nil, fmt.Errorf("logs stream for %q: %w", args.ServiceName, streamErr)
	}

	errMap := nodeErrorMap(nodeErrs)
	text := strings.Join(lines, "\n") + nodeErrorsText(errMap)

	return jsonWithTextResult(logsResult{Service: args.ServiceName, Lines: lines, NodeErrors: errMap}, text)
}

// DmesgArgs defines input for talos_dmesg.
type DmesgArgs struct {
	MaxLines int      `json:"max_lines,omitempty" jsonschema:"Maximum number of dmesg lines to return. Defaults to 200. Note: talos_logs uses tail_lines and talos_events uses tail_count for the equivalent parameter; these will be unified in a future major version."`
	Nodes    []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// HandleDmesg implements the talos_dmesg tool.
func (h *Handlers) HandleDmesg(ctx context.Context, _ *mcp.CallToolRequest, args DmesgArgs) (*mcp.CallToolResult, any, error) {
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	maxLines := args.MaxLines
	if maxLines <= 0 {
		maxLines = defaultDmesgMaxLines
	}

	// Ring buffer: pre-allocated to maxLines; once full, oldest entry is
	// overwritten so memory stays bounded regardless of kernel buffer size.
	lines := make([]string, 0, maxLines)

	// follow=false, tail=false: collect existing messages
	out := talos.StreamEach(ctx, targets,
		func(c context.Context) (machineapi.MachineService_DmesgClient, error) {
			return h.Client.Dmesg(c, false, false)
		},
		func(_ string, msg *commonapi.Data) bool {
			if msg.GetBytes() == nil {
				return false
			}

			for _, line := range strings.Split(strings.TrimRight(string(msg.GetBytes()), "\n"), "\n") {
				if line == "" {
					continue
				}

				if len(lines) < maxLines {
					lines = append(lines, line)
				} else {
					// Shift left and place new line at the end (keep last maxLines).
					copy(lines, lines[1:])
					lines[maxLines-1] = line
				}
			}

			return false
		},
	)

	nodeErrs, openErr, streamErr := out.KeepSurvivors()
	if openErr != nil {
		return nil, nil, fmt.Errorf("dmesg: %w", openErr)
	}

	if streamErr != nil {
		return nil, nil, fmt.Errorf("dmesg stream: %w", streamErr)
	}

	text := strings.Join(lines, "\n")
	if len(lines) == 0 {
		text = "(no dmesg output)"
	}

	errMap := nodeErrorMap(nodeErrs)
	text += nodeErrorsText(errMap)

	return jsonWithTextResult(dmesgResult{Lines: lines, NodeErrors: errMap}, text)
}

// EventsArgs defines input for talos_events.
type EventsArgs struct {
	TailCount int32    `json:"tail_count,omitempty" jsonschema:"Number of recent events to return. Defaults to 50. Use -1 for all available events. Note: talos_logs uses tail_lines and talos_dmesg uses max_lines for the equivalent parameter; these will be unified in a future major version."`
	Nodes     []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// HandleEvents implements the talos_events tool.
// Uses a 5-second collection window to gather recent events after the tail snapshot.
func (h *Handlers) HandleEvents(ctx context.Context, _ *mcp.CallToolRequest, args EventsArgs) (*mcp.CallToolResult, any, error) {
	tailCount := args.TailCount
	if tailCount == 0 {
		tailCount = defaultEventsTailCount
	}

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	rs, out := talos.FanOut(ctx, targets, func(nodeCtx context.Context) ([]eventEntry, error) {
		return h.collectEvents(nodeCtx, tailCount)
	})

	nodeErrs, _, err := out.KeepSurvivors()
	if err != nil {
		return nil, nil, fmt.Errorf("events watch: %w", err)
	}

	events := []eventEntry{}

	for _, r := range rs {
		for _, entry := range r.Payload {
			// With several nodes the machinery would report the endpoint peer
			// as the event's node; attribute it to the requested node instead.
			if targets.Len() > 1 {
				entry.Node = r.Node
			}

			events = append(events, entry)
		}
	}

	return jsonResult(eventsResult{Count: len(events), Events: events, NodeErrors: nodeErrorMap(nodeErrs)})
}

// collectEvents streams one node's events until defaultEventsTimeout. Deadline
// and cancel are the normal end of the window; other errors are returned so
// "no events" stays distinct from "node unreachable".
func (h *Handlers) collectEvents(ctx context.Context, tail int32) ([]eventEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultEventsTimeout)
	defer cancel()

	entries := []eventEntry{}

	err := h.Client.WatchEvents(ctx, tail,
		func(ch <-chan talosclient.Event) {
			for ev := range ch {
				entry := eventEntry{
					Node:    ev.Node,
					TypeURL: ev.TypeURL,
					ID:      ev.ID,
				}

				if ev.Payload != nil {
					entry.Payload = fmt.Sprintf("%v", ev.Payload)
				}

				entries = append(entries, entry)
			}
		},
	)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return nil, err
	}

	return entries, nil
}
