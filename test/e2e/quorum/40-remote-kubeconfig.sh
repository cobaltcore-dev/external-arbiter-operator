#!/usr/bin/env bash
# 40 — create the target namespace + arbiter-installer RBAC, mint an SA token,
# render a kubeconfig pointing at the in-cluster apiserver, and store it as the
# accesskeyRef Secret the RemoteCluster reads (in OP_NS, per makeRemoteClient).
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

kubectl create namespace "${TARGET_NS}" --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace "${OP_NS}" --dry-run=client -o yaml | kubectl apply -f -

log "applying arbiter-installer RBAC in ${TARGET_NS}"
kubectl apply -f "${MANIFESTS}/arbiter-installer-rbac.yaml"

log "minting SA token + building kubeconfig"
TOKEN=$(kubectl -n "${TARGET_NS}" create token arbiter-installer --duration=24h)
CA=$(kubectl config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
[ -n "${CA}" ] || die "could not read cluster CA (embedded ca cert required)"

KCFG="$(mktemp)"
trap 'rm -f "${KCFG}"' EXIT
cat > "${KCFG}" <<EOF
apiVersion: v1
kind: Config
clusters:
- name: e2e
  cluster:
    server: https://kubernetes.default.svc:443
    certificate-authority-data: ${CA}
contexts:
- name: e2e
  context:
    cluster: e2e
    user: arbiter-installer
    namespace: ${TARGET_NS}
current-context: e2e
users:
- name: arbiter-installer
  user:
    token: ${TOKEN}
EOF

log "storing kubeconfig Secret 'external-arbiter' in ${OP_NS}"
kubectl -n "${OP_NS}" create secret generic external-arbiter \
  --from-file=kubeconfig.yaml="${KCFG}" \
  --dry-run=client -o yaml | kubectl apply -f -
log "remote kubeconfig ready"