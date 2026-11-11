<!--
# SPDX-FileCopyrightText: Copyright 2024 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
#
# SPDX-License-Identifier: Apache-2.0
-->

# Research: Error Scenarios Affecting RGW Traffic

## Purpose and Scope

This document analyses the `external-arbiter-operator` architecture and implementation
to identify **error situations that can interrupt RGW (S3) read / write / list traffic**.

The operator's entire reason for existence is *control-plane quorum resilience*: it adds a
third Ceph monitor (`mon.ext-a`) on a remote cluster so a single-AZ or interconnect failure
cannot drop a 2-mon cluster below majority. RGW is a **mon client**: every RGW daemon keeps a
live connection to the mon quorum to fetch OSD maps, auth tickets, and pool metadata. Therefore
**anything that destabilises the mon quorum, the mon map, or the mon endpoint list directly
threatens S3 availability.**

The guiding requirement for this analysis: **RGW traffic must stay uninterrupted at all times.**
Scenarios below are ranked by their potential to interrupt `GET` / `PUT` / `LIST`.

> **Note on "arbiter" naming:** there are two layers that can be confused. (1) *Rook stretch
> arbiter* — a Rook-native feature. (2) *This operator's external arbiter mon* — an extra mon
> deployed off-cluster. This document is about (2). A critical finding is that Rook **ignores**
> `externalMonIDs` when the cluster is in stretch/external mode (see Scenario 8).

### Relationship to the existing quorum-survival harness

