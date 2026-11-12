package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	cosiListCap        = 100
	cosiListOverCap    = cosiListCap + 1
	cosiListReadBound  = 10 * time.Second
	cosiListResourceID = "talos://" + testNode + "/resource/runtime/MetaKey"
)

type cosiListPayload struct {
	Items     []json.RawMessage `json:"items"`
	Truncated bool              `json:"truncated"`
}

func readSeededMetaKeyList(t *testing.T, seeded int) cosiListPayload {
	t.Helper()

	ids := make([]string, seeded)
	for i := range ids {
		ids[i] = fmt.Sprintf("seeded-%03d", i)
	}
	h := newCOSIHandlers(t, ids)

	server := mcp.NewServer(&mcp.Implementation{Name: "cosi-list-cap-test", Version: "0.0.0"}, nil)
	Register(server, h.Client, h.AllowedNodes)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), cosiListReadBound)
	t.Cleanup(cancel)
	go func() {
		_ = server.Run(ctx, serverTransport)
	}()

	session, err := mcp.NewClient(&mcp.Implementation{Name: "cosi-list-cap-client", Version: "0.0.0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("setup: client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: cosiListResourceID})
	if err != nil {
		t.Fatalf("read %s with %d seeded resources: %v", cosiListResourceID, seeded, err)
	}
	if len(res.Contents) == 0 {
		t.Fatalf("read %s: empty resource contents", cosiListResourceID)
	}

	var payload cosiListPayload
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &payload); err != nil {
		t.Fatalf("decode %s payload: %v", cosiListResourceID, err)
	}
	return payload
}

func TestCOSIResourceListCappedAtHundred(t *testing.T) {
	payload := readSeededMetaKeyList(t, cosiListOverCap)
	if len(payload.Items) != cosiListCap {
		t.Errorf("spec AC-35: expected %d items got %d", cosiListCap, len(payload.Items))
	}
}

func TestCOSIResourceListOverCapFlagsTruncated(t *testing.T) {
	payload := readSeededMetaKeyList(t, cosiListOverCap)
	if !payload.Truncated {
		t.Errorf("spec AC-36: expected truncated=true with %d seeded resources got false", cosiListOverCap)
	}
}

func TestCOSIResourceListAtCapNotTruncated(t *testing.T) {
	payload := readSeededMetaKeyList(t, cosiListCap)
	if payload.Truncated {
		t.Errorf("spec AC-47: expected truncated absent or false with %d seeded resources got true", cosiListCap)
	}
}
