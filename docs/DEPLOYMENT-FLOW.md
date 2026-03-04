# External Arbiter Operator - Deployment Flow

## Complete Deployment Flow Diagram

```
┌─────────────────────────────────────────────────────────────────────────────────────────────┐
│                                   DEPLOYMENT TIMELINE                                        │
└─────────────────────────────────────────────────────────────────────────────────────────────┘

PHASE 1: PREREQUISITES (Manual Setup)
═══════════════════════════════════════════════════════════════════════════════════════════════

SOURCE CLUSTER (Cluster A)                        REMOTE CLUSTER (Cluster B)
─────────────────────────                         ─────────────────────────
        │                                                  │
        │ 1. Rook Already Deployed                        │ 1. Create Namespace
        ▼                                                  ▼
┌────────────────────────┐                        ┌────────────────────────┐
│ Rook Operator          │                        │ Namespace:             │
│ CephCluster: my-cluster│                        │ external-arbiter       │
│ - Mon A (10.0.1.10)    │                        └────────────────────────┘
│ - Mon B (10.0.1.11)    │                                 │
│ - 2 OSDs               │                                 │ 2. Create ServiceAccount
└────────────────────────┘                                 ▼
                                                   ┌────────────────────────┐
                                                   │ ServiceAccount:        │
                                                   │ arbiter-sa             │
                                                   └────────────────────────┘
                                                            │
                                                            │ 3. Create Role
                                                            ▼
                                                   ┌────────────────────────┐
                                                   │ Role: arbiter-role     │
                                                   │ Permissions:           │
                                                   │ - deployments (*)      │
                                                   │ - secrets (*)          │
                                                   │ - configmaps (*)       │
                                                   │ - services (*)         │
                                                   └────────────────────────┘
                                                            │
                                                            │ 4. Create RoleBinding
                                                            ▼
                                                   ┌────────────────────────┐
                                                   │ RoleBinding:           │
                                                   │ arbiter-sa → role      │
                                                   └────────────────────────┘
                                                            │
                                                            │ 5. Generate Kubeconfig
                                                            ▼
                                                   ┌────────────────────────┐
                                                   │ kubeconfig.yaml        │
                                                   │ - API Server URL       │
                                                   │ - SA Token             │
                                                   │ - CA Certificate       │
                                                   └────────────────────────┘
                                                            │
                                     ┌──────────────────────┘
                                     │ Copy to Source Cluster
                                     ▼
        ┌────────────────────────────────────────┐
        │ 6. Create Secret                       │
        │                                        │
        │ kubectl create secret generic          │
        │   external-arbiter \                   │
        │   --from-file=kubeconfig.yaml          │
        └────────────────────────────────────────┘


PHASE 2: OPERATOR INSTALLATION
═══════════════════════════════════════════════════════════════════════════════════════════════

SOURCE CLUSTER (Cluster A)
─────────────────────────
        │
        │ 7. Install Cert-Manager
        ▼
┌────────────────────────┐
│ cert-manager           │
│ - Webhook Certificates │
│ - TLS Management       │
└────────────────────────┘
        │
        │ 8. Deploy Operator via Helm
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ helm install arbiter-operator ./contrib/charts/...               │
└───────────────────────────────────────────────────────────────────┘
        │
        ▼
┌───────────────────────────────────────────────────────────────────┐
│                   Operator Deployment Created                     │
│                                                                   │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │ Namespace: arbiter-operator                                 │ │
│  │                                                             │ │
│  │ Resources Created:                                          │ │
│  │                                                             │ │
│  │ ✓ CustomResourceDefinitions (CRDs)                         │ │
│  │   - RemoteCluster                                          │ │
│  │   - RemoteArbiter                                          │ │
│  │                                                             │ │
│  │ ✓ ServiceAccount                                            │ │
│  │   - manager-sa                                             │ │
│  │                                                             │ │
│  │ ✓ ClusterRole & ClusterRoleBinding                         │ │
│  │   - Access to CephCluster resources                        │ │
│  │   - Access to RemoteCluster/RemoteArbiter CRDs            │ │
│  │                                                             │ │
│  │ ✓ Deployment                                                │ │
│  │   - external-arbiter-operator-manager                      │ │
│  │   - Image: ghcr.io/cobaltcore-dev/external-arbiter-...    │ │
│  │                                                             │ │
│  │ ✓ Webhook Service & Certificate                            │ │
│  │   - Validating Webhook                                     │ │
│  │   - Mutating Webhook                                       │ │
│  │                                                             │ │
│  │ ✓ Metrics Service                                           │ │
│  │   - Prometheus metrics endpoint                            │ │
│  └─────────────────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Operator Pod Starting
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Pod: external-arbiter-operator-manager-xxxxx                      │
│                                                                   │
│ Container: manager                                                │
│ ┌───────────────────────────────────────────────────────────┐   │
│ │ Main Process Started                                      │   │
│ │                                                           │   │
│ │ [INFO] Starting manager                                   │   │
│ │ [INFO] version=v1.0.0 commit=abc123                      │   │
│ │ [INFO] Starting controllers                               │   │
│ │ [INFO] RemoteCluster controller started                   │   │
│ │ [INFO] RemoteArbiter controller started                   │   │
│ │ [INFO] Starting webhooks                                  │   │
│ │ [INFO] Webhook server started on :9443                    │   │
│ │ [INFO] Metrics server started on :8443                    │   │
│ │ [INFO] Health probe server started on :8081               │   │
│ │ [INFO] Leader election enabled                            │   │
│ │ [INFO] Waiting for cache sync...                          │   │
│ │ [INFO] Cache synced, ready to reconcile                   │   │
│ └───────────────────────────────────────────────────────────┘   │
└───────────────────────────────────────────────────────────────────┘
        │
        │ ✓ Operator Ready
        ▼


PHASE 3: REMOTECLUSTER CREATION
═══════════════════════════════════════════════════════════════════════════════════════════════

SOURCE CLUSTER
─────────────
        │
        │ 9. Apply RemoteCluster CR
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ kubectl apply -f remote-cluster.yaml                              │
│                                                                   │
│ apiVersion: ceph.cobaltcore.sap.com/v1alpha1                     │
│ kind: RemoteCluster                                              │
│ metadata:                                                         │
│   name: external-arbiter                                         │
│   namespace: arbiter-operator                                    │
│ spec:                                                             │
│   namespace: external-arbiter                                    │
│   accesskeyRef:                                                   │
│     name: external-arbiter                                       │
│     key: kubeconfig.yaml                                         │
│   checkInterval: 1m                                               │
│   timeout: 10s                                                    │
└───────────────────────────────────────────────────────────────────┘
        │
        │ CR Created
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Operator Watch Event Triggered                                    │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Reconciliation Loop #1
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ RemoteCluster Controller: Reconcile()                             │
│                                                                   │
│ Step 1: Fetch RemoteCluster CR                                   │
│ ├─ [INFO] reconcileID=uuid-1234                                  │
│ └─ ✓ Resource found                                              │
│                                                                   │
│ Step 2: Check deletion timestamp                                 │
│ └─ ✓ Not being deleted                                           │
│                                                                   │
│ Step 3: Add finalizer                                             │
│ ├─ Finalizer: remotecluster.ceph.cobaltcore.sap.com/finalizer   │
│ └─ ✓ Finalizer added                                             │
│                                                                   │
│ Step 4: Initialize status conditions                              │
│ ├─ Condition: SecretAvailable (Unknown)                          │
│ ├─ Condition: ConfigValid (Unknown)                              │
│ ├─ Condition: ClusterReachable (Unknown)                         │
│ ├─ Condition: HasEnoughPermissions (Unknown)                     │
│ └─ ✓ Status initialized, State: Init                             │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Status Update
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ kubectl get remotecluster external-arbiter                        │
│                                                                   │
│ NAME               STATE   MESSAGE                                │
│ external-arbiter   Init    Initializing                          │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Reconciliation Loop #2
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ RemoteCluster Controller: Reconcile()                             │
│                                                                   │
│ Step 5: Fetch Secret                                              │
│ ├─ Looking for: arbiter-operator/external-arbiter               │
│ ├─ Key: kubeconfig.yaml                                          │
│ └─ ✓ Secret found                                                │
│    └─ Update Condition: SecretAvailable = True                   │
│                                                                   │
│ Step 6: Create remote Kubernetes client                          │
│ ├─ Parse kubeconfig from secret                                  │
│ ├─ Extract: API Server, Token, CA Cert                           │
│ ├─ Initialize clientset                                          │
│ └─ ✓ Client created successfully                                 │
│    └─ Update Condition: ConfigValid = True                       │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Test Connection
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Test Remote Cluster Connection                                    │
│                                                                   │
│ API Call: GET /api/v1/namespaces/external-arbiter                │
│           → https://remote-cluster-api:6443                       │
└───────────────────────────────────────────────────────────────────┘
        │
        ▼
REMOTE CLUSTER
─────────────
┌───────────────────────────────────────────────────────────────────┐
│ API Server                                                        │
│ ├─ Authenticate token                                            │
│ ├─ Validate permissions                                          │
│ └─ Return 200 OK with namespace details                          │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Response
        ▼
SOURCE CLUSTER
─────────────
┌───────────────────────────────────────────────────────────────────┐
│ Step 7: Check cluster reachability                                │
│ └─ ✓ Cluster responded successfully                              │
│    └─ Update Condition: ClusterReachable = True                  │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Permission Check
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Step 8: Check permissions via SelfSubjectAccessReview             │
│                                                                   │
│ Check: Can create deployments?                                   │
│ API: POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews │
│ ├─ Resource: deployments                                         │
│ ├─ Verb: create                                                  │
│ ├─ Namespace: external-arbiter                                   │
│ └─ Response: Allowed = true                                      │
│                                                                   │
│ Check: Can create secrets?                                       │
│ └─ Response: Allowed = true                                      │
│                                                                   │
│ Check: Can create configmaps?                                    │
│ └─ Response: Allowed = true                                      │
│                                                                   │
│ Check: Can create services?                                      │
│ └─ Response: Allowed = true                                      │
│                                                                   │
│ ✓ All permission checks passed                                   │
│ └─ Update Condition: HasEnoughPermissions = True                 │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Final Status Update
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Update RemoteCluster Status                                       │
│ ├─ State: Ready                                                  │
│ ├─ Message: "Remote cluster is ready"                            │
│ └─ All conditions: True                                           │
└───────────────────────────────────────────────────────────────────┘
        │
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ kubectl get remotecluster external-arbiter                        │
│                                                                   │
│ NAME               STATE   MESSAGE                                │
│ external-arbiter   Ready   Remote cluster is ready               │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Schedule next reconciliation in 1m (checkInterval)
        ▼


PHASE 4: REMOTEARBITER CREATION
═══════════════════════════════════════════════════════════════════════════════════════════════

SOURCE CLUSTER
─────────────
        │
        │ 10. Apply RemoteArbiter CR
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ kubectl apply -f remote-arbiter.yaml                              │
│                                                                   │
│ apiVersion: ceph.cobaltcore.sap.com/v1alpha1                     │
│ kind: RemoteArbiter                                              │
│ metadata:                                                         │
│   name: external-arbiter                                         │
│   namespace: arbiter-operator                                    │
│ spec:                                                             │
│   remoteCluster:                                                  │
│     name: external-arbiter                                       │
│   cephCluster:                                                    │
│     name: my-cluster                                             │
│     namespace: rook-ceph                                         │
│   monIdPrefix: "ext-"                                             │
│   checkInterval: 1m                                               │
│   service:                                                        │
│     type: ClusterIP                                              │
└───────────────────────────────────────────────────────────────────┘
        │
        │ CR Created
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Operator Watch Event Triggered                                    │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Reconciliation Loop #1
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ RemoteArbiter Controller: Reconcile()                             │
│                                                                   │
│ Step 1: Fetch RemoteArbiter CR                                   │
│ └─ ✓ Resource found                                              │
│                                                                   │
│ Step 2: Add finalizer                                             │
│ └─ ✓ Finalizer added                                             │
│                                                                   │
│ Step 3: Initialize status conditions                              │
│ ├─ Condition: RemoteClusterExists (Unknown)                      │
│ ├─ Condition: RemoteClusterReady (Unknown)                       │
│ ├─ Condition: CephClusterExists (Unknown)                        │
│ ├─ Condition: CephClusterReady (Unknown)                         │
│ ├─ Condition: CephClusterConfigured (Unknown)                    │
│ ├─ Condition: ArbiterDeploymentExists (Unknown)                  │
│ ├─ Condition: ArbiterDeploymentReady (Unknown)                   │
│ └─ ✓ Status initialized, State: Init                             │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Reconciliation Loop #2
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Step 4: Fetch RemoteCluster                                       │
│ ├─ Looking for: arbiter-operator/external-arbiter               │
│ └─ ✓ RemoteCluster found                                         │
│    └─ Update Condition: RemoteClusterExists = True              │
│                                                                   │
│ Step 5: Check RemoteCluster status                                │
│ ├─ RemoteCluster.Status.State = "Ready"                         │
│ └─ ✓ RemoteCluster is ready                                     │
│    └─ Update Condition: RemoteClusterReady = True               │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Fetch Ceph Configuration
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Step 6: Fetch CephCluster                                         │
│ ├─ Looking for: rook-ceph/my-cluster                            │
│ └─ ✓ CephCluster found                                           │
│    └─ Update Condition: CephClusterExists = True                │
│                                                                   │
│ Step 7: Check CephCluster status                                  │
│ ├─ CephCluster.Status.Phase = "Ready"                           │
│ ├─ CephCluster.Status.Ceph.Health = "HEALTH_OK"                 │
│ └─ ✓ CephCluster is ready                                       │
│    └─ Update Condition: CephClusterReady = True                 │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Extract Ceph Configuration
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Step 8: Read Monitor Configuration from Rook                      │
│                                                                   │
│ Fetch: rook-ceph/rook-ceph-mon-a (Deployment)                    │
│ ├─ Extract:                                                      │
│ │  ├─ Image: quay.io/ceph/ceph:v17.2.7                          │
│ │  ├─ Command: [ceph-mon]                                       │
│ │  └─ Args: [--fsid=xxx, --keyring=/etc/ceph/keyring, ...]    │
│ └─ ✓ Deployment config extracted                                │
│                                                                   │
│ Fetch: rook-ceph/rook-ceph-mon-a (Secret)                        │
│ ├─ Extract:                                                      │
│ │  ├─ keyring: [base64-encoded-keyring]                         │
│ │  ├─ fsid: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx                │
│ │  └─ mon-secret: [mon-auth-key]                                │
│ └─ ✓ Monitor secrets extracted                                  │
│                                                                   │
│ Fetch: rook-ceph/rook-ceph-config (ConfigMap)                    │
│ ├─ Extract:                                                      │
│ │  ├─ ceph.conf                                                  │
│ │  ├─ mon_host: 10.0.1.10:3300,10.0.1.11:3300                   │
│ │  └─ mon_initial_members: a,b                                   │
│ └─ ✓ Ceph config extracted                                      │
│                                                                   │
│ ✓ All configuration extracted                                    │
│ └─ Update Condition: CephClusterConfigured = True               │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Generate Arbiter Configuration
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Step 9: Generate Monitor ID                                       │
│ ├─ Prefix: "ext-"                                                │
│ ├─ Existing monitors: [a, b]                                     │
│ ├─ Generate: ext-c                                               │
│ └─ MonID = "ext-c"                                               │
│                                                                   │
│ Step 10: Transform Configuration                                  │
│ ├─ Update ceph.conf:                                             │
│ │  ├─ mon_initial_members = a,b,ext-c                            │
│ │  └─ mon_host = 10.0.1.10:3300,10.0.1.11:3300,                │
│ │                arbiter-service.external-arbiter:3300           │
│ │                                                                │
│ ├─ Create keyring for ext-c                                      │
│ └─ Prepare environment variables                                 │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Deploy to Remote Cluster
        ▼


PHASE 5: ARBITER RESOURCE DEPLOYMENT
═══════════════════════════════════════════════════════════════════════════════════════════════

SOURCE CLUSTER → REMOTE CLUSTER
────────────────────────────────
        │
        │ Remote API Calls (via kubeconfig)
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Step 11: Create Arbiter Keyring Secret                            │
│                                                                   │
│ kubectl apply (via remote client)                                 │
│                                                                   │
│ apiVersion: v1                                                    │
│ kind: Secret                                                      │
│ metadata:                                                         │
│   name: external-arbiter-keyring                                 │
│   namespace: external-arbiter                                    │
│   labels:                                                         │
│     ceph.cobaltcore.sap.com/lookup: external-arbiter            │
│     ceph.cobaltcore.sap.com/role: keyring                        │
│ data:                                                             │
│   keyring: <base64-encoded-ceph-keyring>                         │
│   fsid: <base64-encoded-fsid>                                    │
│                                                                   │
│ ✓ Secret created on remote cluster                               │
└───────────────────────────────────────────────────────────────────┘
        │
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Step 12: Create Arbiter ConfigMap                                 │
│                                                                   │
│ kubectl apply (via remote client)                                 │
│                                                                   │
│ apiVersion: v1                                                    │
│ kind: ConfigMap                                                   │
│ metadata:                                                         │
│   name: external-arbiter-override                                │
│   namespace: external-arbiter                                    │
│   labels:                                                         │
│     ceph.cobaltcore.sap.com/lookup: external-arbiter            │
│ data:                                                             │
│   ceph.conf: |                                                    │
│     [global]                                                      │
│     fsid = xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx                  │
│     mon_host = 10.0.1.10:3300,10.0.1.11:3300,...                │
│     mon_initial_members = a,b,ext-c                               │
│     [mon]                                                         │
│     mon_data_avail_warn = 10                                      │
│     ...                                                           │
│                                                                   │
│ ✓ ConfigMap created on remote cluster                            │
└───────────────────────────────────────────────────────────────────┘
        │
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Step 13: Create Arbiter EnvVar Secret                             │
│                                                                   │
│ kubectl apply (via remote client)                                 │
│                                                                   │
│ apiVersion: v1                                                    │
│ kind: Secret                                                      │
│ metadata:                                                         │
│   name: external-arbiter-envvar                                  │
│   namespace: external-arbiter                                    │
│   labels:                                                         │
│     ceph.cobaltcore.sap.com/role: envvar                         │
│ stringData:                                                       │
│   ROOK_CEPH_MON_HOST: 10.0.1.10:3300,10.0.1.11:3300            │
│   ROOK_CEPH_MON_INITIAL_MEMBERS: a,b,ext-c                       │
│   ROOK_CEPH_CLUSTER_FSID: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxx        │
│                                                                   │
│ ✓ Secret created on remote cluster                               │
└───────────────────────────────────────────────────────────────────┘
        │
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Step 14: Create Arbiter Service                                   │
│                                                                   │
│ kubectl apply (via remote client)                                 │
│                                                                   │
│ apiVersion: v1                                                    │
│ kind: Service                                                     │
│ metadata:                                                         │
│   name: external-arbiter-service                                 │
│   namespace: external-arbiter                                    │
│ spec:                                                             │
│   type: ClusterIP                                                │
│   ports:                                                          │
│   - name: mon                                                     │
│     port: 3300                                                    │
│     targetPort: 3300                                             │
│     protocol: TCP                                                │
│   selector:                                                       │
│     ceph.cobaltcore.sap.com/lookup: external-arbiter            │
│     ceph.cobaltcore.sap.com/role: arbiter                        │
│                                                                   │
│ ✓ Service created on remote cluster                              │
│ ├─ ClusterIP: 10.96.100.50 (assigned by K8s)                    │
│ └─ DNS: external-arbiter-service.external-arbiter.svc           │
└───────────────────────────────────────────────────────────────────┘
        │
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Step 15: Create Arbiter Deployment                                │
│                                                                   │
│ kubectl apply (via remote client)                                 │
│                                                                   │
│ apiVersion: apps/v1                                              │
│ kind: Deployment                                                  │
│ metadata:                                                         │
│   name: external-arbiter                                         │
│   namespace: external-arbiter                                    │
│   labels:                                                         │
│     ceph.cobaltcore.sap.com/lookup: external-arbiter            │
│ spec:                                                             │
│   replicas: 1                                                    │
│   selector:                                                       │
│     matchLabels:                                                  │
│       ceph.cobaltcore.sap.com/lookup: external-arbiter          │
│       ceph.cobaltcore.sap.com/role: arbiter                      │
│   template:                                                       │
│     metadata:                                                     │
│       labels:                                                     │
│         ceph.cobaltcore.sap.com/lookup: external-arbiter        │
│         ceph.cobaltcore.sap.com/role: arbiter                    │
│     spec:                                                         │
│       containers:                                                 │
│       - name: mon                                                │
│         image: quay.io/ceph/ceph:v17.2.7                        │
│         command: ["ceph-mon"]                                    │
│         args:                                                     │
│           - "--fsid=$(ROOK_CEPH_CLUSTER_FSID)"                  │
│           - "--id=ext-c"                                         │
│           - "--mon-data=/var/lib/ceph/mon/ceph-ext-c"           │
│           - "--keyring=/etc/ceph/keyring"                       │
│           - "--public-addr=0.0.0.0:3300"                        │
│           - "--setuser=ceph"                                     │
│           - "--setgroup=ceph"                                    │
│         ports:                                                    │
│         - containerPort: 3300                                    │
│         envFrom:                                                  │
│         - secretRef:                                             │
│             name: external-arbiter-envvar                        │
│         volumeMounts:                                             │
│         - name: keyring                                          │
│           mountPath: /etc/ceph                                   │
│           readOnly: true                                         │
│         - name: config                                           │
│           mountPath: /etc/ceph/ceph.conf                        │
│           subPath: ceph.conf                                     │
│           readOnly: true                                         │
│         - name: monmap                                           │
│           mountPath: /tmp/monmap                                 │
│       volumes:                                                    │
│       - name: keyring                                            │
│         secret:                                                   │
│           secretName: external-arbiter-keyring                   │
│       - name: config                                             │
│         configMap:                                                │
│           name: external-arbiter-override                        │
│       - name: monmap                                             │
│         emptyDir: {}                                             │
│                                                                   │
│ ✓ Deployment created on remote cluster                           │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Update Source Status
        ▼

SOURCE CLUSTER
─────────────
┌───────────────────────────────────────────────────────────────────┐
│ Update RemoteArbiter Status                                       │
│ ├─ Condition: ArbiterDeploymentExists = True                     │
│ ├─ State: Progressing                                            │
│ ├─ Message: "Arbiter deployment created, waiting for ready"      │
│ └─ MonID: ext-c                                                  │
└───────────────────────────────────────────────────────────────────┘


PHASE 6: ARBITER POD STARTUP
═══════════════════════════════════════════════════════════════════════════════════════════════

REMOTE CLUSTER
─────────────
        │
        │ Kubernetes schedules Pod
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Pod: external-arbiter-xxxxx                                       │
│ Status: Pending                                                   │
│                                                                   │
│ Events:                                                           │
│ ├─ Scheduled: Successfully assigned to node-1                    │
│ ├─ Pulling: Pulling image quay.io/ceph/ceph:v17.2.7             │
│ └─ Pulled: Successfully pulled image                             │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Container starting
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Container: mon                                                    │
│ Status: Running                                                   │
│                                                                   │
│ Logs:                                                             │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ 2026-03-04 10:00:01.000 INFO  ceph-mon: starting             │ │
│ │ 2026-03-04 10:00:01.100 INFO  reading config from            │ │
│ │                               /etc/ceph/ceph.conf             │ │
│ │ 2026-03-04 10:00:01.200 INFO  fsid xxxxxxxx-xxxx-xxxx-xxxx   │ │
│ │ 2026-03-04 10:00:01.300 INFO  mon.ext-c using public_addr    │ │
│ │                               0.0.0.0:3300                    │ │
│ │ 2026-03-04 10:00:01.400 INFO  reading keyring from           │ │
│ │                               /etc/ceph/keyring               │ │
│ │ 2026-03-04 10:00:01.500 INFO  initial quorum includes        │ │
│ │                               [a, b, ext-c]                   │ │
│ │ 2026-03-04 10:00:02.000 INFO  probing peer mon.a at           │ │
│ │                               10.0.1.10:3300                  │ │
│ │ 2026-03-04 10:00:02.100 INFO  connected to mon.a              │ │
│ │ 2026-03-04 10:00:02.200 INFO  probing peer mon.b at           │ │
│ │                               10.0.1.11:3300                  │ │
│ │ 2026-03-04 10:00:02.300 INFO  connected to mon.b              │ │
│ │ 2026-03-04 10:00:03.000 INFO  mon.ext-c calling new election │ │
│ │ 2026-03-04 10:00:03.500 INFO  mon.ext-c@2 won leader election│ │
│ │                               with quorum 0,1,2               │ │
│ │ 2026-03-04 10:00:03.600 INFO  monmap epoch 3 contains        │ │
│ │                               3 mons: a, b, ext-c            │ │
│ │ 2026-03-04 10:00:03.700 INFO  mon.ext-c joined quorum        │ │
│ │ 2026-03-04 10:00:04.000 INFO  mon.ext-c is ready             │ │
│ └─────────────────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Health check passing
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Pod Status Update                                                 │
│ ├─ Status: Running                                               │
│ ├─ Ready: True                                                   │
│ └─ ContainersReady: True                                         │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Ceph Quorum Established
        ▼
┌───────────────────────────────────────────────────────────────────┐
│                    CEPH CLUSTER STATE                             │
│                                                                   │
│ Quorum:                                                           │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ Mon ID  │  Host          │  Address           │  Status     │ │
│ │─────────┼────────────────┼────────────────────┼────────────│ │
│ │ a       │  rook-mon-a    │  10.0.1.10:3300    │  ✓ Online  │ │
│ │ b       │  rook-mon-b    │  10.0.1.11:3300    │  ✓ Online  │ │
│ │ ext-c   │  arbiter-pod   │  10.96.100.50:3300 │  ✓ Online  │ │
│ └─────────────────────────────────────────────────────────────┘ │
│                                                                   │
│ Cluster Health: HEALTH_OK                                         │
│ Quorum Status: 3 monitors in quorum (a, b, ext-c)               │
└───────────────────────────────────────────────────────────────────┘


PHASE 7: FINAL STATUS UPDATE
═══════════════════════════════════════════════════════════════════════════════════════════════

SOURCE CLUSTER
─────────────
        │
        │ Operator checks Deployment status
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ RemoteArbiter Controller: Reconcile()                             │
│                                                                   │
│ Step 16: Check Arbiter Deployment Status (via remote client)     │
│ ├─ GET /apis/apps/v1/namespaces/external-arbiter/deployments/   │
│ │      external-arbiter                                          │
│ │                                                                │
│ ├─ Deployment.Status.Replicas = 1                               │
│ ├─ Deployment.Status.ReadyReplicas = 1                          │
│ ├─ Deployment.Status.AvailableReplicas = 1                      │
│ └─ ✓ Deployment is ready                                        │
│    └─ Update Condition: ArbiterDeploymentReady = True           │
└───────────────────────────────────────────────────────────────────┘
        │
        │ All conditions met
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Update RemoteArbiter Status                                       │
│ ├─ State: Ready                                                  │
│ ├─ Message: "Remote arbiter is ready and joined quorum"         │
│ ├─ MonID: ext-c                                                  │
│ └─ All conditions: True                                           │
└───────────────────────────────────────────────────────────────────┘
        │
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ kubectl get remotearbiter external-arbiter -o wide                │
│                                                                   │
│ NAME               MON ID  STATE   MESSAGE                        │
│ external-arbiter   ext-c   Ready   Remote arbiter is ready...    │
└───────────────────────────────────────────────────────────────────┘
        │
        │ Schedule periodic health check
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ Requeue after checkInterval (1m)                                  │
│                                                                   │
│ Future reconciliations will:                                      │
│ ├─ Verify RemoteCluster still accessible                         │
│ ├─ Verify Arbiter Deployment still ready                         │
│ ├─ Detect configuration drift and reconcile                      │
│ └─ Update status if any issues detected                          │
└───────────────────────────────────────────────────────────────────┘


PHASE 8: STEADY STATE MONITORING
═══════════════════════════════════════════════════════════════════════════════════════════════

┌───────────────────────────────────────────────────────────────────┐
│                    Ongoing Operations                             │
└───────────────────────────────────────────────────────────────────┘

SOURCE CLUSTER                                  REMOTE CLUSTER
─────────────                                   ─────────────
        │                                               │
        │ Every 1 minute (checkInterval)                │
        ▼                                               │
┌──────────────────┐                                    │
│ Reconcile        │                                    │
│ RemoteCluster    │────Health Check API Call──────────►│
│                  │                                    │
│ ✓ Reachable      │◄───────200 OK────────────────────┤
└──────────────────┘                                    │
        │                                               │
        ▼                                               │
┌──────────────────┐                                    │
│ Reconcile        │                                    │
│ RemoteArbiter    │────Check Deployment Status────────►│
│                  │                                    │
│ ✓ Ready (1/1)    │◄───────Deployment Info───────────┤
└──────────────────┘                                    │
        │                                               │
        │ Monitor Ceph Cluster Health                   │
        ▼                                               │
┌──────────────────┐                                    │
│ CephCluster      │                                    │
│ Health: OK       │                                    │
│ Quorum: 3 mons   │◄────Heartbeats──────────────────┤
└──────────────────┘                                    │
                                                        │
                                                        ▼
                                              ┌──────────────────┐
                                              │ Arbiter Pod      │
                                              │ Status: Running  │
                                              │ Ready: True      │
                                              │ Uptime: 45m      │
                                              └──────────────────┘


═══════════════════════════════════════════════════════════════════════════════════════════════
                                    DEPLOYMENT COMPLETE
═══════════════════════════════════════════════════════════════════════════════════════════════

Final State:
✓ Operator running on source cluster
✓ RemoteCluster validated and ready
✓ RemoteArbiter deployed and ready
✓ Arbiter pod running on remote cluster
✓ Ceph monitor joined quorum as ext-c
✓ Continuous health monitoring active
✓ Geographic redundancy achieved

```