A sibling effort — branch `docs/quorum-survival-harness`, design doc
`docs/superpowers/specs/2026-09-03-quorum-survival-harness-design.md`, harness under
`test/e2e/quorum/` (issues #70/#71) — already proves the **core control-plane property**:

> Kill a source mon and Ceph keeps quorum **because** the arbiter mon is a voting member
> (positive run: 3/4 survives; negative control: 2/4 loses quorum).

That harness is deliberately scoped and **explicitly defers the exact thing this document is
about**. Its "Does NOT prove" list states: *"Data-path resilience … no client workload runs
during the kill."* It also runs single-cluster, ClusterIP-only, and does not exercise the
NodePort/LoadBalancer public-address paths.

**This document therefore extends that harness rather than duplicating it.** The scenarios below
add (a) a **live RGW traffic oracle** during fault injection, and (b) fault classes beyond
"kill one mon" (bad public-addr, teardown race, stretch-mode, config churn, stale `mon_host`,
port reachability). The proposed simulations reuse the existing `test/e2e/quorum/` step
structure, `lib.sh` helpers (`ceph_exec`, `quorum_json`, `toolbox_pod`), and conventions
(numbered steps, `NEGATIVE_CONTROL`, victim-file assertions).

---

## How RGW Depends on the Mon Quorum (Background)

RGW availability is coupled to the mon control plane through several paths that the operator
touches:

1. **Mon quorum must have majority.** If quorum is lost, the cluster stops serving map updates;
   OSDs cannot be marked up/down, PGs cannot peer, and RGW requests block or time out. Existing
   RGW connections may survive briefly on cached maps but new ops that need a map (LIST over
   many shards, PUT that triggers rebalancing, auth renewal) stall.
2. **The mon endpoint list (`mon_host`).** RGW and all clients read mon endpoints from
   `rook-ceph-config` / the mon-endpoints ConfigMap. If this list is rewritten to include an
   **unreachable** external mon address, clients waste connection attempts on a dead endpoint,
   adding latency and, under partition, risking a stall.
3. **The mon map itself.** When the external mon joins, the live monmap gains a member. If that
   member advertises a bogus `--public-addr`, it is *in the monmap but unreachable*, which
   degrades quorum arithmetic (3 mons on paper, 2 reachable) and can produce effective quorum
   loss during a partition.
4. **Rook's `saveMonConfig()` churn.** Adding / removing an ID in `CephCluster.Spec.Mon.ExternalMonIDs`
   causes Rook to regenerate the mon config consumed by every client, including RGW
   (`rook/pkg/operator/ceph/cluster/mon/health.go:491`). Config churn can cause RGW connection
   resets.

Key code references:
- Mon ID reservation and `ExternalMonIDs` mutation: `pkg/controller/remotearbiter_controller.go:364`
- External mon `--public-addr` determination: `pkg/controller/remotearbiter_controller.go:633`
- Monmap init container (`monmaptool ... ext.monmap`): `pkg/controller/remotearbiter_controller.go:789`
- Deletion / `ExternalMonIDs` removal: `pkg/controller/remotearbiter_controller.go:1516`
- Rook's handling of external mons: `rook/pkg/operator/ceph/cluster/mon/health.go:436`

---

## Failure Scenario Catalogue

Each scenario lists: **trigger**, **mechanism**, **expected RGW impact**, **severity**, and a
**simulation approach** runnable against a live (dev/lab) cluster.

> ⚠️ **Safety:** Every simulation below is intended for a **lab / dev cluster only**
> (e.g. the Lima VM in the README quick-start). Several deliberately degrade quorum or the mon
> map. **Do not run against production.** Each script must snapshot state first and restore on
> exit.

### Scenario 1 — External mon advertises an unreachable `--public-addr`

| | |
|---|---|
| **Trigger** | `Service.type=ClusterIP` on the remote cluster (ClusterIP is not routable cross-cluster), or a stale/misassigned NodeIP / LoadBalancer IP. |
| **Mechanism** | `determinePublicAddress` (`remotearbiter_controller.go:633`) feeds `--public-addr`. A wrong value puts a mon in the monmap that clients cannot reach. |
| **RGW impact** | Mon is counted (3/3 on paper) but only 2 are reachable. During a single-AZ partition, quorum effectively drops to 2 reachable → 1, below majority → **S3 fully stalls**. Even without a partition, RGW may spend time dialing the dead endpoint, adding tail latency to LIST/PUT. |
| **Severity** | **Critical** — defeats the operator's purpose while appearing healthy. |
| **Simulation** | Patch the arbiter's Service to `ClusterIP` (or set an obviously bogus NodeIP), let the operator reconcile, then run `ceph mon dump` to confirm the external mon's advertised addr, attempt `ceph ping mon.ext-a` from a mon pod, and run continuous S3 traffic while partitioning one of the Rook mons. Expect S3 errors once the partition drops reachable mons below majority. |

### Scenario 2 — `ExternalMonIDs` removed while external mon is still live (teardown ordering)

| | |
|---|---|
| **Trigger** | Deleting the `RemoteArbiter` (or operator crash mid-teardown). `cleanUpCephCluster` removes the ID from `ExternalMonIDs` (`remotearbiter_controller.go:1516`) and if the remote client cannot be built, remote resource cleanup is **silently skipped** (`:1580`), leaving the mon running. |
| **Mechanism** | With the ID gone from `ExternalMonIDs`, Rook's `removeMonsFromQuorumStatusResponse` protection no longer applies (`health.go:502`); Rook may treat the still-live external mon as a stray / act on failover logic. `saveMonConfig()` rewrites client config. |
| **RGW impact** | Config churn → RGW connection resets; if Rook removes/relocates mons based on the now-inconsistent view, transient quorum disruption → **LIST/PUT errors** during the window. |
| **Severity** | **High.** |
| **Simulation** | Start continuous S3 traffic, `kubectl delete remotearbiter ...`, and watch `ceph quorum_status` + `rook-ceph-config` resourceVersion + RGW error rate. Also simulate the "remote unreachable during delete" path by cordoning/blocking the remote API before deletion. |

### Scenario 3 — Arbiter mon restart storm (template non-determinism / drift loop)

| | |
|---|---|
| **Trigger** | Historically: random map ordering in template mon selection caused perpetual regeneration (fixed; see comment `remotearbiter_controller.go:1193`). A regression, or genuine upstream drift (mon deployment edited by Rook), can re-trigger frequent `restartArbiterDeployment` (`:865`). |
| **Mechanism** | Each restart drops the external mon out of quorum until it re-joins; repeated flapping churns the monmap. |
| **RGW impact** | Each flap momentarily reduces reachable mons; on a 2-AZ+arbiter cluster, a flap coinciding with any other mon blip drops below majority → **S3 stalls in bursts**. Monmap churn also forces client reconnects. |
| **Severity** | **High.** |
| **Simulation** | Repeatedly bump a watched source resource (e.g. annotate the Rook mon deployment / `rook-config-override` ConfigMap) to force drift updates, observe arbiter deployment restart count and `ceph mon stat` membership flaps while S3 traffic runs. |

### Scenario 4 — Cross-cluster network partition isolating the external mon

| | |
|---|---|
| **Trigger** | The inter-cluster link (operator/Rook cluster ↔ arbiter cluster) fails. README explicitly assumes mutual L3 reachability (`README.md:159`). |
| **Mechanism** | External mon becomes unreachable; it drops from quorum. Remaining 2 AZ mons still form majority *as long as the AZ interconnect is up*. |
| **RGW impact** | If **only** the arbiter link fails: no impact (2/3 still = majority) — this is the *designed* resilience. If the arbiter link fails **and** then one AZ mon also fails/partitions: quorum lost → **S3 down**. This tests that the arbiter does not become a new single point of amplification. |
| **Severity** | **Medium** (designed-for) → **Critical** (combined failure). |
| **Simulation** | Apply a NetworkPolicy / drop iptables rules on the arbiter messenger ports (3300/6789) to simulate the partition; verify quorum stays healthy with 2/3; then additionally kill one Rook mon and confirm RGW outage onset and recovery timing. |

### Scenario 5 — Wrong / stale `mon_host` copied into the external mon's monmap

| | |
|---|---|
| **Trigger** | The env-var Secret (`mon_host`, `mon_initial_members`) is copied into the arbiter (`createArbiterEnvVarSecret`, `remotearbiter_controller.go:1007`) and used by the `init-monmap` container (`:789`). If the source is stale (mon failover happened, Secret not yet re-synced), the arbiter builds its monmap from outdated endpoints. |
| **Mechanism** | External mon tries to join using stale peer addresses → slow/failed join, or it joins with a divergent monmap view. |
| **RGW impact** | Delayed or failed arbiter join → reduced effective quorum resilience; in worst case monmap inconsistency propagates, destabilising the quorum RGW depends on. |
| **Severity** | **Medium–High.** |
| **Simulation** | Force a Rook mon failover (delete a mon pod / scale a mon out) so `mon_host` changes, then observe whether the arbiter's copied Secret and monmap converge before the next reconcile. Run S3 traffic to detect disruption during the convergence gap. |

### Scenario 6 — Operator cannot reconcile during active degradation (readiness gating)

| | |
|---|---|
| **Trigger** | The operator refuses to progress unless `CephCluster.Status.Phase == Ready` (`remotearbiter_controller.go:212`) and a mon deployment is fully ready (`:264`). During an active quorum scare the cluster is *not* Ready. |
| **Mechanism** | The operator is **not** a self-healing agent during an outage — it will not fix or re-add the arbiter while the cluster is degraded, exactly when it is most needed. |
| **RGW impact** | Indirect: prolongs an outage because the arbiter cannot be (re)deployed to restore quorum until the cluster is already healthy — a chicken-and-egg risk extending **S3 downtime**. |
| **Severity** | **Medium.** |
| **Simulation** | Degrade the cluster (stop one mon so Phase != Ready), delete/break the arbiter deployment on the remote, and confirm the operator does **not** recover the arbiter until the cluster returns to Ready. Measure the extra downtime window with S3 traffic. |

### Scenario 7 — Unbounded error backoff delays arbiter recovery

| | |
|---|---|
| **Trigger** | Transient remote-cluster API unreachability. No custom `RateLimiter` is wired in `main.go` (`cmd/manager/main.go` ~`:188`), so controller-runtime exponential backoff applies; recovery can lag well beyond the 1m `CheckInterval`. |
| **Mechanism** | After repeated failures, requeue intervals grow; the arbiter stays absent/stale longer than operators expect. |
| **RGW impact** | Indirect: the resilience layer is degraded for longer, widening the window where a second failure causes **S3 outage**. |
| **Severity** | **Low–Medium.** |
| **Simulation** | Block the remote API server intermittently (flap a NetworkPolicy), measure time-to-recovery of the arbiter vs. the configured `CheckInterval`. |

### Scenario 8 — Stretch / external-mode CephCluster silently ignores the arbiter

| | |
|---|---|
| **Trigger** | The CephCluster is configured in stretch mode (common for 2-AZ!) or external mode. Rook's `reconcileExternalMons` only honours `ExternalMonIDs` when **not** stretch and **not** external (`health.go:438`). |
| **Mechanism** | The operator still deploys the external mon and reserves the ID, but Rook ignores it — a dangerous architecture/config mismatch where operators *believe* they have 3-mon resilience but Rook never integrates it. |
| **RGW impact** | **Critical and silent:** during an AZ failure the expected third mon provides no quorum benefit → **S3 outage** despite a green-looking arbiter. |
| **Severity** | **Critical.** |
| **Simulation** | Deploy the arbiter against a stretch-mode CephCluster, confirm `RemoteArbiter` reports Ready and the mon runs, but `ceph mon dump` / Rook never counts it toward quorum protection. This is primarily a *validation/alerting* gap to flag. |

### Scenario 9 — Messenger port reachability (3300 / 6789) via NodePort pinning

| | |
|---|---|
| **Trigger** | NodePort service type pins node ports equal to the Ceph messenger ports 3300/6789 (`arbiterServicePorts`, `remotearbiter_controller.go:1085`). If the target cluster's kube-apiserver node-port range doesn't include those ports, or a firewall blocks them, the mon is unreachable. |
| **Mechanism** | Service creation fails or the port is filtered; external mon unreachable → same effect as Scenario 1. |
| **RGW impact** | External mon in monmap but unreachable → degraded quorum arithmetic → **S3 risk under partition**. |
| **Severity** | **High.** |
| **Simulation** | Block TCP 3300/6789 to the arbiter node/service with a firewall/NetworkPolicy, verify `ceph ping mon.ext-a` fails from a mon pod, run S3 traffic through a simulated AZ partition. |

### Scenario 10 — RGW's own mon-endpoint reconnection under config rewrite

| | |
|---|---|
| **Trigger** | Any operator action that triggers `saveMonConfig()` (reserve/release ID, mon address change). |
| **Mechanism** | RGW re-reads mon endpoints; depending on Ceph/RGW version, this can cause a brief reconnect. |
| **RGW impact** | Short connection resets → sporadic **5xx / retriable errors** on in-flight S3 ops. Clients with retry survive; naive clients see failures. |
| **Severity** | **Low–Medium.** |
| **Simulation** | Add then remove an external mon ID while running high-concurrency S3 traffic; measure error/retry rate correlated with `rook-ceph-config` resourceVersion changes. |

---

## Severity Summary

| # | Scenario | RGW severity | Detection today |
|---|----------|--------------|-----------------|
| 1 | Unreachable `--public-addr` | Critical | Weak (looks healthy) |
| 8 | Stretch/external mode ignores arbiter | Critical | None |
| 2 | `ExternalMonIDs` removed with live mon | High | Weak |
| 3 | Arbiter restart storm | High | Partial (restart count) |
| 9 | Messenger port blocked (NodePort) | High | Weak |
| 5 | Stale `mon_host` in monmap | Med–High | None |
| 4 | Cross-cluster partition (+combined) | Med→Crit | Partial |
| 6 | Readiness gating during degradation | Medium | None |
| 10 | RGW reconnect under config churn | Low–Med | None |
| 7 | Unbounded backoff | Low–Med | None |

---

## Simulation Harness (implemented)

> **Status:** implemented under `test/e2e/quorum/` as the RGW-chaos extension of the
> quorum-survival harness. Entry point: `test/e2e/quorum/chaos.sh`. The scenario → script
> mapping below is live, not aspirational.

Rather than introduce a parallel harness, these simulations **plug into the existing
`test/e2e/quorum/` scaffold** from the `docs/quorum-survival-harness` branch. That harness
already brings up k3d + Rook + the operator + a voting arbiter and proves baseline quorum. We add
an RGW-traffic layer and extra fault steps, reusing its `lib.sh` (`ceph_exec`, `quorum_json`,
`toolbox_pod`), its numbered-step pattern, and its `NEGATIVE_CONTROL` / victim-file idioms.

### What was added to the existing harness

1. **RGW setup step** (`25-objectstore.sh` + `manifests/object-store.yaml`): applies a
   `CephObjectStore` + `CephObjectStoreUser` (pools `size: 1`,
   `requireSafeReplicaSize: false` for the laptop-scale OSD floor), waits for RGW `Ready`, the
   gateway deployment to have a ready replica, and the credentials Secret to appear.
2. **Traffic oracle** (`lib.sh` helpers `start_s3_traffic` / `s3_error_count` /
   `assert_s3_delta` / `hold_with_traffic` / `stop_s3_traffic`): an **in-cluster probe pod**
   (`amazon/aws-cli`) loops PUT→GET→LIST against the RGW ClusterIP DNS name, appending one
   `OK` / `ERR <op>` line per iteration to `/tmp/s3-probe.log`. The error delta over any window
   is `s3_error_count` after − before. An in-cluster pod is required because the RGW service is
   **ClusterIP-only** (not reachable from the host on k3d); `contrib/tools/arbiter-s3-bench`
   remains the Lima-VM equivalent for local dev.
3. **Fault-injection steps** (one script per scenario, mirroring `70-kill-mon.sh`), each with a
   snapshot-and-restore `EXIT`/`INT`/`TERM` trap so state is always restored:

| Step | Scenario | Injection | RGW pass criterion |
|----------|----------|-----------|--------------------|
| `71-bad-public-addr.sh` | S1 | NetworkPolicy makes the arbiter's advertised address unreachable, then kill one source mon | **errors expected** — proves the advertised address is load-bearing (unreachable arbiter → quorum lost on single mon kill) |
| `72-teardown-race.sh` | S2 | `delete remotearbiter` under load; observe `externalMonIDs` vs `rook-ceph-config` churn | **zero** S3 errors; config churn events reported |
| `73-restart-storm.sh` | S3 | repeatedly annotate `rook-config-override` to force operator drift updates | **zero** S3 errors; arbiter ReplicaSet/restart churn reported, arbiter still voting |
| `74-partition-arbiter.sh` | S4/S9 | NetworkPolicy drop msgr 3300/6789 to arbiter (optional 2nd-mon kill via `NEGATIVE_CONTROL`) | single-link loss: **zero** S3 errors; `NEGATIVE_CONTROL=1` + mon kill: errors expected |
| `75-stale-mon-host.sh` | S5 | delete a non-leader source mon Deployment → Rook failover changes `mon_host` | **zero** S3 errors; arbiter env-Secret convergence reported |
| `76-config-churn.sh` | S10 | add/remove a throwaway `externalMonIDs` entry under load | **zero** S3 errors; `rook-ceph-config` rewrite count reported |
| `77-stretch-detect.sh` | S8 | validation: assert non-stretch + flag the missing stretch-mode guard | known-gap marker; passes once the operator emits a guard (`STRETCH_GUARD_EXPECTED=1`) |

   Scenarios S6 (degraded-no-recover) and S7 (backoff-recovery) are primarily operator-behaviour
   timing tests and are left as optional future steps.

The orchestrator `chaos.sh` runs the data-plane flow: optional `FULL=1` bring-up (`00`–`60`),
always `25-objectstore.sh`, then the scenarios (all by default, or named ones as args).

### Shared impact-measurement pattern (per fault step)

Mirrors the existing `60`/`70`/`80` flow, with traffic running throughout:

1. `start_s3_traffic` — launch the in-cluster probe pod; block until it is Running and has
   logged ≥1 result.
2. Baseline: `base_err="$(s3_error_count)"` and a `quorum_json` snapshot.
3. Inject fault (record prior state to a snapshot/victim file; install `restore_all` trap on
   `EXIT INT TERM`).
4. `hold_with_traffic <secs>` — sample `quorum_json` while the probe keeps running.
5. Restore via trap (delete NetworkPolicy, re-add `externalMonIDs`, scale mon back up,
   re-apply arbiter, etc.).
6. `assert_s3_delta "$base_err" zero|nonzero` — the pass/fail gate; auxiliary state
   (config churn count, restart count, `mon_host` convergence) is reported for visibility.
   **For designed-resilience faults (S2, S3, S4 single-link, S5, S10): the criterion is
   `zero`. For S1 (unreachable address): `nonzero` — the fault must bite.**

### Example step (abridged `74-partition-arbiter.sh`)

```bash
#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

restore_all() { kubectl -n "${TARGET_NS}" delete netpol arbiter-chaos-partition --ignore-not-found; stop_s3_traffic; }
trap restore_all EXIT INT TERM

start_s3_traffic
base_err="$(s3_error_count)"
kubectl apply -f - <<YAML      # drop msgr 3300/6789 to the arbiter pod
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: { name: arbiter-chaos-partition, namespace: ${TARGET_NS} }
spec:
  podSelector: { matchLabels: { ceph.cobaltcore.sap.com/lookup: external-arbiter } }
  policyTypes: [Ingress, Egress]
  ingress: []
  egress: []
YAML
hold_with_traffic "${HOLD:-60}"

N=$(quorum_json | jq '.quorum_names | length')
[ "$N" -ge 3 ] || die "expected majority (>=3) with only arbiter lost, got $N"
assert_s3_delta "$base_err" zero       # designed: RGW uninterrupted
```

### Unit-testing the helpers

The pure helpers (error-delta math in `assert_s3_delta`, the counters) are factored so they can
be covered with **bats** using the existing mock pattern in
`contrib/tools/tests/helpers/mock-commands` (`kubectl`, `aws`), consistent with the current
`arbiter-s3-bench.bats` / `arbiter-health.bats` suites.

---

## Recommended Follow-ups (beyond simulation)

These are gaps surfaced by the analysis that the simulations will confirm:

1. **Reachability validation of `--public-addr`** before/after join (addresses S1, S9): the
   operator should verify the advertised mon address is reachable from the Ceph cluster, and set
   a condition `False` if not.
2. **Stretch/external-mode guard** (addresses S8): the webhook or controller should reject /
   warn when the target CephCluster is stretch/external, since Rook ignores `externalMonIDs`.
3. **Safer teardown ordering** (addresses S2): remove the mon from the live monmap / confirm
   remote cleanup succeeded *before* dropping the ID from `ExternalMonIDs`; do not silently skip
   remote cleanup.
4. **Bounded, predictable backoff** (addresses S7): wire a `RateLimiter` with a sane cap so
   arbiter recovery tracks `CheckInterval`.
5. **Alerting on "in monmap but not in quorum"** (addresses S1/S3/S5): `arbiter-health` already
   detects this state; expose it as a metric/alert.

---

## References

- Quorum-survival harness design: `docs/superpowers/specs/2026-09-03-quorum-survival-harness-design.md`
  (branch `docs/quorum-survival-harness`, issues #70/#71).
- Harness scaffold this doc extends: `test/e2e/quorum/` (`run.sh`, `lib.sh`, `60-assert-baseline.sh`,
  `70-kill-mon.sh`, `80-assert-survival.sh`).
- Traffic oracle: `contrib/tools/arbiter-s3-bench`; health/quorum view: `contrib/tools/arbiter-health`.
- Core operator logic: `pkg/controller/remotearbiter_controller.go`; Rook external-mon handling:
  `rook/pkg/operator/ceph/cluster/mon/health.go`.
