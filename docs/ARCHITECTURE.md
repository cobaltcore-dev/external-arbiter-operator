# External Arbiter Operator - Architecture Diagram

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                         SOURCE CLUSTER (Cluster A)                              │
│                                                                                 │
│  ┌───────────────────────────────────────────────────────────────────────────┐ │
│  │                          Rook Operator Namespace                          │ │
│  │                                                                           │ │
│  │  ┌──────────────────┐      ┌──────────────────┐     ┌──────────────────┐│ │
│  │  │  Rook Operator   │      │  CephCluster CR  │     │   Ceph Cluster   ││ │
│  │  │                  │─────▶│   (my-cluster)   │────▶│   Mon-A, Mon-B   ││ │
│  │  └──────────────────┘      └──────────────────┘     │   OSD-0, OSD-1   ││ │
│  │                                     │                │   MGR, RGW, MDS  ││ │
│  │                                     │                └──────────────────┘│ │
│  └─────────────────────────────────────┼───────────────────────────────────┘ │
│                                        │                                      │
│  ┌─────────────────────────────────────┼───────────────────────────────────┐ │
│  │           Arbiter Operator Namespace│                                   │ │
│  │                                     ▼                                   │ │
│  │  ┌────────────────────────────────────────────────────────────┐        │ │
│  │  │         External Arbiter Operator (Manager Pod)            │        │ │
│  │  │  ┌──────────────────────┐  ┌──────────────────────────┐   │        │ │
│  │  │  │ RemoteCluster        │  │ RemoteArbiter            │   │        │ │
│  │  │  │ Controller           │  │ Controller               │   │        │ │
│  │  │  └──────────┬───────────┘  └──────────┬───────────────┘   │        │ │
│  │  │             │                         │                    │        │ │
│  │  │  ┌──────────▼───────────┐  ┌──────────▼───────────────┐   │        │ │
│  │  │  │ RemoteCluster        │  │ RemoteArbiter            │   │        │ │
│  │  │  │ Webhook              │  │ Webhook                  │   │        │ │
│  │  │  └──────────────────────┘  └──────────────────────────┘   │        │ │
│  │  └────────────────────────────────────────────────────────────┘        │ │
│  │                     │                         │                         │ │
│  │                     │                         │                         │ │
│  │  ┌──────────────────▼──────┐   ┌──────────────▼──────────────────────┐ │ │
│  │  │  RemoteCluster CR       │   │  RemoteArbiter CR                   │ │ │
│  │  │  ┌──────────────────┐   │   │  ┌──────────────────────────────┐  │ │ │
│  │  │  │ Spec:            │   │   │  │ Spec:                        │  │ │ │
│  │  │  │ - namespace      │   │   │  │ - cephCluster (ref)          │  │ │ │
│  │  │  │ - accessKeyRef   │   │   │  │ - remoteCluster (ref/inline) │  │ │ │
│  │  │  │ - checkInterval  │   │   │  │ - monIdPrefix                │  │ │ │
│  │  │  │ - timeout        │   │   │  │ - service config             │  │ │ │
│  │  │  └──────────────────┘   │   │  │ - deployment config          │  │ │ │
│  │  │  ┌──────────────────┐   │   │  └──────────────────────────────┘  │ │ │
│  │  │  │ Status:          │   │   │  ┌──────────────────────────────┐  │ │ │
│  │  │  │ - state          │   │   │  │ Status:                      │  │ │ │
│  │  │  │ - message        │   │   │  │ - state                      │  │ │ │
│  │  │  │ - conditions[]   │   │   │  │ - message                    │  │ │ │
│  │  │  └──────────────────┘   │   │  │ - monId                      │  │ │ │
│  │  └─────────────────────────┘   │  │ - conditions[]               │  │ │ │
│  │              │                  │  └──────────────────────────────┘  │ │ │
│  │              │                  └────────────────────────────────────┘ │ │
│  │  ┌───────────▼──────────┐                                             │ │
│  │  │  Secret              │                                             │ │
│  │  │  (kubeconfig.yaml)   │                                             │ │
│  │  │  ┌────────────────┐  │                                             │ │
│  │  │  │ Remote K8s     │  │                                             │ │
│  │  │  │ API Server     │  │                                             │ │
│  │  │  │ Credentials    │  │                                             │ │
│  │  │  └────────────────┘  │                                             │ │
│  │  └─────────────────────┘                                              │ │
│  └─────────────────────────────────────────────────────────────────────┘ │
│                                        │                                   │
└────────────────────────────────────────┼───────────────────────────────────┘
                                         │
                                         │ Kubeconfig Auth
                                         │ REST API Calls
                                         │
