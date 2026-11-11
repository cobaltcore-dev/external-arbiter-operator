<!--
# SPDX-FileCopyrightText: Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
#
# SPDX-License-Identifier: Apache-2.0
-->
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

## RGW-chaos extension (data plane)

`run.sh` proves the **control plane** (quorum survives a mon loss). The chaos
extension proves the **data plane**: that the realistic arbiter/mon fault
scenarios in
[`docs/rgw-failure-scenarios.md`](../../../docs/rgw-failure-scenarios.md) do
**not** interrupt RGW read/write/list traffic — or, where interruption is the
correct outcome, that it happens for the right reason.

```bash
# full flow from scratch (bring cluster up, then run all chaos steps):
FULL=1 bash test/e2e/quorum/chaos.sh

# against a cluster already up from `KEEP_CLUSTER=1 make test-e2e-quorum`:
bash test/e2e/quorum/chaos.sh

# a single scenario:
bash test/e2e/quorum/chaos.sh 74-partition-arbiter
```

The oracle is an in-cluster pod (`25-objectstore.sh` stands up the RGW store +
user) that loops PUT→GET→LIST against the RGW ClusterIP and logs one OK/ERR line
per iteration. Each chaos step baselines the error count, injects a fault, holds
with traffic, restores state (via an `EXIT` trap), then asserts the S3 error
**delta**.

| Step | Scenario | Injection | RGW pass criterion |
|---|---|---|---|
| `25-objectstore` | — | apply `CephObjectStore` + user, wait Ready | setup (not a test) |
| `71-bad-public-addr` | S1 | make the arbiter address unreachable, then kill a source mon | **errors EXPECTED** — proves the advertised addr is load-bearing |
| `72-teardown-race` | S2 | delete the `RemoteArbiter` under load | zero S3 errors; reports config churn |
| `73-restart-storm` | S3 | force operator drift updates (annotate `rook-config-override`) | zero S3 errors; arbiter still voting |
| `74-partition-arbiter` | S4/S9 | NetworkPolicy drop msgr 3300/6789 to the arbiter | zero S3 errors (`NEGATIVE_CONTROL=1` also kills a mon → errors expected) |
| `75-stale-mon-host` | S5 | fail over a source mon so `mon_host` changes | zero S3 errors; reports arbiter Secret convergence |
| `76-config-churn` | S10 | add/remove a throwaway `externalMonIDs` entry | zero S3 errors; counts `rook-ceph-config` rewrites |
| `77-stretch-detect` | S8 | validation: assert non-stretch + flag the missing stretch-mode guard | known-gap marker (see research doc Follow-up #2) |

Tunables (env): `HOLD` (hold window per step), `ISOLATE_SETTLE`, `STORM_CYCLES`,
`CHURN_CYCLES`, `STRETCH_GUARD_EXPECTED`. The oracle is ClusterIP-only by design
(RGW service is ClusterIP), so traffic originates **inside** the cluster.

### Run against a Lima VM (no Docker, no k3d)

The chaos steps need a running cluster, not specifically k3d. If you already have
a Rook-Ceph + operator + arbiter cluster from the repo's
[README quick-start](../../../README.md) Lima VM, point `kubectl` at it and run
the chaos steps directly — no Docker required (the VM uses containerd):

```bash
export KUBECONFIG=~/.lima/k8s/copied-from-guest/kubeconfig.yaml

bash test/e2e/quorum/chaos.sh                          # all chaos steps
bash test/e2e/quorum/chaos.sh 74-partition-arbiter     # single scenario
NEGATIVE_CONTROL=1 bash test/e2e/quorum/chaos.sh 74-partition-arbiter
```

Notes:
- Do **not** set `FULL=1` on the VM — that is the k3d from-scratch path (needs
  Docker/k3d). Without `FULL`, `chaos.sh` assumes the cluster is already up and
  never touches k3d; teardown is a no-op when k3d is absent.
- `25-objectstore.sh` is idempotent: if `my-store` is already `Ready` and the S3
  user Secret exists (the quick-start deploys both), it skips the apply.
- Cilium in the quick-start VM **enforces NetworkPolicy**, so the partition
  scenarios (`71`, `74`) are real there. The in-cluster probe pod pulls
  `amazon/aws-cli` — the VM needs registry access for the first run.
- The base quorum proof works on the VM too, since the quick-start already has a
  4-mon quorum (`a,b,c,ext-a`):
  ```bash
  bash test/e2e/quorum/60-assert-baseline.sh \
    && bash test/e2e/quorum/70-kill-mon.sh \
    && bash test/e2e/quorum/80-assert-survival.sh
  ```

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
