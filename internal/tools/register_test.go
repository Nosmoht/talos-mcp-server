package tools

import (
	"sort"
	"strings"
	"testing"
)

// TestToolDescriptions_MapMatchesRegistered ties the centralised
// toolDescriptions map to the tools actually registered by Register: the map
// must list exactly the registered tool names, and every registered tool's
// Description must equal its map entry. This catches drift in either direction
// — a tool added via AddTool without a map entry, or a stale map entry whose
// constant is no longer wired into Register.
func TestToolDescriptions_MapMatchesRegistered(t *testing.T) {
	// readOnly=false registers the full surface (read-only + mutating tools).
	registered := listToolsWithMode(t, false)

	if len(registered) != len(toolDescriptions) {
		t.Errorf("registered tool count %d != toolDescriptions map size %d",
			len(registered), len(toolDescriptions))
	}

	for name, tool := range registered {
		want, ok := toolDescriptions[name]
		if !ok {
			t.Errorf("registered tool %q has no entry in toolDescriptions", name)
			continue
		}
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("registered tool %q has an empty Description", name)
		}
		if tool.Description != want {
			t.Errorf("registered tool %q Description does not match toolDescriptions[%q]:\n got: %q\nwant: %q",
				name, name, tool.Description, want)
		}
	}

	for name := range toolDescriptions {
		if _, ok := registered[name]; !ok {
			t.Errorf("toolDescriptions lists %q but Register does not register it", name)
		}
	}
}

// TestToolDescriptions_Disambiguation pins the agent-facing routing contract:
// every tool that overlaps a sibling must keep an explicit cross-reference in
// its description so a model picking between them is steered to the right one.
// If a future edit rewrites a description and drops the disambiguation, this
// test fails. Substrings are literal and case-sensitive.
func TestToolDescriptions_Disambiguation(t *testing.T) {
	registered := listToolsWithMode(t, false)

	// tool name → substrings its description must contain.
	wantSubstrings := map[string][]string{
		// talos_get is the low-level catch-all; it must point at the dedicated
		// tools for the common cases and state the single-node fan-out limit.
		"talos_get": {
			"talos_services",
			"talos_etcd",
			"talos_version",
			"talos_resource_definitions",
			// accuracy pin: multi-node COSI is rejected by Talos, not silently
			// single-noded — the description must not imply silent fan-out.
			"one-to-many",
		},
		// dedicated tools must point back at the generic talos_get they replace.
		"talos_services": {"talos_get"},
		"talos_etcd":     {"talos_get"},
		// the three log-ish tools must cross-reference each other.
		"talos_logs":   {"talos_dmesg", "talos_events"},
		"talos_dmesg":  {"talos_logs", "talos_events"},
		"talos_events": {"talos_logs", "talos_dmesg"},
		// config-write pair must disambiguate, and patch must name both patch
		// formats so the strategic-merge-vs-RFC-6902 limitation is visible.
		"talos_patch_config": {"talos_apply_config", "strategic", "RFC 6902"},
		// accuracy pin: the blocklist-disable claim must keep its path distinction
		// without advertising a bypass to the model — the authenticated path is
		// disabled when the blocklist is set, while maintenance mode is governed
		// separately by the insecure-mode allowlist gates (lifecycle.go returns into
		// the insecure branch before the blocklist guard).
		"talos_apply_config": {"talos_patch_config", "governed separately"},
	}

	for name, subs := range wantSubstrings {
		tool, ok := registered[name]
		if !ok {
			t.Errorf("expected tool %q to be registered, but it was not", name)
			continue
		}
		for _, sub := range subs {
			if !strings.Contains(tool.Description, sub) {
				t.Errorf("tool %q description is missing disambiguation substring %q\nfull description: %q",
					name, sub, tool.Description)
			}
		}
	}
}

// TestToolDescriptions_NoSafetyWordingRegression guards the mutating tools: the
// confirm / nodes / dry_run safety wording is load-bearing and must not be
// dropped by a description rewrite.
func TestToolDescriptions_NoSafetyWordingRegression(t *testing.T) {
	registered := listToolsWithMode(t, false)

	// NOTE: these are token-presence regression guards (catch a dropped guard
	// word), not imperative-force checks — a reviewer still owns "is the wording
	// still imperative". See team-red MEDIUM on substring-force limits.
	wantSafety := map[string][]string{
		// service_action: confirm always required AND the node-scope warning
		// (omitting nodes fans the action out to all talosconfig default nodes) AND
		// the cluster-wide-outage caution that its reboot/reset siblings also carry —
		// an omitted-nodes stop/restart hits every default node simultaneously.
		"talos_service_action": {"confirm=true", "targets ALL default nodes", "outage"},
		"talos_reboot":         {"confirm=true", "one node at a time"},
		"talos_upgrade":        {"confirm=true"},
		"talos_rollback":       {"confirm=true"},
		"talos_patch_config":   {"dry_run", "confirm=true"},
		"talos_reset":          {"confirm=true", "IRREVERSIBLE"},
		"talos_apply_config":   {"dry_run", "confirm=true"},
		"talos_meta":           {"confirm=true"},
	}

	names := make([]string, 0, len(wantSafety))
	for name := range wantSafety {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		tool, ok := registered[name]
		if !ok {
			t.Errorf("expected mutating tool %q to be registered, but it was not", name)
			continue
		}
		for _, sub := range wantSafety[name] {
			if !strings.Contains(tool.Description, sub) {
				t.Errorf("mutating tool %q description lost required safety wording %q\nfull description: %q",
					name, sub, tool.Description)
			}
		}
	}
}
