# Quorum-Survival E2E Harness — Design (Issues #70, #71)

**Date:** 2026-09-03
**Parent design:** `2026-09-03-testing-foundation-design.md` (this realises two of
its deferred items)
**Scope decision:** Prove the quorum-survival property with real Ceph, at the
smallest footprint that can honestly prove it. Cross-cluster L3 (#69) is out.

## Goal & the property under test

The operator's reason to exist: deploy a Ceph *arbiter* monitor so a stretched
cluster keeps **mon quorum** when a site or mon is lost. Every current test
(envtest, unit) proves only the Kubernetes plumbing — that the operator emits the
right Deployment/Service/Secret objects. None proves the live property:

> **Kill a source mon and Ceph keeps quorum, *because* the arbiter mon is a
> voting member.**

This harness proves exactly that, end to end, against real `ceph-mon` processes.
It realises the deferred issues **#70 (quorum)** and **#71 (fault injection)**.
It explicitly does **not** attempt **#69 (genuine multi-cluster / cross-cluster
L3)** — see Limitations.

### Why the arbiter forces the topology

The arbiter mon is **not** a separate Ceph cluster. The controller *DeepCopies
the source cluster's existing Rook mon Deployment* (`remotearbiter_controller.go`
`makeDeploymentSpec` → `s.monitorDeployment.Spec.DeepCopy()`, L493), scrapes the
source's `--fsid`,
and builds the arbiter's monmap from the source's `mon_host` /
`mon_initial_members` secret (`getMonMapInitContainer`, `monmaptool --create
... --addv $(ROOK_CEPH_MON_INITIAL_MEMBERS) $(ROOK_CEPH_MON_HOST)`). Same fsid +
same keyring + same monmap ⇒ the arbiter joins the source cluster's **existing
Paxos quorum**. Paxos requires every mon to reach every other mon bidirectionally
on 3300/6789 — which is why the README states "pods/services must be mutually
reachable."

## Topology decision — one k3d cluster, two namespaces

