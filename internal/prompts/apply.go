package prompts

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func applyConfigPrompt() *mcp.Prompt {
	return &mcp.Prompt{
		Name:        "apply-config",
		Description: "Safely apply a machine config patch to a Talos node. Guides a health check → dry-run → user confirmation → apply workflow.",
		Arguments: []*mcp.PromptArgument{
			{Name: "patch", Description: "Machine config patch as a JSON or YAML string.", Required: true},
			{Name: "node", Description: "Target node IP or hostname.", Required: true},
			{Name: "mode", Description: "Apply mode: auto, reboot, no_reboot, staged, or try. Defaults to try.", Required: false},
		},
	}
}

func handleApplyConfig(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	patch, err := requireArg(req, "patch")
	if err != nil {
		return nil, err
	}
	node, err := requireArg(req, "node")
	if err != nil {
		return nil, err
	}

	mode := optionalArg(req, "mode", "try")
	switch mode {
	case "auto", "reboot", "no_reboot", "staged", "try":
		// valid
	default:
		return nil, fmt.Errorf("prompt %q: unknown mode %q: must be auto, reboot, no_reboot, staged, or try", req.Params.Name, mode)
	}

	msg := fmt.Sprintf(`Apply a machine config patch to Talos node %s.
Mode: %s

Patch to apply:
%s

Follow these steps in order. Do NOT apply the patch (dry_run=false) until you reach Step 4 and receive explicit user confirmation.

Step 1 — Pre-flight health check
Check cluster health using talos_health. If any check fails, stop and report — do not proceed with a config change on an unhealthy cluster.

Step 2 — Dry-run validation
Validate the patch using talos_patch_config with dry_run=true targeting node %s with mode=%s. This is safe — it only validates without making any changes. If the dry run reports errors or warnings, stop and report them. Do not proceed if there are errors.

Step 3 — Present plan to the user
Show the user:
  - The patch that will be applied
  - Target node: %s
  - Apply mode: %s
  - The dry-run output from Step 2

Then ask: "The dry-run succeeded. Do you want to apply this config patch to %s with mode=%s? Reply 'yes' to confirm."

Step 4 — Apply (only after explicit user confirmation)
Only if the user explicitly confirms with "yes", apply the patch using talos_patch_config with dry_run=false and confirm=true targeting node %s with mode=%s.

Report the apply output. If mode is "try", remind the user that the config will revert after 60 seconds unless a follow-up apply is issued to confirm it permanently.`,
		node, mode, patch,
		node, mode,
		node, mode,
		node, mode,
		node, mode)

	return textMsg(msg), nil
}
