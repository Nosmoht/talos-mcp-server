package tools

import (
	"context"
	"errors"
	"fmt"

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
	Nodes           []string `json:"nodes,omitempty" jsonschema:"Authenticated mode only: target nodes. action='read' accepts at most one node; 'write' and 'delete' apply to every listed node. Mutually exclusive with endpoint."`
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

var errMetaActionUnreachable = errors.New("talos_meta: unreachable action branch")

// HandleMeta implements the talos_meta tool (read, write or delete a META key).
func (h *Handlers) HandleMeta(ctx context.Context, _ *mcp.CallToolRequest, args MetaArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_meta", metaAuditArgs(args), args.Nodes)

	var (
		result  map[string]any
		outcome string
		err     error
	)
	defer func() { h.auditOutcome("talos_meta", outcome, err) }()

	if err = fingerprintRequiresInsecure("talos_meta", args.CertFingerprint, args.Insecure); err != nil {
		outcome = OutcomeRefusedFPWithoutInsec
		return nil, nil, err
	}

	var key uint8
	if key, outcome, err = h.checkMetaRequest(args); err != nil {
		return nil, nil, err
	}

	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	if args.Insecure {
		result, outcome, err = h.metaInsecure(ctx, args, key)
	} else {
		result, outcome, err = h.metaAuthenticated(ctx, args, key)
	}
	if err != nil {
		return nil, nil, err
	}

	return jsonResult(result)
}

func metaAuditArgs(args MetaArgs) any {
	return struct {
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
	}
}

func (h *Handlers) checkMetaRequest(args MetaArgs) (uint8, string, error) {
	key, err := talos.ParseMetaKey(args.Key)
	if err != nil {
		return 0, OutcomeRefusedArgs, err
	}

	switch args.Action {
	case "read":
		return key, OutcomeOK, nil
	case "write", "delete":
		if !args.Confirm {
			return 0, OutcomeRefusedConfirm, fmt.Errorf("talos_meta refused: confirm must be set to true for action=%s", args.Action)
		}
		if !h.metaKeyWritable(key) {
			return 0, OutcomeRefusedMetaKey, fmt.Errorf("talos_meta refused: key %d (0x%02x) is not in UserReserved* and not listed in TALOS_MCP_META_PRIVILEGED_KEYS", key, key)
		}
		return key, OutcomeOK, nil
	default:
		return 0, OutcomeRefusedArgs, fmt.Errorf("talos_meta refused: action must be 'read', 'write', or 'delete' (got %q)", args.Action)
	}
}

func (h *Handlers) metaKeyWritable(key uint8) bool {
	if _, reserved := reservedMetaKeys[key]; reserved {
		return true
	}
	_, allowed := h.MetaPrivilegedKeys[key]
	return allowed
}

func (h *Handlers) metaInsecure(ctx context.Context, args MetaArgs, key uint8) (map[string]any, string, error) {
	canonicalEndpoint, fp, gateOutcome, err := h.canonicalizeAndCheckInsecure(args.Endpoint, args.CertFingerprint, len(args.Nodes) > 0)
	if err != nil {
		return nil, gateOutcome, err
	}

	client, err := h.dialInsecure(ctx, canonicalEndpoint, fp)
	if err != nil {
		return nil, OutcomeDialError, err
	}
	defer func() { _ = client.Close() }()

	switch args.Action {
	case "read":
		return readMetaFromCOSI(ctx, client.COSIState(), key)
	case "write":
		if err := client.MetaWrite(ctx, key, []byte(args.Value)); err != nil {
			return nil, OutcomeRPCError, fmt.Errorf("meta write (insecure) key=%d: %w", key, err)
		}
		return metaWriteResult(key, args.Value), OutcomeOK, nil
	case "delete":
		if err := client.MetaDelete(ctx, key); err != nil {
			return nil, OutcomeRPCError, fmt.Errorf("meta delete (insecure) key=%d: %w", key, err)
		}
		return metaDeleteResult(key), OutcomeOK, nil
	default:
		return nil, OutcomeRefusedArgs, errMetaActionUnreachable
	}
}

func (h *Handlers) metaAuthenticated(ctx context.Context, args MetaArgs, key uint8) (map[string]any, string, error) {
	if args.Action == "read" {
		if len(args.Nodes) > 1 {
			return nil, OutcomeRefusedArgs, fmt.Errorf("talos_meta read targets exactly one node (got %d); read each node individually", len(args.Nodes))
		}

		ctx, err := talos.WithSingleNode(ctx, args.Nodes, h.AllowedNodes)
		if err != nil {
			return nil, OutcomeRefusedAllowlist, err
		}

		return readMetaFromCOSI(ctx, h.Client.COSIState(), key)
	}

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, OutcomeRefusedAllowlist, err
	}

	switch args.Action {
	case "write":
		_, out := talos.FanOut(ctx, targets, func(c context.Context) (struct{}, error) {
			return struct{}{}, h.Client.MetaWrite(c, key, []byte(args.Value))
		})
		if _, err := out.FailClosed(); err != nil {
			return nil, OutcomeRPCError, fmt.Errorf("meta write key=%d: %w", key, err)
		}
		return metaWriteResult(key, args.Value), OutcomeOK, nil
	case "delete":
		_, out := talos.FanOut(ctx, targets, func(c context.Context) (struct{}, error) {
			return struct{}{}, h.Client.MetaDelete(c, key)
		})
		if _, err := out.FailClosed(); err != nil {
			return nil, OutcomeRPCError, fmt.Errorf("meta delete key=%d: %w", key, err)
		}
		return metaDeleteResult(key), OutcomeOK, nil
	default:
		return nil, OutcomeRefusedArgs, errMetaActionUnreachable
	}
}

func metaWriteResult(key uint8, value string) map[string]any {
	return map[string]any{"action": "write", "key": key, "value_length": len(value)}
}

func metaDeleteResult(key uint8) map[string]any {
	return map[string]any{"action": "delete", "key": key}
}

// readMetaFromCOSI fetches the META key/value pair from the COSI state.
// Resource identity uses the canonical upstream constants from
// pkg/machinery/resources/runtime (MetaKeyType = "MetaKeys.runtime.talos.dev",
// id = "0x06" via MetaKeyTagToID). If the talos runtime renames the
// resource, the upstream error surfaces verbatim and the caller can rediscover
// the type via talos_resource_definitions.
func readMetaFromCOSI(ctx context.Context, st state.State, key uint8) (map[string]any, string, error) {
	id := runtimeres.MetaKeyTagToID(key)

	r, err := st.Get(ctx, resource.NewMetadata(runtimeres.NamespaceName, runtimeres.MetaKeyType, id, resource.VersionUndefined))
	if err != nil {
		return nil, OutcomeRPCError, fmt.Errorf("get MetaKey/%d: %w (discover the current resource type with talos_resource_definitions)", key, err)
	}

	data, err := marshal.Resource(r)
	if err != nil {
		return nil, OutcomeRPCError, fmt.Errorf("marshal MetaKey resource: %w", err)
	}

	return map[string]any{"action": "read", "key": key, "resource": data}, OutcomeOK, nil
}
