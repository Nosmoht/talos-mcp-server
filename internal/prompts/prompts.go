// Package prompts registers MCP prompt handlers with the server.
// Prompts are pure text templates — they produce user-role messages that
// instruct the AI agent on which tools to call and in what order.
// No Talos client is needed here.
package prompts

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Register adds all talos-mcp prompts to server.
// apply-config is only registered when readOnly is false because it
// references talos_patch_config, which is not registered in read-only mode.
func Register(server *mcp.Server, readOnly bool) {
	server.AddPrompt(diagnoseNodePrompt(), handleDiagnoseNode)
	server.AddPrompt(preUpgradePrompt(), handlePreUpgrade)
	server.AddPrompt(investigateEtcdPrompt(), handleInvestigateEtcd)
	server.AddPrompt(debugServicePrompt(), handleDebugService)
	if !readOnly {
		server.AddPrompt(applyConfigPrompt(), handleApplyConfig)
	}
}

// textMsg wraps text in a single user-role PromptMessage result.
func textMsg(text string) *mcp.GetPromptResult {
	return &mcp.GetPromptResult{
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: text}},
		},
	}
}

// requireArg extracts a required argument from req; returns an error if blank or missing.
func requireArg(req *mcp.GetPromptRequest, name string) (string, error) {
	v := req.Params.Arguments[name]
	if v == "" {
		return "", fmt.Errorf("prompt %q requires argument %q", req.Params.Name, name)
	}
	return v, nil
}

// optionalArg extracts an optional argument; returns defaultVal if blank or missing.
func optionalArg(req *mcp.GetPromptRequest, name, defaultVal string) string {
	if v := req.Params.Arguments[name]; v != "" {
		return v
	}
	return defaultVal
}
