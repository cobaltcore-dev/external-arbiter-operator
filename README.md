<!--
# SPDX-FileCopyrightText: Copyright 2024 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
#
# SPDX-License-Identifier: Apache-2.0
-->

# SAP Repository Template

Default templates for SAP open source repositories, including LICENSE, .reuse/dep5, Code of Conduct, etc... All repositories on github.com/SAP will be created based on this template.

## To-Do

In case you are the maintainer of a new SAP open source project, these are the steps to do with the template files:

- Check if the default license (Apache 2.0) also applies to your project. A license change should only be required in exceptional cases. If this is the case, please change the [license file](LICENSE).
- Enter the correct metadata for the REUSE tool. See our [wiki page](https://wiki.one.int.sap/wiki/display/ospodocs/Using+the+Reuse+Tool+of+FSFE+for+Copyright+and+License+Information) for details how to do it. You can find an initial REUSE.toml file to build on. Please replace the parts inside the single angle quotation marks < > by the specific information for your repository and be sure to run the REUSE tool to validate that the metadata is correct.
- Adjust the contribution guidelines (e.g. add coding style guidelines, pull request checklists, different license if needed etc.)
- Add information about your project to this README (name, description, requirements etc). Especially take care for the <your-project> placeholders - those ones need to be replaced with your project name. See the sections below the horizontal line and [our guidelines on our wiki page](https://wiki.one.int.sap/wiki/pages/viewpage.action?pageId=3564976048#GuidelinesforGitHubHealthfiles(Readme,Contributing,CodeofConduct)-Readme.md) what is required and recommended.
- Remove all content in this README above and including the horizontal line ;)

***

# Our new open source project

## About this project

*Insert a short description of your project here...*

## Requirements and Setup
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
# stop vm
limactl stop k8s
# delete vm
limactl delete k8s
```
## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/cobaltcore-dev/external-arbiter-operator/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure
If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/cobaltcore-dev/external-arbiter-operator/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/SAP/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright (2025-)2026 SAP SE or an SAP affiliate company and cobaltcore-dev contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/cobaltcore-dev/external-arbiter-operator).
