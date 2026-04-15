package tools

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/validation"
)

// validateResult is the structured output for talos_validate. Note: the prior
// inline-map shape used a singular "error" string field on failure; the new
// schema exposes "errors" as an array of strings for consistency with
// "warnings". Callers that parsed the prior key must migrate.
type validateResult struct {
	Valid    bool     `json:"valid"`
	Mode     string   `json:"mode"`
	Strict   bool     `json:"strict"`
	Warnings []string `json:"warnings"`
	Errors   []string `json:"errors,omitzero"`
}

var validateOutputSchema = mustDeriveSchema[validateResult]()

// ValidateOutputSchema returns the JSON schema for HandleValidate.
func ValidateOutputSchema() *jsonschema.Schema { return validateOutputSchema }

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

	result := validateResult{
		Valid:    valErr == nil,
		Mode:     mode.String(),
		Strict:   args.Strict,
		Warnings: warnings,
	}
	if valErr != nil {
		result.Errors = []string{valErr.Error()}
	}

	return jsonResult(result)
}
