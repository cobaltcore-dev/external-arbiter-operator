#!/usr/bin/env bash
# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0

set -e

if [ ! -f ./external-arbiter.key ]; then
    openssl genrsa -out external-arbiter.key 2048
fi
if [ ! -f ./external-arbiter.csr ]; then
    openssl req -new -key ./external-arbiter.key -out ./external-arbiter.csr -subj "/CN=external-arbiter/O=cobaltcore-dev"
fi
csr=$(cat ./external-arbiter.csr | base64 | tr -d '\n')
csrResource=$(cat <<-EOF
---
apiVersion: certificates.k8s.io/v1
kind: CertificateSigningRequest
metadata:
  name: external-arbiter-csr
spec:
  request: "${csr}"
  signerName: kubernetes.io/kube-apiserver-client
  usages:
    - client auth
EOF
)
echo "$csrResource" | u8s kubectl apply -f -

u8s kubectl -n external-arbiter certificate approve external-arbiter-csr
u8s kubectl get csr external-arbiter-csr -o jsonpath='{.status.certificate}' | base64 --decode > external-arbiter.crt

namespace=$(cat <<EOF
---
apiVersion: v1
kind: Namespace
metadata:
    name: external-arbiter
    namespace: external-arbiter
EOF
)
echo "$namespace" | u8s kubectl apply -f -

role=$(cat <<EOF
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: external-arbiter-role
  namespace: external-arbiter
rules:
- apiGroups:
  - ""
  resources:
  - services
  - services/status
  - configmaps
  - configmaps/status
  - secrets
  - secrets/status
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - ""
  resources:
  - services/finalizers
  - configmaps/finalizers
  - secrets/finalizers
  verbs:
  - update
- apiGroups:
  - apps
  resources:
  - deployments
  - deployments/status
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - apps
  resources:
  - deployments/finalizers
  verbs:
  - update
EOF
)
echo "$role" | u8s kubectl --namespace=external-arbiter apply -f -

roleBinding=$(cat <<EOF
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: external-arbiter-rolebinding
  namespace: external-arbiter
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: external-arbiter-role
subjects:
  - kind: User
    name: external-arbiter
EOF
)
echo "$roleBinding" | u8s kubectl  --namespace=external-arbiter apply -f -

u8s kubectl get cm kube-root-ca.crt -o jsonpath="{['data']['ca\.crt']}" > k8s-ca.crt
u8s kubectl config set-cluster kubernetes --server=https://st1-qa-de-1-c67f19d507d543a3a9eaa3607729826f.kubernikus-v.qa-de-1.cloud.sap --certificate-authority=k8s-ca.crt --embed-certs=true --kubeconfig="/Users/C5413905/Library/Application Support/SAPCC/u8s/.kube/config"
u8s kubectl config set-credentials external-arbiter --client-certificate=external-arbiter.crt --client-key=external-arbiter.key --embed-certs=true --kubeconfig="/Users/C5413905/Library/Application Support/SAPCC/u8s/.kube/config"
u8s kubectl config set-context external-arbiter@kubernetes --cluster=kubernetes --user=external-arbiter --kubeconfig="/Users/C5413905/Library/Application Support/SAPCC/u8s/.kube/config"
u8s kubectl config use-context external-arbiter@kubernetes --kubeconfig="/Users/C5413905/Library/Application Support/SAPCC/u8s/.kube/config"

kubeconfig=$(cat ./external-arbiter.kubeconfig | base64 | tr -d '\n')
secret=$(cat <<EOF
---
apiVersion: v1
kind: Secret
metadata:
  name: external-arbiter
data:
  # kubeconfig to access remote clustes
  kubeconfig.yaml: ${kubeconfig}
EOF
)
echo "$secret" > ./contrib/k8s/examples/secret.yaml