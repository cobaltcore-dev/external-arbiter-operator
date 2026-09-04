<!--
Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
SPDX-License-Identifier: Apache-2.0
-->

# Test traceability: reconcile transitions → tests

This table maps each reconciler state transition and error branch to the spec
that exercises it. It is the checklist for lifecycle/error-path coverage (#68):
a transition with no covering spec is an untested branch. Function names are
anchors into `pkg/controller/`; spec names are the Ginkgo `It(...)`
descriptions.

Coverage legend: **✓** covered · **partial** happy-path only, error branch
untested · **gap** no spec.

## RemoteCluster reconcile (`remotecluster_controller.go`)

| Transition / branch | Source | Condition / state set | Covered by |
| ------------------- | ------ | --------------------- | ---------- |
| Secret missing | `getSecret` | `SecretAvailable=False`, `Error` | ✓ "Should fail to get secret" |
| Secret present, kubeconfig key absent/invalid | `getSecret` | `ConfigValid=False`, `Error` | ✓ "Should fail to find key secret" / "Should fail to parse secret" |
| Remote cluster unreachable | `makeRemoteClient` / `checkClusterReady` | `ClusterReachable=False`, `Error` | ✓ "Should fail to check readiness" |
| Insufficient RBAC on remote | `validatePermissions` | `HasEnoughPermissions=False`, `Error` | ✓ "Should fail to validate permissions" |
| All checks pass | `Reconcile` | `Ready` | ✓ (happy path) |
| Deletion: finalizer present | `cleanUpRemoteCluster` → `cleanUpSecret` | finalizer removed, `Deleting` | gap |
| Deletion: finalizer absent (idempotent) | `cleanUpRemoteCluster` | no-op | gap |
| Secret→cluster mapping enqueue | `findClusterForSecret` | requeue | gap |

## RemoteArbiter reconcile (`remotearbiter_controller.go`)

| Transition / branch | Source | Condition / state set | Covered by |
| ------------------- | ------ | --------------------- | ---------- |
| RemoteCluster (by name) missing | `fetchRemoteCluster` | `RemoteClusterExists=False`, `Error` | ✓ "should fail to find remote cluster" |
| RemoteCluster not Ready | `Reconcile` | `RemoteClusterReady=False`, `Error` | ✓ "should fail to check if remote cluster ready" |
| Inline RemoteCluster spec creation | `createRemoteCluster` | child RC created | gap |
| Remote client construction fails | `makeRemoteClient` | `RemoteClusterReady=False`, `Error` | partial |
| CephCluster missing | `fetchCephCluster` | `CephClusterExists=False`, `Error` | ✓ "should fail to check if ceph cluster exists" |
| CephCluster not Ready | `Reconcile` | `CephClusterReady=False`, `Error` | ✓ (the second "should fail to check if remote cluster ready" spec exercises `CephClusterReady=False`; its `It` name is mislabeled) |
| Monitor-ID reserved (first free suffix) | `reserveExternalArbiterID` | `MonID` set, `CephClusterConfigured` | ✓ `allocateMonID` "first free when none taken" |
| Monitor-ID collision (prefix taken, next suffix) | `reserveExternalArbiterID` | `MonID` = next free | ✓ `allocateMonID` "skips a single/run of collisions" |
| Monitor-ID exhaustion (a–z all taken) | `reserveExternalArbiterID` | error, `Error` | ✓ `allocateMonID` "exhaustion errors" |
| Monitor deployment missing on remote | `fetchMonitorDeployment` | `MonitorDeploymentExists=False`, `Error` | ✓ "should fail to check if monitor deployment exists" |
| Monitor deployment not Ready | `fetchMonitorDeployment` | `MonitorDeploymentReady=False`, `Error` | ✓ "should fail to check if monitor deployment ready" |
| Public address: ClusterIP / LoadBalancer / NodePort | `determinePublicAddress` | address chosen per Service type | ✓ `determinePublicAddressFor` (all types + unallocated-IP + IPv6-only + empty-NodeIP + unknown-type errors) |
| Requested Service type reaches created Service | `createArbiterService` | `Service.Spec.Type` = spec's `service.type` | **gap** (envtest layer; bug fixed in this PR — type was previously never set, silently defaulting NodePort/LB to ClusterIP) |
| Arbiter keyring/env/config/deploy/svc created | `createArbiter*` | resources exist w/ finalizer | ✓ "should succeed" |
| Arbiter deployment not Ready | `Reconcile` | `ArbiterDeploymentReady=False`, `Error` | ✓ "should fail to check if arbiter deployment ready" |
| Idempotent re-reconcile (no spec change) | `checkArbiterDeploymentUpToDate` | no restart, stable | **gap** |
| Spec change → restart | `restartArbiterDeployment` | rollout | **gap** |
| Deletion: full cascade (reachable remote) | `cleanUpRemoteArbiter` → `cleanUpArbiterDeployment` | finalizers removed, remote objects deleted, mon-ID released | **gap** |
| Deletion: mon-ID released from CephCluster | `cleanUpCephCluster` | `ExternalMonIDs` shrinks | **gap** |
| Deletion: unreachable remote (cleanup skipped, no orphan-block) | `cleanUpArbiterDeployment` | RA finalizer still removed | **gap** |
| Deletion: intermediate state (before mon-ID set) | `cleanUpCephCluster` early return | no CephCluster mutation | **gap** |

## Webhook validation (`pkg/webhook/v1alpha1/`)

| Branch | Source | Covered by |
| ------ | ------ | ---------- |
| `checkInterval` nil / negative | `validateRemoteArbiterSpec` | partial |
| `cephCluster.name` not a DNS label | same | partial |
| `cephCluster.namespace` not a DNS label (error names correct field) | same | ✓ `TestValidateRemoteArbiterSpecErrorBadValue` (regression guard for the field-value bug fixed in this PR) |
| `monIdPrefix` not a DNS label (error names correct field) | same | ✓ `TestValidateRemoteArbiterSpecErrorBadValue` (regression guard for the field-value bug fixed in this PR) |
| both `remoteCluster.name` and `.spec` empty | same | partial |
| `service.type` empty | same | **gap** |
| NodePort `service.nodeIp` unparseable / non-IPv4 (error names correct field, no double-error) | same | ✓ `TestValidateRemoteArbiterSpecNodeIPErrorBadValue` (regression guard for the field-value + double-append bug fixed in this PR) |

## How to use this table

- Every **gap** / **partial** is a candidate spec. Prioritise safety-critical
  paths: monitor-ID allocation (collision/exhaustion/release) and deletion
  cascade, because a wrong ID or a stranded finalizer corrupts quorum config.
- When adding a spec, update its row's "Covered by" cell in the same PR.
- Deletion-cascade and monitor-ID release rows depend on a reachable remote
  target; they run in the envtest controller suite (two API servers), not the
  pure-unit layer.
