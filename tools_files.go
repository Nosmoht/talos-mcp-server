package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// ListFilesArgs defines input for talos_list_files.
type ListFilesArgs struct {
	Path    string   `json:"path" jsonschema:"Absolute path on the node to list (e.g. '/etc'\\, '/var/log')."`
	Recurse bool     `json:"recurse,omitempty" jsonschema:"Recursively list subdirectories. Defaults to false."`
	Nodes   []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// handleListFiles implements the talos_list_files tool.
func (tc *TalosClient) handleListFiles(ctx context.Context, _ *mcp.CallToolRequest, args ListFilesArgs) (*mcp.CallToolResult, any, error) {
	ctx = withNodes(ctx, args.Nodes)

	path := args.Path
	if path == "" {
		path = "/"
	}

	stream, err := tc.client.LS(ctx, &machineapi.ListRequest{
		Root:    path,
		Recurse: args.Recurse,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list files %q: %w", path, err)
	}

	type fileEntry struct {
		Name         string `json:"name"`
		RelativeName string `json:"relative_name,omitempty"`
		Size         int64  `json:"size"`
		IsDir        bool   `json:"is_dir"`
		Mode         string `json:"mode,omitempty"`
	}

	var files []fileEntry

	for {
		info, err := stream.Recv()
		if err != nil {
			break
		}

		if info.GetName() == "" {
			continue
		}

		entry := fileEntry{
			Name:         info.GetName(),
			RelativeName: info.GetRelativeName(),
			Size:         info.GetSize(),
			IsDir:        info.GetIsDir(),
		}

		if info.GetMode() != 0 {
			entry.Mode = fmt.Sprintf("%04o", info.GetMode())
		}

		files = append(files, entry)
	}

	out, err := json.MarshalIndent(files, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// ReadFileArgs defines input for talos_read_file.
type ReadFileArgs struct {
	Path     string   `json:"path" jsonschema:"Absolute path to the file on the node to read (e.g. '/etc/os-release')."`
	MaxBytes int      `json:"max_bytes,omitempty" jsonschema:"Maximum number of bytes to return. Defaults to 32768 (32KB)."`
	Nodes    []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// handleReadFile implements the talos_read_file tool.
func (tc *TalosClient) handleReadFile(ctx context.Context, _ *mcp.CallToolRequest, args ReadFileArgs) (*mcp.CallToolResult, any, error) {
	ctx = withNodes(ctx, args.Nodes)

	maxBytes := args.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 32768
	}

	r, err := tc.client.Read(ctx, args.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("read file %q: %w", args.Path, err)
	}
	defer r.Close() //nolint:errcheck

	var buf bytes.Buffer
	lr := io.LimitReader(r, int64(maxBytes)+1)

	if _, err := io.Copy(&buf, lr); err != nil {
		return nil, nil, fmt.Errorf("read file content: %w", err)
	}

	content := buf.String()
	truncated := false

	if len(content) > maxBytes {
		content = content[:maxBytes]
		truncated = true
	}

	var sb strings.Builder

	sb.WriteString(content)

	if truncated {
		fmt.Fprintf(&sb, "\n\n[truncated at %d bytes]", maxBytes)
	}

	return textResult(sb.String()), nil, nil
}
