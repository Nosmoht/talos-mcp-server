package prompts

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func applyConfigPromptRequest(mode string) *mcp.GetPromptRequest {
	return &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{
			Name:      "apply-config",
			Arguments: map[string]string{"patch": `{"machine":{}}`, "node": "192.0.2.10", "mode": mode},
		},
	}
}

// TestSpecAC31_GivenRebootMode_WhenApplyConfigPromptRendered_ThenFailsWithNoRebootGuidance
// covers one predicate (ac.md AC-31): the apply-config prompt refuses mode
// "reboot" with the no_reboot + talos_reboot guidance.
func TestSpecAC31_GivenRebootMode_WhenApplyConfigPromptRendered_ThenFailsWithNoRebootGuidance(t *testing.T) {
	_, err := handleApplyConfig(context.Background(), applyConfigPromptRequest("reboot"))
	if err == nil {
		t.Fatalf("spec ac.md AC-31: expected=mode \"reboot\" is refused got=rendered prompt")
	}
	for _, want := range []string{"no_reboot", "talos_reboot"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("spec ac.md AC-31: expected=error text containing %q got=%q", want, err.Error())
		}
	}
}

// TestSpecAC31_GivenNoRebootMode_WhenApplyConfigPromptRendered_ThenRendersWithThatMode
// is the negative case of ac.md AC-31: the refusal is specific to "reboot", so
// the advertised replacement mode no_reboot still renders.
func TestSpecAC31_GivenNoRebootMode_WhenApplyConfigPromptRendered_ThenRendersWithThatMode(t *testing.T) {
	res, err := handleApplyConfig(context.Background(), applyConfigPromptRequest("no_reboot"))
	if err != nil {
		t.Fatalf("spec ac.md AC-31: expected=mode no_reboot renders got=%v", err)
	}
	var text string
	if len(res.Messages) > 0 {
		if tc, ok := res.Messages[0].Content.(*mcp.TextContent); ok {
			text = tc.Text
		}
	}
	if !strings.Contains(text, "Mode: no_reboot") {
		t.Errorf("spec ac.md AC-31: expected=rendered prompt naming \"Mode: no_reboot\" got=%q", text)
	}
}
