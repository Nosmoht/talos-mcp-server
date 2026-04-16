# Quorum / Cluster-Member Counting

When counting cluster members (etcd, Kubernetes, any consensus group) for a quorum or strict-majority decision:

- **Deduplicate by member ID before counting.** A Talos API proxy or gRPC stream aggregation can emit the same `EtcdMember` / `Member` across multiple `Messages[]` entries, so `len(msg.GetMembers())` is not a safe count.
- **Use the raft node `Id` (`uint64`), not hostname or peer URL.** Hostnames can be missing for learners or nodes that have not yet advertised a peer URL — `Id` is etcd's canonical identity and matches its own dedup semantics.
- **Record the ID type in the helper's doc comment** so reviewers can verify the dedup structure (`map[uint64]struct{}` for etcd, typed equivalent for other consensus groups).
- The strict-majority rule `(healthy - remove) > configured/2` is only sound when `configured` is the deduplicated count. An inflated count can let the gate pass when it must fail — a silent path to a dangerous mutation.

Applies to all preflight helpers under `internal/tools/` that inform mutating decisions (etcd remove/leave, k8s control-plane rotation, future consensus-group mutators). Canonical implementation: `fetchEtcdMemberCount` in `internal/tools/etcd_preflight.go`.

**Learner caveat:** etcd learners (`IsLearner=true`) do not count toward quorum. Dedup alone does not filter learners — any helper that computes voting-member majority must also filter by `IsLearner`.
