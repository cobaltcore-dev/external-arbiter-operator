# Quorum-survival e2e harness

Proves the operator's reason to exist: **kill a source Ceph mon and quorum
survives, because the arbiter mon is a voting member.** Realises issues #70
(quorum) and #71 (fault injection). Design:
[`docs/superpowers/specs/2026-09-03-quorum-survival-harness-design.md`](../../../docs/superpowers/specs/2026-09-03-quorum-survival-harness-design.md).

## Run

```bash
make test-e2e-quorum              # full run, tears down on exit
KEEP_CLUSTER=1 make test-e2e-quorum   # leave the k3d cluster up to debug
make test-e2e-quorum-teardown     # delete the k3d cluster
```

Needs `docker`, `k3d`, `kubectl`, `jq`, `helm`. **~16 GB RAM, ~12 min cold.**
Real Ceph is never small — the one-cluster/two-namespace topology removes the
*networking* footprint (issue #69), not the *Ceph* footprint. Not part of
`make test`; gate it behind an explicit CI label.

## What each step does

| Step | |
|---|---|
| `00-k3d-up` | k3d cluster, 1 server + 2 agents (mon anti-affinity wants 3 nodes) |
| `10-rook-install` | cert-manager (operator webhooks need it) + Rook operator v1.18.6, CSI disabled |
| `15-osd-loopdev` | privileged DaemonSet: sparse file → `losetup` loop device per node (k3d has no spare disk) |
| `20-cephcluster` | source `CephCluster` `mon.count=3`, OSDs on the loop devices (`useAllDevices`), wait Ready |
| `30-operator-deploy` | `docker build` → `k3d image import` → `helm install` (local.yaml) |
| `40-remote-kubeconfig` | target ns + installer RBAC + SA-token kubeconfig Secret |
| `50-apply-arbiter` | `RemoteCluster` + `RemoteArbiter`, wait `state=Ready`, assert arbiter Deployment (lookup label) |
| `60-assert-baseline` | 4 mons in quorum, `ext-*` arbiter already voting |
| `70-kill-mon` | scale one source mon Deployment to 0, record its ceph mon id |
| `80-assert-survival` | victim absent from quorum, ≥3 mons, arbiter still voting |

`NEGATIVE_CONTROL=1` kills a *second* source mon (2/4) and inverts step 80 to
expect quorum **lost**. The positive run alone is a smoke test; only the
positive+negative pair isolates the arbiter's vote as load-bearing — run both
for the full proof.

## Namespaces

- `rook-ceph` — source Rook Ceph (mons, mgr, OSDs, toolbox).
- `arbiter-operator` — the operator, the CRs, and the accesskeyRef Secret
  (`makeRemoteClient` reads the Secret from the RemoteCluster's own namespace).
- `external-arbiter` — where the arbiter mon Deployment lands (`spec.namespace`).

## Caveats

- `KEEP_CLUSTER=1` re-runs on the *same* cluster are not idempotent (a scaled-to-0
  victim mon, stale namespaces, and the loop backing file persist). For a clean
  run, let teardown delete the cluster (the default) or `make test-e2e-quorum-teardown`.
- Running individual steps out of order reuses `$VICTIM_FILE`
  (`${TMPDIR:-/tmp}/arbiter-e2e-victims`); clear it between unrelated manual runs.
