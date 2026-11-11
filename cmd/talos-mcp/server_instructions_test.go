package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	instructionsGuardTerms = []string{"confirm", "dry_run", "no_reboot"}

	instructionsRoutedTools = []string{
		"talos_health", "talos_services", "talos_logs", "talos_version", "talos_etcd",
		"talos_dmesg", "talos_events", "talos_containers", "talos_processes",
		"talos_list_files", "talos_read_file", "talos_get", "talos_resource_definitions",
	}
)

func serverInstructions(t *testing.T, acID string) string {
	t.Helper()

	c2s, s2c := serveOverPipes(t, newCompatServer(t, newFakeBackend(t)))
	session := connectLegacyClient(t, &mcp.IOTransport{Reader: s2c, Writer: c2s}, acID)
	t.Cleanup(func() { _ = session.Close() })

	instructions := session.InitializeResult().Instructions
	if instructions == "" {
		t.Fatalf("spec %s: precondition expected non-empty server Instructions got empty", acID)
	}
	return instructions
}

func TestInstructionsCarryNoGuardSemantics(t *testing.T) {
	lower := strings.ToLower(serverInstructions(t, "AC-18"))

	var found []string
	for _, term := range instructionsGuardTerms {
		if strings.Contains(lower, term) {
			found = append(found, term)
		}
	}
	if len(found) != 0 {
		t.Errorf("spec AC-18: expected none of %v in the Instructions got %v", instructionsGuardTerms, found)
	}
}

func TestInstructionsRouteReadQuestions(t *testing.T) {
	instructions := serverInstructions(t, "AC-19")

	var missing []string
	for _, tool := range instructionsRoutedTools {
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(tool) + `\b`).MatchString(instructions) {
			missing = append(missing, tool)
		}
	}
	if len(missing) != 0 {
		t.Errorf("spec AC-19: expected the Instructions to name every routed tool got missing %v", missing)
	}
}

func TestInstructionsPointToToolDescriptions(t *testing.T) {
	instructions := serverInstructions(t, "AC-43")

	sentences := strings.FieldsFunc(strings.ToLower(instructions), func(r rune) bool { return r == '.' || r == '\n' })
	for _, sentence := range sentences {
		if strings.Contains(sentence, "guard") && strings.Contains(sentence, "description") {
			return
		}
	}
	t.Errorf("spec AC-43: expected a sentence containing both %q and %q got sentences %q", "guard", "description", sentences)
}