┌────────────────────────────────────────┼───────────────────────────────────┐
│                         REMOTE CLUSTER │(Cluster B)                        │
│                                        │                                   │
│  ┌─────────────────────────────────────▼───────────────────────────────┐ │
│  │                     Target Namespace (external-arbiter)             │ │
│  │                                                                     │ │
│  │  ┌──────────────────────────────────────────────────────────────┐  │ │
│  │  │              Arbiter Deployment (Pod)                        │  │ │
│  │  │  ┌────────────────────────────────────────────────────────┐  │  │ │
│  │  │  │  Container: Ceph Monitor (ext-a)                       │  │  │ │
│  │  │  │  (DeepCopy of source Rook mon Deployment, patched)     │  │  │ │
│  │  │  │  ┌──────────────────────────────────────────────────┐  │  │  │ │
│  │  │  │  │  Command/args: inherited from source Rook mon     │  │  │  │ │
│  │  │  │  │  - Joins Ceph quorum                              │  │  │  │ │
│  │  │  │  │  - Participates in consensus                      │  │  │  │ │
│  │  │  │  │  - Local mon store at /var/lib/rook/mon-ext-a/data│  │  │  │ │
│  │  │  │  └──────────────────────────────────────────────────┘  │  │  │ │
│  │  │  │                                                         │  │  │ │
│  │  │  │  Volumes (patched from source mon):                     │  │  │ │
│  │  │  │  - rook-ceph-mons-keyring (→ keyring Secret)            │  │  │ │
│  │  │  │  - rook-config-override (→ override ConfigMap)          │  │  │ │
│  │  │  │  - ceph-daemon-data (hostPath mon-ext-a/data)           │  │  │ │
│  │  │  │  - monmap (emptyDir; built by monmap init container)    │  │  │ │
│  │  │  └─────────────────────────────────────────────────────────┘  │  │ │
│  │  └──────────────────────────────────────────────────────────────┘  │ │
│  │                                    │                                │ │
│  │  ┌─────────────────────────────────▼────────────────────────────┐  │ │
│  │  │              Service (arbiter-service-<random>)                 │  │ │
│  │  │  Type: ClusterIP / NodePort / LoadBalancer                   │  │ │
│  │  │  (created only when spec.service is set; else uses POD_IP)   │  │ │
│  │  │  Ports: 6789 (tcp-msgr1 v1), 3300 (tcp-msgr2 v2)             │  │ │
│  │  │  Selector: ceph.cobaltcore.sap.com/lookup=<arbiter name>     │  │ │
│  │  └──────────────────────────────────────────────────────────────┘  │ │
│  │                                                                     │ │
│  │  ┌──────────────────────────────────────────────────────────────┐  │ │
│  │  │         Secret: arbiter-keyring-secret-<random>              │  │ │
│  │  │  (role=keyring label; Data copied verbatim from source)      │  │ │
│  │  │  - keyring (Ceph authentication key)                         │  │ │
│  │  └──────────────────────────────────────────────────────────────┘  │ │
│  │                                                                     │ │
│  │  ┌──────────────────────────────────────────────────────────────┐  │ │
│  │  │         ConfigMap: arbiter-override-configmap-<random>       │  │ │
│  │  │  (lookup label only; Data copied verbatim from source)       │  │ │
│  │  │  - ceph.conf (Ceph configuration)                            │  │ │
│  │  └──────────────────────────────────────────────────────────────┘  │ │
│  │                                                                     │ │
│  │  ┌──────────────────────────────────────────────────────────────┐  │ │
│  │  │         Secret: arbiter-env-var-secret-<random>              │  │ │
│  │  │  (role=envvar label; Data copied verbatim from source)       │  │ │
│  │  │  - ROOK_CEPH_MON_HOST                                        │  │ │
│  │  │  - ROOK_CEPH_MON_INITIAL_MEMBERS                             │  │ │
│  │  └──────────────────────────────────────────────────────────────┘  │ │
│  │                                                                     │ │
│  │  ┌──────────────────────────────────────────────────────────────┐  │ │
│  │  │         ServiceAccount: arbiter-sa                           │  │ │
│  │  │         Role: arbiter-role                                   │  │ │
│  │  │         RoleBinding: arbiter-rolebinding                     │  │ │
│  │  └──────────────────────────────────────────────────────────────┘  │ │
│  └─────────────────────────────────────────────────────────────────┘ │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
                                   │
                                   │ Ceph Protocol (6789/3300)
                                   │ Monitor Communication
                                   ▼
         ┌─────────────────────────────────────────────┐
         │         Ceph Quorum (Consensus)             │
         │  ┌────────┐  ┌────────┐  ┌────────┐         │
         │  │ mon-a  │  │ mon-b  │  │ ext-a  │         │
         │  │(Src)   │  │(Src)   │  │(Arbtr) │         │
         │  └────────┘  └────────┘  └────────┘         │
         │        Arbiter is a voting member            │
         └─────────────────────────────────────────────┘
