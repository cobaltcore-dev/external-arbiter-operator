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
│  │  │  │  Container: Ceph Monitor (ext-c)                       │  │  │ │
│  │  │  │  ┌──────────────────────────────────────────────────┐  │  │  │ │
│  │  │  │  │  Command: ceph-mon                                │  │  │  │ │
│  │  │  │  │  - Joins Ceph quorum                              │  │  │  │ │
│  │  │  │  │  - Participates in consensus                      │  │  │  │ │
│  │  │  │  │  - No data storage (arbiter mode)                 │  │  │  │ │
│  │  │  │  └──────────────────────────────────────────────────┘  │  │  │ │
│  │  │  │                                                         │  │  │ │
│  │  │  │  Volumes:                                               │  │  │ │
│  │  │  │  - keyring-secret (from Secret)                         │  │  │ │
│  │  │  │  - override-configmap (from ConfigMap)                  │  │  │ │
│  │  │  │  - envvar-secret (from Secret)                          │  │  │ │
│  │  │  │  - arbiter-monmap (emptyDir)                            │  │  │ │
│  │  │  └─────────────────────────────────────────────────────────┘  │  │ │
│  │  └──────────────────────────────────────────────────────────────┘  │ │
│  │                                    │                                │ │
│  │  ┌─────────────────────────────────▼────────────────────────────┐  │ │
│  │  │              Service (arbiter-service)                        │  │ │
│  │  │  Type: ClusterIP / NodePort / LoadBalancer                   │  │ │
│  │  │  Port: 3300 (Ceph mon port)                                  │  │ │
│  │  │  Selector: role=arbiter, lookup=external-arbiter             │  │ │
│  │  └──────────────────────────────────────────────────────────────┘  │ │
│  │                                                                     │ │
│  │  ┌──────────────────────────────────────────────────────────────┐  │ │
│  │  │         Secret: arbiter-keyring                              │  │ │
│  │  │  - keyring (Ceph authentication key)                         │  │ │
│  │  └──────────────────────────────────────────────────────────────┘  │ │
│  │                                                                     │ │
│  │  ┌──────────────────────────────────────────────────────────────┐  │ │
│  │  │         ConfigMap: arbiter-override                          │  │ │
│  │  │  - ceph.conf (Ceph configuration)                            │  │ │
│  │  └──────────────────────────────────────────────────────────────┘  │ │
│  │                                                                     │ │
│  │  ┌──────────────────────────────────────────────────────────────┐  │ │
│  │  │         Secret: arbiter-envvar                               │  │ │
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
                                   │ Ceph Protocol (3300)
                                   │ Monitor Communication
                                   ▼
         ┌─────────────────────────────────────────────┐
         │         Ceph Quorum (Consensus)             │
         │  ┌────────┐  ┌────────┐  ┌────────┐         │
         │  │ Mon-A  │  │ Mon-B  │  │ Mon-C  │         │
         │  │(Src)   │  │(Src)   │  │(Remote)│         │
         │  └────────┘  └────────┘  └────────┘         │
         │             Arbiter Mode                     │
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
                               │ 9. Read Ceph Config│
                               │    from source:    │
                               │    - mon secrets   │
                               │    - mon configmap │
                               │    - mon deployment│
                               └─────────┬──────────┘
                                         │
                     Condition: CephClusterConfigured
                                         │
                                         ▼
                               ┌────────────────────┐
                               │ 10. Generate MonID │
                               │     (ext-c, ext-d) │
                               └─────────┬──────────┘
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
                               │     MonID: ext-c   │
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
    │  MonID: ext-c      │                  │                 │                │
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
                    │  2. Extract keyring  │                 │
                    │  3. Update ceph.conf │                 │
                    │  4. Set mon endpoints│                 │
                    │  5. Configure service│                 │
                    └──────────┬───────────┘                 │
                               │                             │
                               │  Creates Resources          │
                               │                             │
                               ▼                             ▼
                                           ┌─────────────────────────┐
                                           │ Arbiter Secret          │
                                           │ - arbiter-keyring       │
                                           │   (from source)         │
                                           └─────────────────────────┘
                                                       │
                                           ┌───────────▼─────────────┐
                                           │ Arbiter ConfigMap       │
                                           │ - ceph.conf             │
                                           │   (modified with monID) │
                                           └─────────────────────────┘
                                                       │
                                           ┌───────────▼─────────────┐
                                           │ Arbiter EnvVar Secret   │
                                           │ - ROOK_CEPH_MON_HOST    │
                                           │ - ROOK_CEPH_MON_INITIAL_│
                                           │   MEMBERS               │
                                           └─────────────────────────┘
                                                       │
                                           ┌───────────▼─────────────┐
                                           │ Arbiter Deployment      │
                                           │                         │
                                           │ Mounts:                 │
                                           │ - keyring → /etc/ceph   │
                                           │ - config → /etc/ceph    │
                                           │ - envvar → container env│
                                           └─────────────────────────┘
```

## Permission Model

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Permission Requirements                           │
└─────────────────────────────────────────────────────────────────────────────┘

SOURCE CLUSTER (Operator Namespace)
┌───────────────────────────────────────────────────────────────┐
│ Service Account: external-arbiter-operator                    │
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