## Deployment Command Summary

```bash
# ════════════════════════════════════════════════════════════════════
#                      QUICK DEPLOYMENT REFERENCE
# ════════════════════════════════════════════════════════════════════

# REMOTE CLUSTER SETUP
# ────────────────────
kubectl create namespace external-arbiter
./hack/configure-k8s-user.sh  # Creates SA, Role, RoleBinding, kubeconfig

# SOURCE CLUSTER SETUP
# ────────────────────
# 1. Create secret with remote kubeconfig
kubectl create secret generic external-arbiter \
  --from-file=kubeconfig.yaml \
  -n arbiter-operator

# 2. Install operator
helm install arbiter-operator \
  ./contrib/charts/external-arbiter-operator \
  --create-namespace \
  --namespace arbiter-operator \
  --values ./contrib/charts/external-arbiter-operator/values.yaml

# 3. Create RemoteCluster
kubectl apply -f contrib/k8s/examples/remote-cluster.yaml -n arbiter-operator

# 4. Wait for RemoteCluster ready
kubectl wait --for=condition=Ready remotecluster/external-arbiter \
  -n arbiter-operator --timeout=60s

# 5. Create RemoteArbiter
kubectl apply -f contrib/k8s/examples/remote-arbiter.yaml -n arbiter-operator

# 6. Watch deployment progress
kubectl get remotearbiter -n arbiter-operator -w

# VERIFICATION
# ────────────
# Check operator logs
kubectl logs -n arbiter-operator deployment/external-arbiter-operator-manager -f

# Check RemoteCluster status
kubectl get remotecluster -n arbiter-operator -o yaml

# Check RemoteArbiter status
kubectl get remotearbiter -n arbiter-operator -o yaml

# Check arbiter pod on remote cluster (switch kubeconfig)
kubectl get pods -n external-arbiter
kubectl logs -n external-arbiter deployment/external-arbiter

# Verify Ceph quorum (from source cluster)
kubectl exec -n rook-ceph deployment/rook-ceph-tools -it -- ceph mon dump
kubectl exec -n rook-ceph deployment/rook-ceph-tools -it -- ceph -s
```

