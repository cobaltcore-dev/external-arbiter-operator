<!--
Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
SPDX-License-Identifier: Apache-2.0
-->

# CI: required checks and branch protection

This repository gates every pull request on a fast CI baseline (see
`.github/workflows/ci.yaml`). Expensive checks run on a schedule
(`.github/workflows/nightly.yaml`) and are **not** required for merge.

## Required status checks

Configure branch protection on `main` to require these checks (the names are
the job `name:` values, which is what GitHub surfaces as the check name):

| Check (job name)                     | What it guards                                              |
| ------------------------------------ | ----------------------------------------------------------- |
| `generated files up to date`         | `make gen` is a no-op — committed CRDs/RBAC/deepcopy match source. |
| `fmt / vet / lint / unit / build`    | gofmt, `go vet`, golangci-lint, unit tests (`-race -shuffle=on`), `go build`. |
| `envtest integration`                | Controller reconciliation against a real API server (envtest) using the pinned Rook CRD. |
| `helm lint + template`               | Helm chart lints and renders; CRD templates stay valid.     |
| `govulncheck`                        | No known vulnerabilities in dependencies or the toolchain.  |
| `REUSE Compliance Check`             | License/copyright headers present (from the separate `reuse.yaml` workflow, not `ci.yaml`). |

## What is pinned vs. what reaches the network

CI is **reproducible** (versions are pinned), not fully **hermetic** (some jobs
still fetch over the network on a cache miss). Concretely:

- Pinned and network-free: the **Rook CRD** is vendored at
  `contrib/k8s/3rdparty/rook.yaml` (Rook `v1.18.6`), so no job clones Rook. All
  Go tooling is invoked through `go tool` (declared in `go.mod`) and resolved by
  `actions/setup-go`; the Go version comes from `go.mod` (`go-version-file`) and
  the Kubernetes/envtest version from the `Makefile` (`K8S_VERSION`).
- Still reaches the network: the **envtest** job runs `setup-envtest use`, which
  downloads the kube-apiserver/etcd binaries from a Google-hosted bucket on a
  cache miss; **govulncheck** queries `vuln.go.dev` for the vulnerability
  database. These are external dependencies, not repository state.

## Branch-protection settings

On `main`, enable:

- **Require a pull request before merging** — with at least one approving review
  (see #77 for the independent-review requirement).
- **Require status checks to pass before merging** — select all six checks above.
- **Require branches to be up to date before merging** — so checks run against the
  post-merge tree.
- **Do not allow bypassing the above settings** — applies the rules to admins too.

## Performance target

Fast CI is expected to complete within **15 minutes** (each job carries a
`timeout-minutes: 15`). The determinism repeat (20×) is deliberately deferred to
the nightly workflow to keep the required gate fast.

## Updating the vendored Rook CRD

`make deps` still fetches `deploy/examples/crds.yaml` from the pinned `ROOK_VERSION`
into `contrib/k8s/3rdparty/rook.yaml`. After bumping `ROOK_VERSION`, run `make deps`
and commit the regenerated file. The file is verified byte-for-byte against the
upstream tag before committing.
