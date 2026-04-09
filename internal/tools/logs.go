package tools

import (
	"context"
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
	defaultLogsTailLines    int32 = 100
	defaultDmesgMaxLines          = 200
	defaultEventsTailCount  int32 = 50
	defaultEventsTimeout          = 5 * time.Second
	defaultLogsNamespace          = "system"
)

// LogsArgs defines input for talos_logs.
type LogsArgs struct {
	ServiceName string   `json:"service_name" jsonschema:"Service or container name (e.g. 'kubelet'\\, 'containerd'\\, 'etcd')."`
	TailLines   int32    `json:"tail_lines,omitempty" jsonschema:"Number of log lines to return from the end. Defaults to 100."`
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
		}
	}

	if streamErr != nil {
		return nil, nil, fmt.Errorf("logs stream for %q: %w", args.ServiceName, streamErr)
	}

	return textResult(strings.Join(lines, "\n")), nil, nil
}

// DmesgArgs defines input for talos_dmesg.
type DmesgArgs struct {
	MaxLines int      `json:"max_lines,omitempty" jsonschema:"Maximum number of dmesg lines to return. Defaults to 200."`
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

	var lines []string

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
				if line != "" {
					lines = append(lines, line)
				}
			}
		}
	}

	if streamErr != nil {
		return nil, nil, fmt.Errorf("dmesg stream: %w", streamErr)
	}

	// Truncate to max_lines from the end (most recent)
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	result := strings.Join(lines, "\n")
	if len(lines) == 0 {
		result = "(no dmesg output)"
	}

	return textResult(result), nil, nil
}

// EventsArgs defines input for talos_events.
type EventsArgs struct {
	TailCount int32    `json:"tail_count,omitempty" jsonschema:"Number of recent events to return. Defaults to 50. Use -1 for all available events."`
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
