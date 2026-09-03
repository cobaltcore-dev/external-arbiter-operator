<!--
Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
SPDX-License-Identifier: Apache-2.0
-->

# Test traceability: reconcile transitions → tests

This table maps each reconciler state transition and error branch to the spec
that exercises it. It is the checklist for lifecycle/error-path coverage (#68):
a transition with no covering spec is an untested branch. Function names and
line references are anchors into `pkg/controller/`; spec names are the Ginkgo
`It(...)` descriptions.

Coverage legend: **✓** covered · **partial** happy-path only, error branch
untested · **gap** no spec.

## RemoteCluster reconcile (`remotecluster_controller.go`)

| Transition / branch | Source | Condition / state set | Covered by |
| ------------------- | ------ | --------------------- | ---------- |
| Secret missing | `getSecret` | `SecretAvailable=False`, `Error` | ✓ "Should fail to get secret" |
| Secret present, kubeconfig key absent/invalid | `getSecret` | `ConfigValid=False`, `Error` | partial |
| Remote cluster unreachable | `makeRemoteClient` / `checkClusterReady` | `ClusterReachable=False`, `Error` | partial |
| Insufficient RBAC on remote | `validatePermissions` | `HasEnoughPermissions=False`, `Error` | partial |
| All checks pass | `Reconcile` | `Ready` | ✓ (happy path) |
| Deletion: finalizer present | `cleanUpRemoteCluster` → `cleanUpSecret` | finalizer removed, `Deleting` | gap |
| Deletion: finalizer absent (idempotent) | `cleanUpRemoteCluster` | no-op | gap |
| Secret→cluster mapping enqueue | `findClusterForSecret` | requeue | gap |

## RemoteArbiter reconcile (`remotearbiter_controller.go`)

| Transition / branch | Source | Condition / state set | Covered by |
| ------------------- | ------ | --------------------- | ---------- |
| RemoteCluster (by name) missing | `fetchRemoteCluster` | `RemoteClusterExists=False`, `Error` | ✓ "should fail to find remote cluster" |
| RemoteCluster not Ready | `Reconcile` :158 | `RemoteClusterReady=False`, `Error` | ✓ "should fail to check if remote cluster ready" |
| Inline RemoteCluster spec creation | `createRemoteCluster` | child RC created | gap |
| Remote client construction fails | `makeRemoteClient` :1191 | `RemoteClusterReady=False`, `Error` | partial |
| CephCluster missing | `fetchCephCluster` | `CephClusterExists=False`, `Error` | partial |
| CephCluster not Ready | `Reconcile` | `CephClusterReady=False`, `Error` | partial |
| Monitor-ID reserved (first free suffix) | `reserveExternalArbiterID` :355 | `MonID` set, `CephClusterConfigured` | ✓ `allocateMonID` "first free when none taken" |
| Monitor-ID collision (prefix taken, next suffix) | `reserveExternalArbiterID` | `MonID` = next free | ✓ `allocateMonID` "skips a single/run of collisions" |
| Monitor-ID exhaustion (a–z all taken) | `reserveExternalArbiterID` | error, `Error` | ✓ `allocateMonID` "exhaustion errors" |
| Monitor deployment missing on remote | `fetchMonitorDeployment` :1014 | `MonitorDeploymentExists=False`, `Error` | ✓ "should fail to check if monitor deployment exists" |
| Monitor deployment not Ready | `fetchMonitorDeployment` | `MonitorDeploymentReady=False`, `Error` | ✓ "should fail to check if monitor deployment ready" |
| Public address: ClusterIP / LoadBalancer / NodePort | `determinePublicAddress` :570 | address chosen per Service type | ✓ `determinePublicAddressFor` (all types + unallocated-IP + IPv6-only + unknown-type errors) |
| Arbiter keyring/env/config/deploy/svc created | `createArbiter*` :874–969 | resources exist w/ finalizer | ✓ "should succeed" |
| Arbiter deployment not Ready | `Reconcile` | `ArbiterDeploymentReady=False`, `Error` | ✓ "should fail to check if arbiter deployment ready" |
| Idempotent re-reconcile (no spec change) | `checkArbiterDeploymentUpToDate` :393 | no restart, stable | **gap** |
| Spec change → restart | `restartArbiterDeployment` :759 | rollout | **gap** |
| Deletion: full cascade (reachable remote) | `cleanUpRemoteArbiter` → `cleanUpArbiterDeployment` :1383 | finalizers removed, remote objects deleted, mon-ID released | **gap** |
| Deletion: mon-ID released from CephCluster | `cleanUpCephCluster` :1321 | `ExternalMonIDs` shrinks | **gap** |
| Deletion: unreachable remote (cleanup skipped, no orphan-block) | `cleanUpArbiterDeployment` :1384 | RA finalizer still removed | **gap** |
| Deletion: intermediate state (before mon-ID set) | `cleanUpCephCluster` :1322 early return | no CephCluster mutation | **gap** |

## Webhook validation (`pkg/webhook/v1alpha1/`)

| Branch | Source | Covered by |
| ------ | ------ | ---------- |
| `checkInterval` nil / negative | `validateRemoteArbiterSpec` | partial |
| `cephCluster.name` not a DNS label | same | partial |
| `cephCluster.namespace` not a DNS label (error names correct field) | same :120 | ✓ `TestValidateRemoteArbiterSpecErrorBadValue` (regression guard for the field-value bug fixed in this PR) |
| `monIdPrefix` not a DNS label (error names correct field) | same :128 | ✓ `TestValidateRemoteArbiterSpecErrorBadValue` (regression guard for the field-value bug fixed in this PR) |
| both `remoteCluster.name` and `.spec` empty | same | partial |
| `service.type` empty | same :161 | **gap** |
| NodePort `service.nodeIp` unparseable / non-IPv4 (error names correct field, no double-error) | same :166 | ✓ `TestValidateRemoteArbiterSpecNodeIPErrorBadValue` (regression guard for the field-value + double-append bug fixed in this PR) |

## How to use this table

- Every **gap** / **partial** is a candidate spec. Prioritise safety-critical
  paths: monitor-ID allocation (collision/exhaustion/release) and deletion
  cascade, because a wrong ID or a stranded finalizer corrupts quorum config.
- When adding a spec, update its row's "Covered by" cell in the same PR.
- Deletion-cascade and monitor-ID release rows depend on a reachable remote
  target; they run in the envtest controller suite (two API servers), not the
  pure-unit layer.
