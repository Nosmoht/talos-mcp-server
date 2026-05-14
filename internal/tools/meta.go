package tools

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/siderolabs/talos/pkg/machinery/meta"
	runtimeres "github.com/siderolabs/talos/pkg/machinery/resources/runtime"

	"github.com/Nosmoht/talos-mcp-server/internal/marshal"
	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// MetaArgs defines input for the talos_meta tool — read/write/delete on the
// META partition key/value store. Supports both authenticated and
// maintenance-mode (insecure) paths.
type MetaArgs struct {
	Action          string   `json:"action" jsonschema:"REQUIRED: 'read'\\, 'write'\\, or 'delete'."`
	Key             string   `json:"key" jsonschema:"REQUIRED: META key as decimal or 0x-prefixed hex (uint8\\, 0-255). Well-known: see github.com/siderolabs/talos/pkg/machinery/meta (Upgrade=6\\, UserReserved1=12)."`
	Value           string   `json:"value,omitempty" jsonschema:"Required for action='write'; stored as raw bytes. Ignored otherwise."`
	Confirm         bool     `json:"confirm,omitempty" jsonschema:"REQUIRED for action='write' or 'delete'."`
	Nodes           []string `json:"nodes,omitempty" jsonschema:"Authenticated mode only: target node. Mutually exclusive with endpoint."`
	Insecure        bool     `json:"insecure,omitempty" jsonschema:"Maintenance-mode insecure connection (bypasses mTLS). Requires endpoint and TALOS_MCP_ENABLE_INSECURE=true."`
	Endpoint        string   `json:"endpoint,omitempty" jsonschema:"Required when insecure=true: bare IPv4 or IPv6 address of the maintenance-mode node."`
	CertFingerprint string   `json:"cert_fingerprint,omitempty" jsonschema:"Optional SHA-256 server cert fingerprint (hex; 64 chars after stripping colons/whitespace) for TOFU pinning. Only valid when insecure=true."`
}

// reservedMetaKeys are always permitted for write/delete; the operator does
// not need to enumerate them in TALOS_MCP_META_PRIVILEGED_KEYS. They are
// "reserved for user-defined metadata" per the upstream pkg/machinery/meta
// docstrings and have no node-bricking semantics.
var reservedMetaKeys = map[uint8]struct{}{
	uint8(meta.UserReserved1): {},
	uint8(meta.UserReserved2): {},
	uint8(meta.UserReserved3): {},
}

// HandleMeta implements the talos_meta tool. action ∈ {read, write, delete}.
// Reading happens via the COSI MetaKey resource — the upstream resource-type
// name is not yet verified against the live Talos source; the handler surfaces
// upstream errors verbatim so callers can recover via talos_resource_definitions
// if the type drifts.
func (h *Handlers) HandleMeta(ctx context.Context, _ *mcp.CallToolRequest, args MetaArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_meta", struct {
		Action            string   `json:"action"`
		Key               string   `json:"key"`
		ValueLength       int      `json:"value_length,omitempty"`
		Confirm           bool     `json:"confirm,omitempty"`
		Nodes             []string `json:"nodes,omitempty"`
		Insecure          bool     `json:"insecure,omitempty"`
		Endpoint          string   `json:"endpoint,omitempty"`
		FingerprintPinned bool     `json:"fingerprint_pinned,omitempty"`
	}{
		Action:            args.Action,
		Key:               args.Key,
		ValueLength:       len(args.Value), // length, NOT content — value may be a binary token
		Confirm:           args.Confirm,
		Nodes:             args.Nodes,
		Insecure:          args.Insecure,
		Endpoint:          args.Endpoint,
		FingerprintPinned: args.CertFingerprint != "",
	}, args.Nodes)

	var (
		outcome  = OutcomeOK
		finalErr error
	)
	defer func() { h.auditOutcome("talos_meta", outcome, finalErr) }()

	if args.CertFingerprint != "" && !args.Insecure {
		outcome = OutcomeRefusedFPWithoutInsec
		finalErr = fmt.Errorf("talos_meta refused: cert_fingerprint requires insecure=true")
		return nil, nil, finalErr
	}

	key, err := parseMetaKey(args.Key)
	if err != nil {
		outcome = OutcomeRefusedArgs
		finalErr = err
		return nil, nil, err
	}

	// Validate action enum + write/delete confirm + privileged-key safelist
	// before dialling.
	switch args.Action {
	case "read":
		// no extra guards
	case "write", "delete":
		if !args.Confirm {
			outcome = OutcomeRefusedConfirm
			finalErr = fmt.Errorf("talos_meta refused: confirm must be set to true for action=%s", args.Action)
			return nil, nil, finalErr
		}
		if _, reserved := reservedMetaKeys[key]; !reserved {
			if _, allowed := h.MetaPrivilegedKeys[key]; !allowed {
				outcome = OutcomeRefusedMetaKey
				finalErr = fmt.Errorf("talos_meta refused: key %d (0x%02x) is not in UserReserved* and not listed in TALOS_MCP_META_PRIVILEGED_KEYS", key, key)
				return nil, nil, finalErr
			}
		}
	default:
		outcome = OutcomeRefusedArgs
		finalErr = fmt.Errorf("talos_meta refused: action must be 'read', 'write', or 'delete' (got %q)", args.Action)
		return nil, nil, finalErr
	}

	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	// Dispatch to authenticated or insecure transport.
	if args.Insecure {
		canonicalEndpoint, fp, gateOutcome, err := h.canonicalizeAndCheckInsecure(args.Endpoint, args.CertFingerprint, len(args.Nodes) > 0)
		if err != nil {
			outcome = gateOutcome
			finalErr = err
			return nil, nil, err
		}
		client, err := h.dialInsecure(ctx, canonicalEndpoint, fp)
		if err != nil {
			outcome = OutcomeDialError
			finalErr = err
			return nil, nil, err
		}
		defer func() { _ = client.Close() }()

		switch args.Action {
		case "read":
			return readMetaFromCOSI(ctx, client.COSI, key, &outcome, &finalErr)
		case "write":
			if err := client.MetaWrite(ctx, key, []byte(args.Value)); err != nil {
				outcome = OutcomeRPCError
				finalErr = fmt.Errorf("meta write (insecure) key=%d: %w", key, err)
				return nil, nil, finalErr
			}
			return jsonResult(map[string]any{"action": "write", "key": key, "value_length": len(args.Value)})
		case "delete":
			if err := client.MetaDelete(ctx, key); err != nil {
				outcome = OutcomeRPCError
				finalErr = fmt.Errorf("meta delete (insecure) key=%d: %w", key, err)
				return nil, nil, finalErr
			}
			return jsonResult(map[string]any{"action": "delete", "key": key})
		}
	}

	// Authenticated path.
	ctx, err = talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		outcome = OutcomeRefusedAllowlist
		finalErr = err
		return nil, nil, err
	}

	switch args.Action {
	case "read":
		return readMetaFromCOSI(ctx, h.Client.COSIState(), key, &outcome, &finalErr)
	case "write":
		if err := h.Client.MetaWrite(ctx, key, []byte(args.Value)); err != nil {
			outcome = OutcomeRPCError
			finalErr = fmt.Errorf("meta write key=%d: %w", key, err)
			return nil, nil, finalErr
		}
		return jsonResult(map[string]any{"action": "write", "key": key, "value_length": len(args.Value)})
	case "delete":
		if err := h.Client.MetaDelete(ctx, key); err != nil {
			outcome = OutcomeRPCError
			finalErr = fmt.Errorf("meta delete key=%d: %w", key, err)
			return nil, nil, finalErr
		}
		return jsonResult(map[string]any{"action": "delete", "key": key})
	}

	// Unreachable: action enum is validated above.
	outcome = OutcomeRefusedArgs
	finalErr = errors.New("talos_meta: unreachable action branch")
	return nil, nil, finalErr
}

