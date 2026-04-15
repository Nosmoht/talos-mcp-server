package completions

// knownServices is the curated list of Talos system services that appear in
// talos_services on a healthy node. Source: talosctl service list output on
// Talos v1.9. Keep sorted for deterministic completion order.
var knownServices = []string{
	"apid", "containerd", "cri", "etcd", "kubelet",
	"machined", "trustd", "udevd",
}

// applyModes mirrors the enum documented in internal/prompts/apply.go.
var applyModes = []string{"auto", "no_reboot", "reboot", "staged", "try"}

// commonNamespaces lists the COSI namespaces that appear in a default Talos
// cluster. Less-common namespaces still work at tool-call time; they just
// won't appear in autosuggest.
var commonNamespaces = []string{
	"cluster", "config", "cri", "files", "hardware",
	"k8s", "network", "runtime", "time", "v1alpha1",
}
