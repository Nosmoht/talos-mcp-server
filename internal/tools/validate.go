package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/validation"
)

// ValidateArgs defines input for talos_validate.
type ValidateArgs struct {
	Config string `json:"config" jsonschema:"Machine config content to validate (YAML or JSON string)."`
	Mode   string `json:"mode,omitempty" jsonschema:"Runtime mode: 'metal' (default), 'cloud', or 'container'."`
	Strict bool   `json:"strict,omitempty" jsonschema:"Treat warnings as errors. Defaults to false."`
}

// validateMode implements validation.RuntimeMode for the three supported mode strings.
type validateMode struct {
	name            string
	requiresInstall bool
	inContainer     bool
}

func (m validateMode) String() string        { return m.name }
func (m validateMode) RequiresInstall() bool { return m.requiresInstall }
func (m validateMode) InContainer() bool     { return m.inContainer }

// parseValidateMode converts a user-supplied mode string to a validation.RuntimeMode.
func parseValidateMode(mode string) (validation.RuntimeMode, error) {
	switch mode {
	case "", "metal":
		return validateMode{name: "metal", requiresInstall: true, inContainer: false}, nil
	case "cloud":
		return validateMode{name: "cloud", requiresInstall: false, inContainer: false}, nil
	case "container":
		return validateMode{name: "container", requiresInstall: false, inContainer: true}, nil
	default:
		return nil, fmt.Errorf("unknown mode %q: must be 'metal', 'cloud', or 'container'", mode)
	}
}

// HandleValidate implements the talos_validate tool.
func (h *Handlers) HandleValidate(_ context.Context, _ *mcp.CallToolRequest, args ValidateArgs) (*mcp.CallToolResult, any, error) {
	if args.Config == "" {
		return nil, nil, fmt.Errorf("config must be specified")
	}

	mode, err := parseValidateMode(args.Mode)
	if err != nil {
		return nil, nil, err
	}

	cfg, err := configloader.NewFromBytes([]byte(args.Config))
	if err != nil {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}

	opts := []validation.Option{validation.WithLocal()}
	if args.Strict {
		opts = append(opts, validation.WithStrict())
	}

	warnings, valErr := cfg.Validate(mode, opts...)

	if warnings == nil {
		warnings = []string{}
	}

	result := map[string]any{
		"valid":    valErr == nil,
		"mode":     mode.String(),
		"strict":   args.Strict,
		"warnings": warnings,
	}
	if valErr != nil {
		result["error"] = valErr.Error()
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}
