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
| `10-rook-install` | cert-manager (operator webhooks need it) + Rook operator v1.18.6 |
| `20-cephcluster` | source `CephCluster` `mon.count=3`, loopback-PVC OSDs, wait Ready |
| `30-operator-deploy` | `docker build` → `k3d image import` → `helm install` (local.yaml) |
| `40-remote-kubeconfig` | target ns + installer RBAC + SA-token kubeconfig Secret |
| `50-apply-arbiter` | `RemoteCluster` + `RemoteArbiter`, wait `state=Ready` |
| `60-assert-baseline` | 4 mons in quorum, `ext-*` arbiter already voting |
| `70-kill-mon` | scale one source mon Deployment to 0 |
| `80-assert-survival` | ≥3 mons, arbiter still voting, not HEALTH_ERR/no-quorum |

`NEGATIVE_CONTROL=1` kills a *second* source mon (2/4) and inverts step 80 to
expect quorum **lost** — proving the positive assertion isn't vacuous.

## Namespaces

- `rook-ceph` — source Rook Ceph (mons, mgr, OSDs, toolbox).
- `arbiter-operator` — the operator, the CRs, and the accesskeyRef Secret
  (`makeRemoteClient` reads the Secret from the RemoteCluster's own namespace).
- `external-arbiter` — where the arbiter mon Deployment lands (`spec.namespace`).
