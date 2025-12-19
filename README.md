<!--
# SPDX-FileCopyrightText: Copyright 2024 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
#
# SPDX-License-Identifier: Apache-2.0
-->

# external-arbiter-operator

## About this project

external-arbiter-operator works with [rook](https://rook.io/)-provisioned ceph clusters and deploys 
external, not managed by rook, arbiter (monitor), that participates in consensus.

Operator also monitors remote cluster and checks whether cluster is available and tenant has enough
permissions to handle arbiter deployment.

## Requirements and Setup

### Required tools

Following tools should be available on development machine
- sed
- openssl
- make
- git
- golang
- [lima](https://lima-vm.io/), or other way to provision k8s locally, like minikube
- kubectl
- docker, or any other compatible container engine, like podman
- helm

The rest is provisioned via `go tool`, including kubebuilder toolset.

### Quick start

A quick walkthrough on how to prepare environment, run operator locally and deploy external monitor.

```bash
# clone rook repo if not yet done
make deps
# create osd for ceph
limactl disk create osd --size=8G
# create vm instance
limactl create --name=k8s ./contrib/vm.yaml
# start vm
limactl start k8s
# use kubeconfig provided by vm
export KUBECONFIG="${HOME}/.lima/k8s/copied-from-guest/kubeconfig.yaml"
# install cert manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.19.2/cert-manager.yaml
# install rook operator
kubectl apply -f ./rook/deploy/examples/crds.yaml
kubectl apply -f ./rook/deploy/examples/common.yaml
kubectl apply -f ./rook/deploy/examples/operator.yaml
kubectl apply -f ./rook/deploy/examples/csi-operator.yaml
# create ceph cluster 
kubectl apply -f ./rook/deploy/examples/cluster-test.yaml
# (optional) install ceph toolbox
kubectl apply -f ./rook/deploy/examples/toolbox.yaml
# build image 
limactl shell k8s sudo nerdctl --namespace k8s.io build -t localhost:5000/cobaltcore-dev/external-arbiter-operator:latest -f ./Dockerfile .
# dry run operator install via helm
helm install --dry-run --create-namespace --namespace arbiter-operator --values ./contrib/charts/external-arbiter-operator/local.yaml arbiter-operator ./contrib/charts/external-arbiter-operator 
# install operator via helm chart
helm install --create-namespace --namespace arbiter-operator --values ./contrib/charts/external-arbiter-operator/local.yaml arbiter-operator ./contrib/charts/external-arbiter-operator
# create namespace, user, role, rolebinding, kubeconfig and secret for arbiter
./hack/configure-k8s-user.sh
# create secret with remote cluster access configuration produced on previous step
kubectl apply -f ./contrib/k8s/examples/secret.yaml -n arbiter-operator
# create remote cluster
kubectl apply -f ./contrib/k8s/examples/remote-cluster.yaml -n arbiter-operator
# create remote arbiter
kubectl apply -f ./contrib/k8s/examples/remote-arbiter.yaml -n arbiter-operator
# watch until arbiter ready
kubectl get remotearbiter -n arbiter-operator -w
# check arbiter joined quorum
kubectl exec deployment/rook-ceph-tools -n rook-ceph -it -- ceph mon dump
# stop vm
limactl stop k8s
# delete vm
limactl delete k8s
```

### Make goals

Useful `make` commands 

```bash
# build binary 
make
# prettify project, run linters, etc.
make pretty
# run tests
make test
# regenerate k8s resources
make gen 
# copy CRD definitions to helm chart
make helm
```

### How to configure deployment

Deplmoyment manifests are managed by helm.
[values.yaml](./contrib/charts/external-arbiter-operator/values.yaml) lists all possible configuration options.

### How to configure resources

Following examples are provided:
- [secret.yaml](./contrib/k8s/examples/secret.yaml) for arbiter installation kubeconfig secret
- [remote-cluster.yaml](./contrib/k8s/examples/remote-cluster.yaml) for RemoteCluster resource
- [remote-arbiter.yaml](./contrib/k8s/examples/remote-arbiter.yaml) for RemoteArbiter resource

### How to run

We assume that 
- Ceph cluster operated by rook is already up and running on source k8s cluster
- Resources (pods, services) from target (arbiter) cluster are reachable on source (operator/rook) cluster and vice versa

1. Create user on target cluster
2. Create target namespace on target cluster
3. Grant user permissions to manage deployments, secrets, configmaps, their statuses and finalizers
4. Provision target user kubeconfig on source cluster via secret
5. Deploy operator on source cluster
6. Create `RemoteCluster` resource on source cluster, referring target user kubeconfig secret
7. Create `RemoteArbiter` resource on source cluster, referring `RemoteCluster`
8. Watch until resources are ready
9. Check that arbiter has joined quorum by dumping mon map with `ceph mon dump`

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/cobaltcore-dev/external-arbiter-operator/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure
If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/cobaltcore-dev/external-arbiter-operator/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/SAP/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright (2025-)2026 SAP SE or an SAP affiliate company and cobaltcore-dev contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/cobaltcore-dev/external-arbiter-operator).
