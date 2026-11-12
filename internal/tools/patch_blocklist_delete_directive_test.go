package tools

import (
	"strings"
	"testing"
)

func TestPatchDeleteOnAncestorOfBlockedPathRefused(t *testing.T) {
	const blocked = "cluster.etcd"
	client := newBlocklistProbeClient(t, singleDocConfig)

	err := dryRunPatchUnderBlocklist(t, client, []string{blocked}, "cluster:\n  $patch: delete\n")
	if err == nil || !strings.Contains(err.Error(), blocked) || len(client.reached) != 0 {
		t.Errorf("spec AC-44: expected=refusal naming %q before any Talos client call got=err:%v client calls:%v", blocked, err, client.reached)
	}
}

func TestTopLevelPatchDeleteRefusedWhileBlocklistSet(t *testing.T) {
	for _, blocked := range []string{"cluster.etcd", "machine.security", "machine.network.hostname"} {
		t.Run(blocked, func(t *testing.T) {
			client := newBlocklistProbeClient(t, singleDocConfig)

			err := dryRunPatchUnderBlocklist(t, client, []string{blocked}, "$patch: delete\n")
			if err == nil || !strings.Contains(err.Error(), blocked) || len(client.reached) != 0 {
				t.Errorf("spec AC-45: case=%s expected=refusal containing %q before any Talos client call got=err:%v client calls:%v",
					blocked, blocked, err, client.reached)
			}
		})
	}
}

func TestPatchDeleteUnderUnrelatedSiblingAccepted(t *testing.T) {
	const configWithHostname = "version: v1alpha1\nmachine:\n  type: worker\n  network:\n    hostname: node-a\n"
	client := newBlocklistProbeClient(t, configWithHostname)

	err := dryRunPatchUnderBlocklist(t, client, []string{"cluster.etcd"}, "machine:\n  network:\n    hostname:\n      $patch: delete\n")
	if err != nil || client.gotApply == nil || !client.gotApply.GetDryRun() {
		t.Errorf("spec AC-46: expected=ApplyConfiguration submitted with DryRun=true got=err:%v request:%v", err, client.gotApply)
	}
}
