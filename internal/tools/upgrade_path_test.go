package tools

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Nosmoht/talos-mcp-server/internal/version"
)

// specUpgradePath is the scope of the upgrade-path check in the #265 triage
// comment. The version bounds below come from machinery compatibility/talos114
// (MinimumHostUpgradeVersion 1.12.0, MaximumHostDowngradeVersion 1.16.0).
const specUpgradePath = "spec #265 triage scope 1 (upgrade path via compatibility.UpgradeableFrom)"

// nodeVersionClient answers GetNodeVersion with a fixed version or error and
// records the upgrade request.
type nodeVersionClient struct {
	*mockClient

	ver *version.TalosVersion
	err error
}

func (c *nodeVersionClient) GetNodeVersion(_ context.Context, _ string) (*version.TalosVersion, error) {
	return c.ver, c.err
}

func runUpgrade(t *testing.T, client *nodeVersionClient, image string, skip bool) error {
	t.Helper()

	h := &Handlers{Client: client, SkipVersionCheck: skip}
	_, _, err := h.HandleUpgrade(context.Background(), nil, UpgradeArgs{
		Nodes:   []string{insecureTestEndpoint},
		Image:   image,
		Confirm: true,
	})

	return err
}

func nodeAt(major, minor, patch int) *nodeVersionClient {
	return &nodeVersionClient{
		mockClient: &mockClient{recordUpgrade: true},
		ver:        &version.TalosVersion{Major: major, Minor: minor, Patch: patch},
	}
}

// TestGivenUpgradeThatCannotBeVerified_WhenSkipFlagUnset_ThenUpgradeIsRefusedBeforeTheNodeIsTouched
// asserts per case that the handler refuses with the skip-flag hint and never
// sends the upgrade request.
func TestGivenUpgradeThatCannotBeVerified_WhenSkipFlagUnset_ThenUpgradeIsRefusedBeforeTheNodeIsTouched(t *testing.T) {
	t.Parallel()

	unreachable := &nodeVersionClient{
		mockClient: &mockClient{recordUpgrade: true},
		err:        status.Error(codes.Unavailable, "connection refused"),
	}

	tests := []struct {
		id     string
		client *nodeVersionClient
		image  string
	}{
		{id: "node_version_unavailable", client: unreachable, image: "ghcr.io/siderolabs/installer:v1.14.5"},
		{id: "host_older_than_minimum", client: nodeAt(1, 11, 0), image: "ghcr.io/siderolabs/installer:v1.14.5"},
		{id: "target_newer_than_machinery", client: nodeAt(1, 14, 5), image: "ghcr.io/siderolabs/installer:v1.15.0"},
	}

	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()

			err := runUpgrade(t, tc.client, tc.image, false)
			if err == nil || !strings.Contains(err.Error(), "TALOS_MCP_SKIP_VERSION_CHECK") {
				t.Errorf("%s: case=%s expected a refusal naming the skip flag, got %v", specUpgradePath, tc.id, err)
			}
			if tc.client.gotUpgradeReq != nil {
				t.Errorf("%s: case=%s expected no upgrade request after a refusal, got one", specUpgradePath, tc.id)
			}
		})
	}
}

// TestGivenPathTalosAllows_WhenUpgrading_ThenUpgradeProceeds asserts per case
// that the upgrade request reaches the client.
func TestGivenPathTalosAllows_WhenUpgrading_ThenUpgradeProceeds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id     string
		client *nodeVersionClient
		image  string
	}{
		{id: "next_minor", client: nodeAt(1, 13, 6), image: "ghcr.io/siderolabs/installer:v1.14.5"},
		{id: "skip_one_minor_from_minimum", client: nodeAt(1, 12, 0), image: "ghcr.io/siderolabs/installer:v1.14.5"},
		{id: "same_version_reinstall", client: nodeAt(1, 14, 5), image: "factory.talos.dev/installer/abc:v1.14.5"},
	}

	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()

			if err := runUpgrade(t, tc.client, tc.image, false); err != nil {
				t.Errorf("%s: case=%s expected the upgrade to proceed, got %v", specUpgradePath, tc.id, err)
			}
			if tc.client.gotUpgradeReq == nil {
				t.Errorf("%s: case=%s expected the upgrade request to reach the client, got none", specUpgradePath, tc.id)
			}
		})
	}
}

func TestGivenSkipFlag_WhenNodeVersionIsUnreadable_ThenUpgradeProceeds(t *testing.T) {
	t.Parallel()

	client := &nodeVersionClient{
		mockClient: &mockClient{recordUpgrade: true},
		err:        status.Error(codes.Unavailable, "connection refused"),
	}
	if err := runUpgrade(t, client, "ghcr.io/siderolabs/installer:v1.15.0", true); err != nil {
		t.Fatalf("%s: expected TALOS_MCP_SKIP_VERSION_CHECK to override the check, got %v", specUpgradePath, err)
	}
	if client.gotUpgradeReq == nil {
		t.Errorf("%s: expected the upgrade request to reach the client, got none", specUpgradePath)
	}
}

func TestGivenImageTagWithoutVersion_WhenUpgrading_ThenUpgradeProceedsWithoutTheCheck(t *testing.T) {
	t.Parallel()

	client := &nodeVersionClient{
		mockClient: &mockClient{recordUpgrade: true},
		err:        status.Error(codes.Unavailable, "connection refused"),
	}
	if err := runUpgrade(t, client, "ghcr.io/siderolabs/installer:latest", false); err != nil {
		t.Fatalf("%s: expected an unversioned tag to pass with a warning, got %v", specUpgradePath, err)
	}
	if client.gotUpgradeReq == nil {
		t.Errorf("%s: expected the upgrade request to reach the client, got none", specUpgradePath)
	}
}
