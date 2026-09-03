# Testing Foundation Design — Issues #66, #67, #68, #76

**Date:** 2026-09-03
**Parent:** #65 (Testing maturity program)
**Scope decision:** Foundation only, verified. No real Ceph. One PR per issue.

## Context

The repo has ~3,000 lines of production Go (2 controllers, 2 webhooks, 2 CRD
types) and ~1,460 lines of envtest-based Ginkgo tests (~32 `It` blocks). There
is **no build/test CI** — only `reuse.yaml` (license) and `stale.yaml`.

Current pain points, verified in-tree:

- `make test` = `pretty env deps` + `go test ./...`. `pretty` **mutates source**
  (fmt, gen, fieldalignment, license) and `deps` **clones the Rook repo over the
  network**. Not hermetic, not safe for CI, not runnable read-only.
- Tests load a manually-captured `contrib/k8s/test/mon-deployment.yaml` fixture
  — the exact anti-pattern #67 calls out.
- One shared envtest suite mixes pure logic and API-server integration; unit
  logic cannot run without a control plane.
- Shared mutable globals (`ctx`, `refMonitor*`) across the whole suite.

## What this program delivers (and what it does NOT)

**In scope now (verifiable on this machine, no Ceph):**

| Issue | Deliverable |
|-------|-------------|
| #67 | Hermetic test layering: `make test-unit` (no API server), `make test-envtest` (explicit assets), `make test` (both). Fixture builders replace the captured manifest. Randomized order, `-count`, `-race`, log capture on failure, leak/cleanup checks, bounded `Eventually`. |
| #66 | `.github/workflows/ci.yaml` — SHA-pinned, least-privilege, concurrency-cancel, timeouts, artifact-on-failure. Jobs: gen-diff, fmt, vet, lint, unit, envtest, race, helm-lint, crd-validate, license (reuse), vuln (govulncheck), build. Documented required-check names. Split fast (PR) vs scheduled (nightly). |
| #68 | Lifecycle/error-path coverage against the real reconcilers + a traceability table mapping each reconcile transition and error branch to a test. Coverage thresholds for critical packages. |
| #76 | Invariant catalogue + Go fuzz tests + property tests for the **pure, oracle-having** functions: mon-ID allocation, defaulting, validation, `Interval` round-trip, status-transition monotonicity. Mutation testing (`gremlins`) on critical packages with a documented, changed-code threshold. |

**Deferred to design+plan docs only (need real infra or org policy — cannot be verified here):**

- #69 multi-cluster Rook/Ceph harness, #70 quorum/workload, #71 fault injection,
  #74 upgrade/rollback — all require kind + Rook + Ceph + CSI running.
- #72 compatibility matrix — depends on the harness existing.
- #73 Renovate/Dependabot gates, #75 release qualification, #77 branch
  protection / CODEOWNERS / independent review — org policy + repo-admin, not
  code we can prove by running `go test`.

Each deferred issue gets a short design note + implementation-plan stub committed
under `docs/superpowers/specs/`, so the work is captured and ordered without
shipping unverifiable scaffolding.

## Architecture: test layering (#67)

```
pkg/**/          production code
  *_unit_test.go   pure logic, package-internal, NO envtest, NO API server
  *_test.go        envtest integration (Ginkgo), tagged //go:build envtest
test/
  builders/        fixture builders (mon deployment, secrets, CRs) — replaces captured YAML
  invariants/      #76 invariant catalogue + property/fuzz targets
```

- **Build tags:** envtest suites gain `//go:build envtest`. `make test-unit`
  runs without the tag (no API server needed, fast). `make test-envtest` runs
  with it after `setup-envtest`. This is the mechanical separation #67 requires.
- **Fixture builders** are Go functions returning typed objects with sane
  defaults + functional options, replacing `os.ReadFile` of captured manifests.
- **Determinism:** `go test -shuffle=on -race -count=1`; Ginkgo
  `--randomize-all`. Bounded `Eventually` already present (30s) — kept, and
  failure output extended to dump object state + controller logs via a
  `DeferCleanup` reporter.
- **Leak checks:** `goleak` in `TestMain` for the unit layer; namespace/
  finalizer cleanup assertions in envtest `AfterEach`.

