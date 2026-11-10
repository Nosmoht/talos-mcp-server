package tools

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// unaryResult returns the tool result for FanOut responses: the raw response
// for no node or one node, attributeMessages for several.
func unaryResult[R any](rs []talos.NodeResult[R]) (*mcp.CallToolResult, any, error) {
	switch len(rs) {
	case 0:
		return nil, nil, fmt.Errorf("no node returned a response")
	case 1:
		return jsonResult(rs[0].Payload)
	default:
		return attributeMessages(rs)
	}
}

// attributeMessages returns {"messages": [...]} in request order, each message
// with "metadata" replaced by {"hostname": <requested node>}. Messages stay raw
// JSON so 64-bit IDs keep their digits; common.Metadata is deprecated, so unnamed.
func attributeMessages[R any](rs []talos.NodeResult[R]) (*mcp.CallToolResult, any, error) {
	messages := []map[string]json.RawMessage{}

	for _, r := range rs {
		raw, err := json.Marshal(r.Payload)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal response from %s: %w", r.Node, err)
		}

		var resp struct {
			Messages []map[string]json.RawMessage `json:"messages"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, nil, fmt.Errorf("decode response from %s: %w", r.Node, err)
		}

		hostname, err := json.Marshal(map[string]string{"hostname": r.Node})
		if err != nil {
			return nil, nil, fmt.Errorf("marshal hostname %s: %w", r.Node, err)
		}

		for _, m := range resp.Messages {
			if m == nil {
				m = map[string]json.RawMessage{}
			}

			m["metadata"] = hostname
			messages = append(messages, m)
		}
	}

	return jsonResult(map[string]any{"messages": messages})
}

// nodeErrorMap returns the per-node failures keyed by requested node, or nil
// when there are none. The structured node_errors field and the
// "[node errors: ...]" text block both come from this one map.
func nodeErrorMap(errs talos.NodeErrors) map[string]string {
	if len(errs) == 0 {
		return nil
	}

	m := make(map[string]string, len(errs))
	for _, e := range errs {
		m[e.Node] = e.Err.Error()
	}

	return m
}

// nodeErrorsText renders m as the trailing "[node errors: ...]" text block, or
// "" when m is nil.
func nodeErrorsText(m map[string]string) string {
	if m == nil {
		return ""
	}

	b, _ := json.Marshal(m)

	return "\n\n[node errors: " + string(b) + "]"
}
