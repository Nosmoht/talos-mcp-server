// Package completions implements the MCP completion/complete handler for
// talos-mcp. It returns argument suggestions for registered prompts and
// resource templates; it never performs cluster I/O.
//
// CompleteParams.Context.Arguments (previously-resolved template variables)
// is intentionally ignored: all candidate sources are static tables or the
// in-memory node allowlist, neither of which depends on prior selections.
// When dynamic discovery is added, thread Context.Arguments through to
// scope the result.
package completions

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// NewHandler returns a CompletionHandler bound to allowedNodes. The returned
// handler is safe for concurrent use: it reads immutable static tables and
// only calls NodeAllowlist.Exact, which returns a fresh slice per call.
func NewHandler(allowedNodes *talos.NodeAllowlist) func(context.Context, *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	return func(_ context.Context, req *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
		ref := req.Params.Ref
		if ref == nil {
			return emptyResult(), nil
		}
		name := req.Params.Argument.Name
		partial := req.Params.Argument.Value

		var cands []string
		switch ref.Type {
		case "ref/prompt":
			cands = forPrompt(ref.Name, name, allowedNodes)
		case "ref/resource":
			cands = forResource(ref.URI, name, allowedNodes)
		}
		values := filterPrefix(cands, partial)
		if values == nil {
			return emptyResult(), nil
		}
		return &mcp.CompleteResult{
			Completion: mcp.CompletionResultDetails{Values: values},
		}, nil
	}
}

// emptyResult returns a result whose Values is a non-nil empty slice. The
// MCP wire format requires "values" to be an array; a nil []string would
// marshal to null and break spec-compliant clients.
func emptyResult() *mcp.CompleteResult {
	return &mcp.CompleteResult{
		Completion: mcp.CompletionResultDetails{Values: []string{}},
	}
}

// forPrompt dispatches on (promptName, argName). Prompt name is part of the
// key because the same argument label can mean different things in a
// hypothetical future prompt.
func forPrompt(promptName, arg string, nodes *talos.NodeAllowlist) []string {
	switch promptName {
	case "diagnose-node", "investigate-etcd":
		if arg == "node" {
			return nodes.Exact()
		}
	case "debug-service":
		switch arg {
		case "node":
			return nodes.Exact()
		case "service":
			return knownServices
		}
	case "pre-upgrade-checklist":
		if arg == "nodes" {
			return nodes.Exact()
		}
	case "apply-config":
		switch arg {
		case "node":
			return nodes.Exact()
		case "mode":
			return applyModes
		}
	}
	return nil
}

// forResource dispatches on (template URI prefix, argName). The "talos://"
// prefix check scopes this resolver to the templates registered in
// internal/resources/resources.go.
func forResource(uri, arg string, nodes *talos.NodeAllowlist) []string {
	if !strings.HasPrefix(uri, "talos://") {
		return nil
	}
	switch arg {
	case "node":
		return nodes.Exact()
	case "namespace":
		return commonNamespaces
	}
	return nil
}

func filterPrefix(cands []string, partial string) []string {
	if partial == "" {
		return cands
	}
	p := strings.ToLower(partial)
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		if strings.HasPrefix(strings.ToLower(c), p) {
			out = append(out, c)
		}
	}
	return out
}