## CI (#66)

Single `ci.yaml`, `pull_request` + `push` to main, plus a scheduled `nightly.yaml`.

- All third-party actions pinned by commit SHA.
- `permissions: contents: read` at top; escalate per-job only where needed.
- `concurrency: { group: ci-${{ github.ref }}, cancel-in-progress: true }`.
- Per-job `timeout-minutes`; fast lane target ≤15 min.
- Go + K8s/envtest versions read from `go.mod` / Makefile vars — not hardcoded twice.
- `actions/upload-artifact` on failure: JUnit, coverage, envtest logs.
- **gen-diff job**: runs `controller-gen`, fails if the tree is dirty (provenance
  for generated files — also satisfies part of #77).
- The `deps` Rook clone is replaced by vendoring the pinned Rook CRD YAML into
  the repo (no network in CI). Documented in the PR.

Required checks documented in `docs/ci/required-checks.md`.

## Coverage & lifecycle (#68)

Add envtest scenarios for the branches that currently lack direct assertions,
prioritised by safety impact:

- Deletion in each intermediate state; mon-ID release on cleanup; orphan
  prevention when the remote client can't be built.
- Mon-ID collision / exhaustion (prefix+suffix search in `reserveExternalArbiterID`).
- Idempotent re-reconcile; recovery after partial creation; stale resourceVersion conflict.
- Webhook defaulting/validation boundaries for every CRD field.
- Status condition + observed-generation assertions.

Deliverable includes `docs/testing/traceability.md` — a table of (reconcile
step / error branch) → (test name). Coverage threshold enforced in CI for
`pkg/controller` and `pkg/webhook`.

## Invariants, fuzz, property, mutation (#76)

**Invariant catalogue** (`docs/testing/invariants.md`) — each maps to ≥1 test:

| Invariant | Encoded as |
|-----------|-----------|
| Allocated mon-ID is unique within `ExternalMonIDs` and carries the prefix | property test on `reserveExternalArbiterID` extraction |
| Ready is never set unless upstream conditions hold | envtest assertion (#68) |
| Cleanup removes the mon-ID from the Ceph cluster | envtest deletion test |
| Defaulting is idempotent (`Default(Default(x)) == Default(x)`) | property test |
| `Interval` JSON round-trips | fuzz + property |
| Validation rejects negative/empty intervals and non-DNS names | fuzz on `validate*Spec` |
| Status transitions are monotonic (no Ready→Init) | property/model test |

**Fuzz targets** (`go test -fuzz`, bounded on PR, longer nightly; crashers saved
as `testdata/fuzz` regression corpus): CRD validation/defaulting, `Interval`
parsing, mon address handling.

**Mutation testing:** `gremlins` on `pkg/controller` + `pkg/webhook`. Seed
mutations listed in #76 (inverted conditions, skipped errors, removed
finalizers, false Ready). CI runs changed-code mutation; nightly runs full.
Initial threshold 80%, **zero surviving safety-critical mutation**. If the tool
is unstable on Go 1.26, fall back to a documented manual seeded-mutation
checklist so the acceptance criterion is still met.

## Delivery: one PR per issue

Branch + PR per issue, each closing its issue, in dependency order:

1. **#67** hermetic foundation (unblocks everything; PRs 2-4 build on the layout).
2. **#66** CI (needs the make targets from #67 to call).
3. **#68** coverage (needs the envtest layer + builders from #67).
4. **#76** invariants/fuzz/mutation (needs #68 tests as the base suite to mutate).

Then a single **docs PR** with design+plan stubs for #69–75, #77.

Each PR: conventional commits, no AI attribution, branch off main, verified
green locally before push (`make test-unit` always; `make test-envtest` where
assets install).

## Risks / honesty

- `gremlins` may not support Go 1.26 cleanly → documented manual fallback above.
- envtest binaries for K8s 1.34.1 must install in CI; if the exact patch is
  unavailable the workflow pins the nearest supported and records it.
- #68/#76 will likely surface real bugs (e.g. mon-ID exhaustion behaviour). Those
  get filed as issues, not silently patched, unless the fix is trivial and in scope.
