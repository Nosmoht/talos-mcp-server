package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	commonapi "github.com/siderolabs/talos/pkg/machinery/api/common"
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

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
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

	// follow=false: finite stream capped by tailLines
	stream, err := h.Client.Logs(ctx, ns, commonapi.ContainerDriver_CONTAINERD, args.ServiceName, false, tailLines)
	if err != nil {
		return nil, nil, fmt.Errorf("logs for %q: %w", args.ServiceName, err)
	}

	var lines []string

	nodeErrors := make(map[string]string)

	var streamErr error

	for {
		msg, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				streamErr = err
			}

			break
		}

		if msg.GetBytes() != nil {
			lines = append(lines, strings.TrimRight(string(msg.GetBytes()), "\n"))

			// Defense-in-depth: stop collecting once we have tailLines entries.
			// The server-side cap should already enforce this, but a rogue
			// implementation could send more, causing unbounded memory growth.
			if len(lines) >= int(tailLines) {
				break
			}
		} else if meta := msg.GetMetadata(); meta != nil && meta.GetError() != "" {
			// Per-node error embedded in the stream (e.g. node unreachable).
			node := meta.GetHostname()
			if node == "" {
				node = "unknown"
			}

			nodeErrors[node] = meta.GetError()
		}
	}

	if streamErr != nil {
		return nil, nil, fmt.Errorf("logs stream for %q: %w", args.ServiceName, streamErr)
	}

	result := strings.Join(lines, "\n")
	if len(nodeErrors) > 0 {
		errJSON, _ := json.Marshal(nodeErrors)
		result += "\n\n[node errors: " + string(errJSON) + "]"
	}

	return textResult(result), nil, nil
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

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	maxLines := args.MaxLines
	if maxLines <= 0 {
		maxLines = defaultDmesgMaxLines
	}

	// follow=false, tail=false: collect existing messages
	stream, err := h.Client.Dmesg(ctx, false, false)
	if err != nil {
		return nil, nil, fmt.Errorf("dmesg: %w", err)
	}

	// Ring buffer: pre-allocated to maxLines; once full, oldest entry is
	// overwritten so memory stays bounded regardless of kernel buffer size.
	lines := make([]string, 0, maxLines)

	nodeErrors := make(map[string]string)

	var streamErr error

	for {
		msg, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				streamErr = err
			}

			break
		}

		if msg.GetBytes() != nil {
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
		} else if meta := msg.GetMetadata(); meta != nil && meta.GetError() != "" {
			// Per-node error embedded in the stream (e.g. node unreachable).
			node := meta.GetHostname()
			if node == "" {
				node = "unknown"
			}

			nodeErrors[node] = meta.GetError()
		}
	}

	if streamErr != nil {
		return nil, nil, fmt.Errorf("dmesg stream: %w", streamErr)
	}

	result := strings.Join(lines, "\n")
	if len(lines) == 0 {
		result = "(no dmesg output)"
	}

	if len(nodeErrors) > 0 {
		errJSON, _ := json.Marshal(nodeErrors)
		result += "\n\n[node errors: " + string(errJSON) + "]"
	}

	return textResult(result), nil, nil
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

	// Use a cancellable child context so we can stop after collecting enough events.
	nodesCtx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}
	collectCtx, cancel := context.WithTimeout(nodesCtx, defaultEventsTimeout)
	defer cancel()

	type eventEntry struct {
		Node    string `json:"node"`
		TypeURL string `json:"type_url"`
		ID      string `json:"id"`
		Payload string `json:"payload"`
	}

	var events []eventEntry

	// EventsWatch streams forever — we stop it via context timeout.
	// WithTailEvents sends the last N historical events, then continues streaming new ones.
	// The 5-second timeout gives us the tail events plus any immediate new ones.
	// DeadlineExceeded and Canceled are expected: they signal normal collection-window expiry.
	// Any other error (e.g. connection refused) is surfaced so callers can distinguish
	// "no events" from "node unreachable".
	if err := h.Client.EventsWatch(collectCtx,
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

				events = append(events, entry)
			}
		},
		talosclient.WithTailEvents(tailCount),
	); err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return nil, nil, fmt.Errorf("events watch: %w", err)
	}

	type result struct {
		Count  int          `json:"count"`
		Events []eventEntry `json:"events"`
	}

	return jsonResult(result{Count: len(events), Events: events})
}