// parseMetaKey converts the string key field to uint8. Accepts decimal
// (`6`), hex (`0x06`), and explicit octal (`0o6`). REJECTS leading-zero
// decimal-looking input (`06`) because strconv.ParseUint with base 0 would
// interpret it as octal — surprising for operators who type META keys in
// decimal. Out-of-range (> 255) is rejected explicitly even though the
// bit-size already covers it (clearer error message).
func parseMetaKey(s string) (uint8, error) {
	if s == "" {
		return 0, errors.New("key must not be empty")
	}
	// Reject ambiguous leading-zero decimal-looking input.
	if len(s) > 1 && s[0] == '0' {
		second := s[1]
		if second != 'x' && second != 'X' && second != 'o' && second != 'O' && second != 'b' && second != 'B' {
			return 0, fmt.Errorf("key %q has ambiguous leading zero: use explicit 0x prefix for hex or 0o for octal", s)
		}
	}
	v, err := strconv.ParseUint(s, 0, 9)
	if err != nil {
		return 0, fmt.Errorf("key %q: %w", s, err)
	}
	if v > 255 {
		return 0, fmt.Errorf("key %q: value %d exceeds META key range 0-255", s, v)
	}
	return uint8(v), nil
}

// readMetaFromCOSI fetches the META key/value pair from the COSI state.
// Resource identity uses the canonical upstream constants from
// pkg/machinery/resources/runtime (MetaKeyType = "MetaKeys.runtime.talos.dev",
// id = "0x06" via MetaKeyTagToID). If the talos runtime renames the
// resource, the upstream error surfaces verbatim and the caller can rediscover
// the type via talos_resource_definitions.
func readMetaFromCOSI(
	ctx context.Context,
	st state.State,
	key uint8,
	outcome *string,
	finalErr *error,
) (*mcp.CallToolResult, any, error) {
	id := runtimeres.MetaKeyTagToID(key)

	r, err := st.Get(ctx, resource.NewMetadata(runtimeres.NamespaceName, runtimeres.MetaKeyType, id, resource.VersionUndefined))
	if err != nil {
		*outcome = OutcomeRPCError
		*finalErr = fmt.Errorf("get MetaKey/%d: %w (discover the current resource type with talos_resource_definitions)", key, err)
		return nil, nil, *finalErr
	}
	data, err := marshal.Resource(r)
	if err != nil {
		*outcome = OutcomeRPCError
		*finalErr = fmt.Errorf("marshal MetaKey resource: %w", err)
		return nil, nil, *finalErr
	}
	return jsonResult(map[string]any{"action": "read", "key": key, "resource": data})
}