```

## Reconciliation Flow

### RemoteCluster Controller Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                    RemoteCluster Reconciliation                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                   ┌──────────────────────┐
                   │ 1. Fetch RemoteCluster│
                   │    Resource (CR)      │
                   └──────────┬────────────┘
                              │
                              ▼
                   ┌──────────────────────┐
                   │ 2. Check Deletion    │
                   │    Timestamp         │
                   └──────────┬────────────┘
                              │
                    ┌─────────┴─────────┐
                    │                   │
              Deleting?               No
                    │                   │
                    ▼                   ▼
         ┌──────────────────┐  ┌────────────────────┐
         │ Clean Up:        │  │ 3. Add Finalizer   │
         │ - Check deps     │  │    if missing      │
         │ - Remove finalizer│ └─────────┬──────────┘
         └──────────────────┘            │
                                         ▼
                              ┌──────────────────────┐
                              │ 4. Initialize Status │
                              │    Conditions        │
                              └─────────┬────────────┘
                                        │
                                        ▼
                              ┌──────────────────────┐
                              │ 5. Fetch Secret      │
                              │    (kubeconfig)      │
                              └─────────┬────────────┘
                                        │
                              Condition: SecretAvailable
                                        │
                                        ▼
                              ┌──────────────────────┐
                              │ 6. Create Remote     │
                              │    K8s Client        │
                              └─────────┬────────────┘
                                        │
                              Condition: ConfigValid
                                        │
                                        ▼
                              ┌──────────────────────┐
                              │ 7. Check Cluster     │
                              │    Reachability      │
                              │    (API call)        │
                              └─────────┬────────────┘
                                        │
                              Condition: ClusterReachable
                                        │
                                        ▼
                              ┌──────────────────────┐
                              │ 8. Check Permissions │
                              │    (SelfSubjectAccess│
                              │     Review)          │
                              └─────────┬────────────┘
                                        │
                     Condition: HasEnoughPermissions
                                        │
                                        ▼
                              ┌──────────────────────┐
                              │ 9. Update Status     │
                              │    State: Ready      │
                              └─────────┬────────────┘
                                        │
                                        ▼
                              ┌──────────────────────┐
                              │ 10. Requeue after    │
                              │     checkInterval    │
                              └──────────────────────┘
```