| | One cluster, 2 ns (**chosen**) | Two k3d clusters |
|---|---|---|
| Mon-to-mon routing | Free — one flat pod/service CIDR | Separate CIDRs, **no route** |
| Extra infra to make quorum possible | none | submariner (~a day) / docker-net static routes (brittle) / NodePort forcing every source mon behind a routable addr |
| Proves quorum survival | **yes** | yes, *after* you build the networking |
| Proves cross-cluster L3 | no | yes (that's #69) |
| Footprint | Ceph only | Ceph **+** networking stack |

Two clusters is a legitimate goal, but it proves **networking**, not **quorum** —
and there is no small version of it. One cluster with `rook-ceph` (source) and
`external-arbiter` (target) namespaces removes the networking footprint while
keeping everything the quorum proof needs: real Rook Ceph, a real arbiter
`ceph-mon` joining real quorum, the real `RemoteCluster`/`RemoteArbiter`
reconcile, and a real kill-a-mon.

The "remote" cluster is the **same apiserver**, reached by the operator through a
kubeconfig Secret whose ServiceAccount token is scoped to the target namespace.

## Cluster & resource floor (honest)

```bash
k3d cluster create arbiter-e2e \
  --agents 2 \
  --k3s-arg "--disable=traefik@server:0" \
  --k3s-arg "--disable=metrics-server@server:0" \
  --wait
```

- **3 nodes** (1 server + 2 agents): Rook mon anti-affinity
  (`allowMultiplePerNode: false`) wants each of the 3 source mons on its own node
  so killing one is a real node/mon loss. The arbiter mon has no such Rook
  anti-affinity and co-schedules fine.
- **Resource floor:** Rook operator + 3 mons + 1 mgr + 1–2 OSDs + arbiter mon +
  operator-under-test ≈ **6–8 GB RAM, 3–4 vCPU, ~15 GB disk**. **16 GB RAM is the
  realistic floor**; below ~10 GB the OSD/mgr will OOM or swap. Docker Desktop VM
  must be sized accordingly.
- **Bring-up:** k3d ~30 s; Rook operator ~1 min; **CephCluster to `Ready` with
  OSDs ~5–8 min**. Full cold run ≈ **10–12 min**. This is not "small" — real Ceph
  never is. The one-cluster choice removes the *networking* footprint, not the
  *Ceph* footprint. The only way to make it tiny is to fake Ceph, which then
  proves nothing about quorum.

### OSD without a real disk

k3d nodes have no spare disk, and k3d's built-in `local-path` StorageClass does
**not** support `volumeMode: Block` (its provisioner only `mkdir`s a directory),
so a `storageClassDeviceSets` PVC-backed OSD cannot provision there. Instead a
privileged DaemonSet (`manifests/osd-loopdev-daemonset.yaml`, applied by
`15-osd-loopdev.sh`) creates an 8 Gi sparse file on each node and `losetup`s it
to a raw loop device, which Rook consumes via `storage.useAllNodes: true` +
`useAllDevices: true`. One OSD per node reaches `HEALTH_OK`.

## Rook install

Pin **Rook v1.18.6** — matches `ROOK_VERSION` in the repo Makefile
(verified). This matters: the operator DeepCopies Rook's mon Deployment and
rewrites its args by prefix, so it is sensitive to the exact arg shape a given
Rook version emits. Test against the version the repo targets.

```bash
ROOK=v1.18.6
for f in crds common operator toolbox; do
  kubectl apply -f https://raw.githubusercontent.com/rook/rook/${ROOK}/deploy/examples/${f}.yaml
done
kubectl -n rook-ceph rollout status deploy/rook-ceph-operator --timeout=180s
```

CephCluster (`manifests/source-cephcluster.yaml`), `mon.count: 3`:

```yaml
apiVersion: ceph.rook.io/v1
kind: CephCluster
metadata: { name: my-cluster, namespace: rook-ceph }
spec:
  cephVersion: { image: quay.io/ceph/ceph:v18 }   # reef, pairs with Rook 1.18
  dataDirHostPath: /var/lib/rook
  mon: { count: 3, allowMultiplePerNode: false }
  mgr: { count: 1 }
  dashboard: { enabled: false }
  storage:
    storageClassDeviceSets:
      - name: set1
        count: 2
        portable: false
        volumeClaimTemplates:
          - metadata: { name: data }
            spec:
              resources: { requests: { storage: 5Gi } }
              storageClassName: local-path
              volumeMode: Block
              accessModes: [ ReadWriteOnce ]
  resources:
    mon: { requests: { cpu: "100m", memory: "512Mi" }, limits: { memory: "1Gi" } }
    mgr: { requests: { cpu: "100m", memory: "512Mi" }, limits: { memory: "1Gi" } }
    osd: { requests: { cpu: "100m", memory: "1Gi"   }, limits: { memory: "2Gi" } }
```

Wait: `kubectl -n rook-ceph wait cephcluster/my-cluster
--for=jsonpath='{.status.phase}'=Ready --timeout=600s`.

**Why `mon.count: 3` and kill one** (not `count: 2`): with 3 source mons +
arbiter = **4 mons**, killing one source mon leaves **3/4 = majority**, quorum
retained, and the arbiter is unambiguously one of the surviving voters.
`count: 2` is a degenerate even-sized Ceph config Rook warns about and models the
tie-breaker story poorly.

## Remote kubeconfig (operator → target namespace)

The operator reads the kubeconfig Secret from **the RemoteCluster's own
namespace** — verified: `makeRemoteClient` sets
`Namespace: s.remoteCluster.Namespace` for the Secret lookup
(`remotearbiter_controller.go` ~L1225–1233). The implemented harness places the
operator, the `RemoteCluster`/`RemoteArbiter` CRs, and the Secret all in
`arbiter-operator` (matching the chart/README convention), so the Secret is read
from `arbiter-operator`, while its *token* grants access to the
`external-arbiter` namespace. (The snippets below predate that decision and show
`rook-ceph`; the shipped `manifests/` use `arbiter-operator`.)

Create an SA in `external-arbiter` bound to a Role that is the arbiter-installer
RBAC **reused verbatim from `pkg/controller/suite_test.go` (~L232–270)** — that
rule set is already proven sufficient by the envtest suite. Then:

```bash
kubectl -n external-arbiter create token arbiter-installer --duration=24h > /tmp/sa.token
CA=$(kubectl config view --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
# render kubeconfig.yaml: server https://kubernetes.default.svc:443, token, ca=$CA
kubectl -n rook-ceph create secret generic external-arbiter \
  --from-file=kubeconfig.yaml=/tmp/kubeconfig.yaml
```

- Server is `https://kubernetes.default.svc:443` because the operator runs
  in-cluster.
- Secret key defaults to `kubeconfig.yaml` (`+default="kubeconfig.yaml"` on
  `KubeconfigSecretSource.Key`, verified).
- The operator's *own* client (not this remote one) reads the source mon
  Deployment + keyring in `rook-ceph` via its normal `manager-role`
  (`contrib/k8s/rbac/role.yaml`), applied when the operator is deployed.

## CRs (`manifests/remote-*.yaml`)

```yaml
apiVersion: ceph.cobaltcore.sap.com/v1alpha1
kind: RemoteCluster
metadata: { name: external-arbiter, namespace: rook-ceph }
spec:
  namespace: external-arbiter        # target ns for the arbiter
  accesskeyRef: { name: external-arbiter, key: kubeconfig.yaml }
  checkInterval: 1m
  timeout: 10s
---
apiVersion: ceph.cobaltcore.sap.com/v1alpha1
kind: RemoteArbiter
metadata: { name: external-arbiter, namespace: rook-ceph }
spec:
  remoteCluster: { name: external-arbiter }
  cephCluster: { name: my-cluster, namespace: rook-ceph }
  monIdPrefix: "ext-"
  service: { type: ClusterIP }       # SEE NOTE — set explicitly, do not omit
  checkInterval: 1m
```

**Service type — set `service: {type: ClusterIP}` explicitly.** Verified nuance:
`RemoteArbiterSpec.Service` is a **pointer with no field-level default**, so
*omitting* `service:` leaves it `nil`, and `determinePublicAddressFor` takes the
`service == nil` branch → `--public-addr=$(ROOK_POD_IP)`. The pod IP is on the
flat network and *is* routable, so quorum would still form — but to actually
exercise the Service/ClusterIP address path (`determinePublicAddressFor` returns
`service.Spec.ClusterIP`, verified ~L598–602) the CR must include
`service: {type: ClusterIP}`. ClusterIP is the right choice here: it is
allocated automatically and routable on the shared network. NodePort would
require a hand-picked, webhook-validated IPv4 `nodeIp`
(`remotearbiter_webhook.go` L166–173); LoadBalancer needs a klipper-lb ingress IP
k3d won't hand out cleanly. Both are left to a future topology-A harness.

Wait for the arbiter: `kubectl -n rook-ceph wait remotearbiter/external-arbiter
--for=jsonpath='{.status.state}'=Ready --timeout=300s` and confirm the arbiter
mon Deployment exists in `external-arbiter`.

## Fault injection & assertion

Toolbox (installed above) drives Ceph:

```bash
TOOLS=$(kubectl -n rook-ceph get pod -l app=rook-ceph-tools -o name | head -1)
```

**Predicate 1 — baseline (arbiter votes).** Before any kill, the arbiter must
already be *in* quorum — this is the real proof it is a voting member, not just a
scheduled pod:

```bash
Q=$(kubectl -n rook-ceph exec $TOOLS -- ceph quorum_status --format json)
echo "$Q" | jq -e '.quorum_names | length == 4' >/dev/null
echo "$Q" | jq -e '[.quorum_names[] | select(startswith("ext-"))] | length >= 1' >/dev/null
```

**Fault injection — kill one source mon** (scale its Rook mon Deployment to 0; a
scaled-down mon gives a more stable window than a delete that Rook races to
recreate):

```bash
VICTIM=$(kubectl -n rook-ceph get deploy -l app=rook-ceph-mon -o name | head -1)
kubectl -n rook-ceph scale $VICTIM --replicas=0
sleep 20   # let the mon election settle
```

**Predicate 2 — the core assertion (quorum survives, arbiter still voting):**

```bash
Q=$(kubectl -n rook-ceph exec $TOOLS -- ceph quorum_status --format json)
NUM=$(echo "$Q" | jq '.quorum_names | length')
ARB=$(echo "$Q" | jq '[.quorum_names[] | select(startswith("ext-"))] | length')
kubectl -n rook-ceph exec $TOOLS -- ceph -s | grep -qiE 'HEALTH_ERR|no quorum' && { echo FAIL; exit 1; }
test "$NUM" -ge 3 && test "$ARB" -ge 1 && echo "PASS: quorum survived, arbiter voting" || { echo FAIL; exit 1; }
```

**Negative control (`NEGATIVE_CONTROL=1`)** — kill a *second* source
mon → 2/4, no majority → quorum lost. This proves Predicate 2 isn't vacuous
(quorum *can* be lost, and the arbiter's vote is what prevents it at one loss).
The positive run alone shows the arbiter is *present* in a surviving quorum; only
the positive+negative **pair** isolates the arbiter's vote as load-bearing. Run
both — the positive run is a smoke test on its own.

## In-repo layout & entry point

```
test/e2e/quorum/
  run.sh                    # set -euo pipefail; trap '99-teardown.sh' EXIT; runs 00..80
  00-k3d-up.sh              10-rook-install.sh     20-cephcluster.sh
  30-operator-deploy.sh     40-remote-kubeconfig.sh 50-apply-arbiter.sh
  60-assert-baseline.sh     70-kill-mon.sh          80-assert-survival.sh
  99-teardown.sh            # k3d cluster delete arbiter-e2e
  manifests/
    source-cephcluster.yaml
    arbiter-installer-rbac.yaml   # copied verbatim from suite_test.go
    remote-cluster.yaml  remote-arbiter.yaml
```

Root Makefile:

```makefile
.PHONY: test-e2e-quorum
test-e2e-quorum:            ## Real Rook Ceph quorum-survival e2e (needs docker+k3d, ~16GB RAM, ~12min)
	ROOK_VERSION=$(ROOK_VERSION) bash test/e2e/quorum/run.sh

.PHONY: test-e2e-quorum-teardown
test-e2e-quorum-teardown:
	bash test/e2e/quorum/99-teardown.sh
```

Kept out of the fast envtest/unit lane. In CI it runs only behind an explicit
label / manual dispatch, never on every PR — it's a 12-minute, 16 GB job.

## Limitations — what this proves and what it does NOT

**Proves:**
- The arbiter is a real `ceph-mon` in the source cluster's Paxos quorum (same
  fsid/keyring/monmap) — shown live by `ext-*` in `quorum_names`.
- Quorum survives one source-mon loss **because** the arbiter votes (3/4 holds;
  negative control shows 2/4 fails).
- The full operator reconcile: CRs → `RESTConfigFromKubeConfig` → arbiter mon
  Deployment DeepCopied from the source mon → ClusterIP `--public-addr` wiring.
- The arbiter-installer RBAC surface (reused from `suite_test.go`) is sufficient
  against a real apiserver.

**Does NOT prove:**
- **Cross-cluster L3 networking (#69).** One flat network; the "remote" apiserver
  is the same apiserver. The README's "mutually reachable across two clusters" is
  *assumed here, not tested*. A real two-DC deployment still needs submariner /
  routing validation — a separate, larger harness.
- **NodePort / LoadBalancer `nodeIp` paths.** Only the ClusterIP branch of
  `determinePublicAddressFor` runs. NodePort/LB stay covered by unit tests + a
  future topology-A harness.
- **Data-path resilience.** 1–2 loopback OSDs prove PG health, not real IO or
  recovery under mon loss; no client workload runs during the kill.
- **Production scale/timing.** Laptop-scale mon election, OSD, and failover
  timings differ from real multi-node clusters with real disks.
- **Determinism of the kill window.** Scaling the victim mon to 0 leaves Rook
  free to try re-creating it; the 20 s settle is a heuristic. A flaky run means
  Rook re-elected faster than expected, not that the property failed — re-run or
  widen the window. (A future hardening: cordon Rook's mon-failover by pausing
  the CephCluster reconcile during the assertion.)

## Follow-on

- The two-cluster / cross-cluster-L3 harness is issue **#69**; track it
  separately. The scripts here are structured so a `TOPOLOGY=two-cluster` variant
  could later swap `40-remote-kubeconfig.sh` (real second cluster) +
  `50-apply-arbiter.sh` (NodePort + node IPs) without rewriting the assertions.