## Troubleshooting Flow

```
Problem: RemoteCluster stuck in Progressing state
├─ Check: kubectl describe remotecluster <name> -n arbiter-operator
├─ Look at: Status.Conditions[]
│   ├─ SecretAvailable = False
│   │   └─ Fix: Verify secret exists and has correct key
│   ├─ ConfigValid = False
│   │   └─ Fix: Verify kubeconfig format and credentials
│   ├─ ClusterReachable = False
│   │   └─ Fix: Check network connectivity, API server URL
│   └─ HasEnoughPermissions = False
│       └─ Fix: Review RBAC on remote cluster
└─ Check operator logs for detailed error messages

Problem: RemoteArbiter stuck in Progressing state
├─ Check: kubectl describe remotearbiter <name> -n arbiter-operator
├─ Look at: Status.Conditions[]
│   ├─ RemoteClusterReady = False
│   │   └─ Fix: Resolve RemoteCluster issues first
│   ├─ CephClusterReady = False
│   │   └─ Fix: Check Rook CephCluster status
│   ├─ ArbiterDeploymentExists = False
│   │   └─ Fix: Check remote cluster for deployment errors
│   └─ ArbiterDeploymentReady = False
│       └─ Fix: Check arbiter pod logs on remote cluster
└─ Check operator logs for deployment errors

Problem: Arbiter pod CrashLoopBackOff
├─ Check pod logs: kubectl logs -n external-arbiter <pod-name>
├─ Common issues:
│   ├─ Invalid keyring → Verify secret content
│   ├─ Cannot reach other monitors → Check network/DNS
│   ├─ FSID mismatch → Verify configuration extraction
│   └─ Permission denied → Check pod security context
└─ Restart: kubectl rollout restart deployment/external-arbiter -n external-arbiter
```