### RemoteArbiter Controller Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                    RemoteArbiter Reconciliation                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                   ┌──────────────────────┐
                   │ 1. Fetch RemoteArbiter│
                   │    Resource (CR)      │
                   └──────────┬────────────┘
                              │
                              ▼
                   ┌──────────────────────┐
                   │ 2. Check Deletion    │
                   │    Timestamp         │
                   └──────────┬────────────┘
                              │
                    ┌─────────┴─────────┐
                    │                   │
              Deleting?               No
                    │                   │
                    ▼                   ▼
         ┌──────────────────┐  ┌────────────────────┐
         │ Clean Up:        │  │ 3. Add Finalizer   │
         │ - Delete arbiter │  │    if missing      │
         │   deployment     │  └─────────┬──────────┘
         │ - Delete secrets │            │
         │ - Delete service │            ▼
         │ - Remove finalizer│ ┌────────────────────┐
         └──────────────────┘  │ 4. Initialize Status│
                               │    Conditions       │
                               └─────────┬───────────┘
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 5. Fetch/Create    │
                               │    RemoteCluster   │
                               └─────────┬──────────┘
                                         │
                       Condition: RemoteClusterExists
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 6. Check Remote    │
                               │    Cluster Ready   │
                               └─────────┬──────────┘
                                         │
                       Condition: RemoteClusterReady
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 7. Fetch CephCluster│
                               │    (Rook Resource) │
                               └─────────┬──────────┘
                                         │
                       Condition: CephClusterExists
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 8. Check Ceph      │
                               │    Cluster Ready   │
                               └─────────┬──────────┘
                                         │
                       Condition: CephClusterReady
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 9. Generate MonID  │
                               │    (ext-a, ext-b)  │
                               │    from ExternalMon│
                               │    IDs (first free)│
                               └─────────┬──────────┘
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 10. Read Ceph      │
                               │     Config from    │
                               │     source:        │
                               │     - mon secrets  │
                               │     - mon configmap│
                               │     - mon deploymnt│
                               └─────────┬──────────┘
                                         │
                     Condition: CephClusterConfigured,
                                MonitorDeploymentExists,
                                MonitorDeploymentReady
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 11. Create/Update  │
                               │     Arbiter Secret │
                               │     (keyring)      │
                               └─────────┬──────────┘
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 12. Create/Update  │
                               │     Arbiter        │
                               │     ConfigMap      │
                               │     (ceph.conf)    │
                               └─────────┬──────────┘
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 13. Create/Update  │
                               │     Arbiter EnvVar │
                               │     Secret         │
                               └─────────┬──────────┘
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 14. Create/Update  │
                               │     Arbiter Service│
                               │  (only if spec.    │
                               │   service is set)  │
                               └─────────┬──────────┘
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 15. Create/Update  │
                               │     Arbiter        │
                               │     Deployment     │
                               └─────────┬──────────┘
                                         │
                   Condition: ArbiterDeploymentExists
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 16. Check Arbiter  │
                               │     Pod Ready      │
                               └─────────┬──────────┘
                                         │
                    Condition: ArbiterDeploymentReady
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 17. Update Status  │
                               │     State: Ready   │
                               │     MonID: ext-a   │
                               └─────────┬──────────┘
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 18. Requeue after  │
                               │     checkInterval  │
                               └────────────────────┘
```

## Component Interactions

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                          Component Interaction Flow                          │
└──────────────────────────────────────────────────────────────────────────────┘

   User                Kubectl           Operator           Remote            Ceph
    │                    │                  │              Cluster          Cluster
    │                    │                  │                 │                │
    │  1. Create Secret  │                  │                 │                │
    ├───────────────────►│                  │                 │                │
    │   (kubeconfig)     │                  │                 │                │
    │                    │                  │                 │                │
    │  2. Apply          │                  │                 │                │
    │     RemoteCluster  │                  │                 │                │
    ├───────────────────►│──────Watch──────►│                 │                │
    │     CR             │                  │                 │                │
    │                    │                  │  3. Validate    │                │
    │                    │                  │     Connection  │                │
    │                    │                  ├────────────────►│                │
    │                    │                  │                 │                │
    │                    │                  │◄────────────────┤                │
    │                    │                  │   200 OK        │                │
    │                    │                  │                 │                │
    │                    │                  │  4. Check       │                │
    │                    │                  │     Permissions │                │
    │                    │                  ├────────────────►│                │
    │                    │                  │  (SSAR)         │                │
    │                    │                  │◄────────────────┤                │
    │                    │                  │   Allowed       │                │
    │                    │                  │                 │                │
    │                    │                  │  5. Update      │                │
    │                    │                  │     Status      │                │
    │                    │                  │     Ready       │                │
    │                    │                  │                 │                │
    │  6. Apply          │                  │                 │                │
    │     RemoteArbiter  │                  │                 │                │
    ├───────────────────►│──────Watch──────►│                 │                │
    │     CR             │                  │                 │                │
    │                    │                  │  7. Fetch       │                │
    │                    │                  │     CephCluster │                │
    │                    │                  ├─────────────────┼───────────────►│
    │                    │                  │                 │                │
    │                    │                  │  8. Read Ceph   │                │
    │                    │                  │     Config      │                │
    │                    │                  │◄────────────────┼────────────────┤
    │                    │                  │                 │                │
    │                    │                  │  9. Create      │                │
    │                    │                  │     Secrets     │                │
    │                    │                  ├────────────────►│                │
    │                    │                  │                 │                │
    │                    │                  │  10. Create     │                │
    │                    │                  │      ConfigMap  │                │
    │                    │                  ├────────────────►│                │
    │                    │                  │                 │                │
    │                    │                  │  11. Create     │                │
    │                    │                  │      Service    │                │
    │                    │                  ├────────────────►│                │
    │                    │                  │                 │                │
    │                    │                  │  12. Create     │                │
    │                    │                  │      Deployment │                │
    │                    │                  ├────────────────►│                │
    │                    │                  │                 │                │
    │                    │                  │                 │  13. Arbiter   │
    │                    │                  │                 │      Pod Starts│
    │                    │                  │                 │                │
    │                    │                  │                 │  14. Join      │
    │                    │                  │                 │      Quorum    │
    │                    │                  │                 ├───────────────►│
    │                    │                  │                 │  (Ceph Proto)  │
    │                    │                  │                 │◄───────────────┤
    │                    │                  │                 │  Quorum Formed │
    │                    │                  │                 │                │
    │                    │                  │  15. Check      │                │
    │                    │                  │      Deployment │                │
    │                    │                  │      Status     │                │
    │                    │                  │◄────────────────┤                │
    │                    │                  │   Ready         │                │
    │                    │                  │                 │                │
    │  16. Status        │                  │                 │                │
    │      Update        │                  │                 │                │
    │◄───────────────────┤◄─────Watch──────┤                 │                │
    │  State: Ready      │                  │                 │                │
    │  MonID: ext-a      │                  │                 │                │
    │                    │                  │                 │                │
```

