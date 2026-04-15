// Package resources implements MCP resource and resource template handlers
// for the Talos MCP server.
package resources

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// Handlers holds the Talos client and exposes MCP resource handler methods.
type Handlers struct {
	Client       *talos.Client
	AllowedNodes *talos.NodeAllowlist
}

// Register registers all static MCP resources and resource templates on server.
func Register(server *mcp.Server, client *talos.Client, allowedNodes *talos.NodeAllowlist) {
	h := &Handlers{Client: client, AllowedNodes: allowedNodes}

	server.AddResource(&mcp.Resource{
		URI:         "talos://cluster/version",
		Name:        "talos-version",
		Description: "Talos version information from the cluster's default endpoint.",
		MIMEType:    "application/json",
	}, h.handleVersion)

	server.AddResource(&mcp.Resource{
		URI:         "talos://cluster/resource-definitions",
		Name:        "talos-resource-definitions",
		Description: "All available Talos COSI resource types with their aliases and default namespaces. Read this first to discover what types can be queried via resource templates.",
		MIMEType:    "application/json",
	}, h.handleResourceDefinitions)

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "talos://{node}/resource/{namespace}/{type}",
		Name:        "talos-resource-list",
		Description: "List all COSI resources of a given type in a namespace on a specific node. Read talos://cluster/resource-definitions to discover available types and namespaces.",
		MIMEType:    "application/json",
	}, h.handleCOSIResource)

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "talos://{node}/resource/{namespace}/{type}/{id}",
		Name:        "talos-resource-get",
		Description: "Get a specific COSI resource by namespace, type, and ID on a specific node.",
		MIMEType:    "application/json",
	}, h.handleCOSIResource)
}

// ParseCOSIURI parses a COSI resource URI of the form:
//
//	talos://{node}/resource/{namespace}/{type}
//	talos://{node}/resource/{namespace}/{type}/{id}
//
// url.Parse treats the part after // as the authority, so for
// talos://10.0.0.1/resource/runtime/MachineStatus it gives:
//
//	Hostname() = "10.0.0.1"
//	Path       = "/resource/runtime/MachineStatus"
//
// Hostname() (not Host) is used so that an optional port — e.g.
// talos://10.0.0.1:50000/... — is stripped before passing the node
// address to the Talos client.
func ParseCOSIURI(rawURI string) (node, namespace, resourceType, resourceID string, err error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return "", "", "", "", fmt.Errorf("parse URI: %w", err)
	}
	if u.Scheme != "talos" {
		return "", "", "", "", fmt.Errorf("unexpected scheme %q, want \"talos\"", u.Scheme)
	}

	node = u.Hostname()
	if node == "" {
		return "", "", "", "", fmt.Errorf("missing node in URI %q", rawURI)
	}

	// path is "/resource/{namespace}/{type}" or "/resource/{namespace}/{type}/{id}"
	// Split and trim leading slash.
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "resource" {
		return "", "", "", "", fmt.Errorf("invalid URI path %q: want /resource/{namespace}/{type}[/{id}]", u.Path)
	}

	namespace, err = url.PathUnescape(parts[1])
	if err != nil {
		return "", "", "", "", fmt.Errorf("unescape namespace: %w", err)
	}
	resourceType, err = url.PathUnescape(parts[2])
	if err != nil {
		return "", "", "", "", fmt.Errorf("unescape type: %w", err)
	}
	if len(parts) >= 4 && parts[3] != "" {
		resourceID, err = url.PathUnescape(parts[3])
		if err != nil {
			return "", "", "", "", fmt.Errorf("unescape id: %w", err)
		}
	}

	if namespace == "" {
		return "", "", "", "", fmt.Errorf("empty namespace in URI %q", rawURI)
	}
	if resourceType == "" {
		return "", "", "", "", fmt.Errorf("empty type in URI %q", rawURI)
	}

	return node, namespace, resourceType, resourceID, nil
}
