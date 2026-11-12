package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

const (
	defaultReadMaxBytes = 32768 // 32 KB
	maxListEntries      = 10000
)

// fileEntry describes a single filesystem entry returned by talos_list_files.
type fileEntry struct {
	Name         string `json:"name"`
	RelativeName string `json:"relative_name,omitzero"`
	Size         int64  `json:"size"`
	IsDir        bool   `json:"is_dir"`
	Mode         string `json:"mode,omitzero"`
}

// listFilesResult is the structured output for talos_list_files.
type listFilesResult struct {
	Path       string            `json:"path"`
	Entries    []fileEntry       `json:"entries"`
	Truncated  bool              `json:"truncated"`
	NodeErrors map[string]string `json:"node_errors,omitzero"`
}

// readFileResult is the structured output for talos_read_file.
type readFileResult struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	MaxBytes  int    `json:"max_bytes"`
}

var (
	listFilesOutputSchema = mustDeriveSchema[listFilesResult]()
	readFileOutputSchema  = mustDeriveSchema[readFileResult]()
)

// ListFilesOutputSchema returns the JSON schema for HandleListFiles.
func ListFilesOutputSchema() *jsonschema.Schema { return listFilesOutputSchema }

// ReadFileOutputSchema returns the JSON schema for HandleReadFile.
func ReadFileOutputSchema() *jsonschema.Schema { return readFileOutputSchema }

// ListFilesArgs defines input for talos_list_files.
type ListFilesArgs struct {
	Path    string   `json:"path" jsonschema:"Absolute path on the node to list (e.g. '/etc'\\, '/var/log'). When TALOS_MCP_ALLOWED_PATHS is set the prefix is enforced as defense-in-depth only; symlinks on the node are not resolved against the allowlist."`
	Recurse bool     `json:"recurse,omitempty" jsonschema:"Recursively list subdirectories. Defaults to false."`
	Nodes   []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// ReadFileArgs defines input for talos_read_file.
type ReadFileArgs struct {
	Path     string   `json:"path" jsonschema:"Absolute path to the file on the node to read (e.g. '/etc/os-release'). When TALOS_MCP_ALLOWED_PATHS is set the prefix is enforced as defense-in-depth only; symlinks on the node are not resolved against the allowlist."`
	MaxBytes int      `json:"max_bytes,omitempty" jsonschema:"Maximum number of bytes to return. Defaults to 32768 (32KB)."`
	Nodes    []string `json:"nodes,omitempty" jsonschema:"Target node IP or hostname (exactly one). Omit to use the default node from talosconfig."`
}

// HandleListFiles implements the talos_list_files tool.
func (h *Handlers) HandleListFiles(ctx context.Context, _ *mcp.CallToolRequest, args ListFilesArgs) (*mcp.CallToolResult, any, error) {
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	listPath := args.Path
	if listPath == "" {
		listPath = "/"
	}

	if err := checkPathAllowed(listPath, h.AllowedPaths); err != nil {
		return nil, nil, err
	}

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	files := []fileEntry{}

	truncated := false

	out := talos.StreamEach(ctx, targets,
		func(c context.Context) (machineapi.MachineService_ListClient, error) {
			return h.Client.LS(c, &machineapi.ListRequest{
				Root:    listPath,
				Recurse: args.Recurse,
			})
		},
		func(_ string, info *machineapi.FileInfo) bool {
			if info.GetName() == "" {
				return false
			}

			if len(files) >= maxListEntries {
				truncated = true

				return true
			}

			files = append(files, newFileEntry(info))

			return false
		},
	)

	nodeErrs, openErr, streamErr := out.KeepSurvivors()
	if openErr != nil {
		return nil, nil, fmt.Errorf("list files %q: %w", listPath, openErr)
	}

	if streamErr != nil {
		return nil, nil, fmt.Errorf("list files %q: %w", listPath, streamErr)
	}

	return renderListFiles(listFilesResult{Path: listPath, Entries: files, Truncated: truncated, NodeErrors: nodeErrorMap(nodeErrs)})
}

func newFileEntry(info *machineapi.FileInfo) fileEntry {
	entry := fileEntry{
		Name:         info.GetName(),
		RelativeName: info.GetRelativeName(),
		Size:         info.GetSize(),
		IsDir:        info.GetIsDir(),
	}

	if info.GetMode() != 0 {
		entry.Mode = fmt.Sprintf("%04o", info.GetMode())
	}

	return entry
}

func renderListFiles(result listFilesResult) (*mcp.CallToolResult, any, error) {
	encoded, err := json.Marshal(result.Entries)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	text := string(encoded)
	if result.Truncated {
		text += fmt.Sprintf("\n\n[truncated at %d entries]", maxListEntries)
	}

	text += nodeErrorsText(result.NodeErrors)

	return jsonWithTextResult(result, text)
}

// HandleReadFile implements the talos_read_file tool.
func (h *Handlers) HandleReadFile(ctx context.Context, _ *mcp.CallToolRequest, args ReadFileArgs) (*mcp.CallToolResult, any, error) {
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	if err := checkPathAllowed(args.Path, h.AllowedPaths); err != nil {
		return nil, nil, err
	}

	if len(args.Nodes) > 1 {
		return nil, nil, fmt.Errorf("talos_read_file requires exactly one target node (got %d); read from each node individually", len(args.Nodes))
	}

	ctx, err := talos.WithSingleNode(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	maxBytes := args.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultReadMaxBytes
	}

	r, err := h.Client.Read(ctx, args.Path)
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

	dto := readFileResult{
		Path:      args.Path,
		Content:   content,
		Truncated: truncated,
		MaxBytes:  maxBytes,
	}

	return jsonWithTextResult(dto, sb.String())
}

// ParseAllowedPaths parses a comma-separated list of path prefixes into a slice.
// Each prefix is trimmed of whitespace and canonicalized with filepath.Clean to
// prevent bypass attempts using ".." components.
// Returns nil when raw is empty (no allowlist — all paths permitted).
func ParseAllowedPaths(raw string) []string {
	if raw == "" {
		return nil
	}

	var paths []string

	for _, p := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			paths = append(paths, filepath.Clean(trimmed))
		}
	}

	return paths
}

// checkPathAllowed returns an error if path is not under any of the allowed prefixes.
// It returns nil when allowed is empty (no allowlist configured).
// Prefix matching is directory-boundary-safe: "/etc" does not match "/etc-evil".
// The path is canonicalized via filepath.Clean to prevent ".." traversal bypass.
//
// Limitation: this check runs on the MCP server host against the requested path.
// It does NOT resolve symlinks on the remote Talos node — a request for a path
// under an allowed prefix that is a symlink to a target outside the allowlist
// still passes (the symlink is resolved on the node, outside this check's view).
// Treat the allowlist as defense-in-depth, not a hard security boundary. See issue #42.
func checkPathAllowed(rawPath string, allowed []string) error {
	if len(allowed) == 0 {
		return nil
	}

	path := filepath.Clean(rawPath)

	for _, prefix := range allowed {
		// Normalise so the prefix always ends with "/" for safe directory boundary matching.
		dirPrefix := strings.TrimSuffix(prefix, "/") + "/"
		if path == prefix || strings.HasPrefix(path, dirPrefix) {
			return nil
		}
	}

	return fmt.Errorf("path %q is not in the allowed paths list (TALOS_MCP_ALLOWED_PATHS=%s)", rawPath, strings.Join(allowed, ","))
}