## Data Flow - Ceph Configuration

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Ceph Configuration Data Flow                            │
└─────────────────────────────────────────────────────────────────────────────┘

SOURCE CLUSTER                                          REMOTE CLUSTER
     │                                                        │
     │  Rook Ceph Resources                                  │
     │                                                        │
     ▼                                                        │
┌─────────────────────┐                                      │
│ Monitor Deployment  │                                      │
│ (rook-ceph-mon-a)   │                                      │
│                     │                                      │
│ - Image             │──────────┐                           │
│ - Command           │          │                           │
│ - Args              │          │                           │
└─────────────────────┘          │                           │
                                 │                           │
┌─────────────────────┐          │                           │
│ Monitor Secrets     │          │                           │
│ - mon-a-keyring     │          │                           │
│ - mon-secret        │──────────┤                           │
│ - admin-keyring     │          │                           │
└─────────────────────┘          │                           │
                                 │   Operator Reads          │
┌─────────────────────┐          │   & Transforms            │
│ Monitor ConfigMap   │          │                           │
│ - ceph.conf         │──────────┤                           │
│ - mon-endpoints     │          │                           │
└─────────────────────┘          │                           │
                                 │                           │
┌─────────────────────┐          │                           │
│ CephCluster CR      │          │                           │
│ - mon count         │──────────┤                           │
│ - network config    │          │                           │
│ - version           │          │                           │
└─────────────────────┘          │                           │
                                 │                           │
                                 ▼                           │
                    ┌──────────────────────┐                 │
                    │  Operator Transform  │                 │
                    │                      │                 │
                    │  1. Generate MonID   │                 │
                    │  2. Copy keyring     │                 │
                    │     Secret verbatim  │                 │
                    │  3. Copy override CM │                 │
                    │     verbatim         │                 │
                    │  4. Copy envvar      │                 │
                    │     Secret verbatim  │                 │
                    │  5. DeepCopy source  │                 │
                    │     mon Deployment,  │                 │
                    │     add monmap init  │                 │
                    │     container        │                 │
                    └──────────┬───────────┘                 │
                               │                             │
                               │  Creates Resources          │
                               │  (all GenerateName;          │
                               │   found by lookup label)     │
                               │                             │
                               ▼                             ▼
                                           ┌─────────────────────────┐
                                           │ Arbiter Secret          │
                                           │ arbiter-keyring-secret-  │
                                           │   (Data from source)     │
                                           └─────────────────────────┘
                                                       │
                                           ┌───────────▼─────────────┐
                                           │ Arbiter ConfigMap       │
                                           │ arbiter-override-        │
                                           │   configmap-<random>     │
                                           │   (Data from source)     │
                                           └─────────────────────────┘
                                                       │
                                           ┌───────────▼─────────────┐
                                           │ Arbiter EnvVar Secret   │
                                           │ arbiter-env-var-secret-  │
                                           │   (Data from source)     │
                                           └─────────────────────────┘
                                                       │
                                           ┌───────────▼─────────────┐
                                           │ Arbiter Deployment      │
                                           │ arbiter-deployment-      │
                                           │                         │
                                           │ Volumes (patched from    │
                                           │ source mon by name):     │
                                           │ - rook-ceph-mons-keyring │
                                           │ - rook-config-override   │
                                           │ - ceph-daemon-data       │
                                           │ - monmap (emptyDir)      │
                                           └─────────────────────────┘
```

## Permission Model

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Permission Requirements                           │
└─────────────────────────────────────────────────────────────────────────────┘

SOURCE CLUSTER (Operator Namespace)
┌───────────────────────────────────────────────────────────────┐
│ Service Account: <release>-controller-manager                 │
│                                                               │
│ Cluster-level permissions:                                    │
│ - cephclusters.ceph.rook.io (get, list, watch)              │
│ - remoteclusters.ceph.cobaltcore.sap.com (all)              │
│ - remotearbiters.ceph.cobaltcore.sap.com (all)              │
│                                                               │
│ Namespace-level permissions (rook-ceph):                      │
│ - secrets (get, list, watch)                                  │
│ - configmaps (get, list, watch)                               │
│ - deployments (get, list, watch)                              │
│ - services (get, list, watch)                                 │
└───────────────────────────────────────────────────────────────┘

REMOTE CLUSTER (Target Namespace)
┌───────────────────────────────────────────────────────────────┐
│ Service Account: arbiter-sa (created by user)                 │
│                                                               │
│ Namespace-level permissions (external-arbiter):               │
│ - deployments (create, get, list, watch, update, delete)     │
│ - secrets (create, get, list, watch, update, delete)         │
│ - configmaps (create, get, list, watch, update, delete)      │
│ - services (create, get, list, watch, update, delete)        │
│ - deployments/status (get)                                    │
│ - deployments/finalizers (update)                             │
│                                                               │
│ Cluster-level permissions:                                    │
│ - selfsubjectaccessreviews (create) [for permission check]    │
│                                                               │
│ Kubeconfig stored as Secret in source cluster                 │
└───────────────────────────────────────────────────────────────┘
```

## State Machines

### RemoteCluster State Machine

```
     ┌──────┐
     │ Init │
     └───┬──┘
         │ CR Created
         ▼
  ┌──────────────┐
  │ Progressing  │◄────────┐
  └───┬──────────┘         │
      │                    │ Periodic Check
      │ All Conditions OK  │ (checkInterval)
      ▼                    │
   ┌───────┐               │
   │ Ready ├───────────────┘
   └───┬───┘
       │ Error Detected
       ▼
   ┌───────┐
   │ Error │
   └───┬───┘
       │ Retry/Fix
       ▼
  ┌──────────────┐
  │ Progressing  │
  └──────────────┘
       │ Deletion Requested
       ▼
  ┌──────────┐
  │ Deleting │
  └──────────┘
```

### RemoteArbiter State Machine

```
     ┌──────┐
     │ Init │
     └───┬──┘
         │ CR Created
         ▼
  ┌──────────────┐
  │ Progressing  │◄────────┐
  └───┬──────────┘         │
      │                    │ Periodic Check
      │ Deployment Ready   │ (checkInterval)
      ▼                    │
   ┌───────┐               │
   │ Ready ├───────────────┘
   └───┬───┘
       │ Error Detected
       ▼
   ┌───────┐
   │ Error │
   └───┬───┘
       │ Retry/Fix
       ▼
  ┌──────────────┐
  │ Progressing  │
  └──────────────┘
       │ Deletion Requested
       ▼
  ┌──────────┐
  │ Deleting │
  └──────────┘
```

## Key Design Patterns

1. **Reconciliation Loop Pattern**: Controllers continuously watch resources and reconcile actual state with desired state

2. **Owner References**: Arbiter resources on remote cluster may have finalizers but are managed through operator lifecycle

3. **Condition-based Status**: Detailed conditions show progress through reconciliation steps

4. **Cross-cluster Communication**: Operator uses kubeconfig credentials to manage resources on remote cluster

5. **Configuration Extraction**: Ceph configuration is read from Rook-managed resources and transformed for arbiter

6. **Health Monitoring**: Periodic reconciliation ensures remote cluster and arbiter remain healthy

7. **Finalizers**: Ensure clean deletion of resources across both clusters

8. **Webhooks**: Validate resource specifications before admission to cluster
