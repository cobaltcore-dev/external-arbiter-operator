// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"
)

const (
	RemoteArbiterFinalizer = "remotearbiter.ceph.cobaltcore.sap.com/finalizer"

	RemoteArbiterRestartAnnotation = "ceph.cobaltcore.sap.com/restarted-at"

	RemoteArbiterResourceVersionLabel = "ceph.cobaltcore.sap.com/last-applied-resource-version"
	RemoteArbiterLookupLabel          = "ceph.cobaltcore.sap.com/lookup"
	RemoteArbiterRoleLabel            = "ceph.cobaltcore.sap.com/role"

	RemoteArbiterRemoteClusterKey = ".remotearbiter.remotecluster"
	RemoteArbiterCephClusterKey   = ".remotearbiter.cephcluster"
	RemoteClusterOwnerKey         = ".remotecluster.owner"

	RemoteArbiterKeyringRole = "keyring"
	RemoteArbiterEnvVarRole  = "envvar"

	RemoteArbiterMonMapVolumeName = "arbiter-monmap"
	RemoteArbiterMonMapMountPath  = "/tmp/monmap"
)

var (
	ErrorNotCreated = errors.New("not created")
)

type RemoteArbiterReconcilationState struct {
	log                          *logr.Logger
	remoteArbiterObjectKey       *types.NamespacedName
	remoteArbiter                *v1alpha1.RemoteArbiter
	remoteClusterObjectKey       *types.NamespacedName
	remoteCluster                *v1alpha1.RemoteCluster
	remoteClusterSecretObjectKey *types.NamespacedName
	remoteClusterSecret          *corev1.Secret
	remoteClusterClient          client.Client
	cephClusterObjectKey         *types.NamespacedName
	cephCluster                  *rookv1.CephCluster
	// monitorDeploymentObjectKey        *types.NamespacedName
	monitorDeployment *appsv1.Deployment
	// monitorKeyringSecretObjectKey     *types.NamespacedName
	monitorKeyringSecret *corev1.Secret
	// monitorOverrideConfigMapObjectKey *types.NamespacedName
	monitorOverrideConfigMap *corev1.ConfigMap
	// monitorEnvVarSecretObjectKey      *types.NamespacedName
	monitorEnvVarSecret *corev1.Secret
	// arbiterServiceObjectKey           *types.NamespacedName
	// arbiterService                    *corev1.Service
	arbiterDeploymentObjectKey        *types.NamespacedName
	arbiterDeployment                 *appsv1.Deployment
	arbiterKeyringSecretObjectKey     *types.NamespacedName
	arbiterKeyringSecret              *corev1.Secret
	arbiterOverrideConfigMapObjectKey *types.NamespacedName
	arbiterOverrideConfigMap          *corev1.ConfigMap
	arbiterEnvVarSecretObjectKey      *types.NamespacedName
	arbiterEnvVarSecret               *corev1.Secret
	outdated                          bool
	shouldRestart                     bool
}

// RemoteArbiterReconciler reconciles a RemoteArbiter object
type RemoteArbiterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *RemoteArbiterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues(RemoteArbiterTypeName, req.NamespacedName)

	s := &RemoteArbiterReconcilationState{
		log:                    &log,
		remoteArbiterObjectKey: &req.NamespacedName,
	}

	if err := r.fetchArbiter(ctx, s); apierrors.IsNotFound(err) {
		s.log.Info("resource not found, assuming it's gone")
		return ctrl.Result{}, nil
	} else if err != nil {
		log.Error(err, "unable to fetch self")
		return ctrl.Result{}, err
	}
	log.Info("self fetch complete")

	if s.remoteArbiter.GetDeletionTimestamp() != nil {
		log.Info("deletion timestamp set, cleaning up")
		if err := r.cleanUpRemoteArbiter(ctx, s); err != nil {
			log.Error(err, "unable to clean up")
			return ctrl.Result{}, err
		}
		log.Info("clean up complete")
		return ctrl.Result{}, nil
	}
	log.Info("deletion check complete")

	if err := r.checkSelfFinalizer(ctx, s); err != nil {
		log.Error(err, "unable to check self finalizer")
		return ctrl.Result{}, err
	}
	log.Info("finalizer check complete")

	if err := r.checkStatusInitialized(ctx, s); err != nil {
		log.Error(err, "unable to initialize conditions")
		return ctrl.Result{}, err
	}
	log.Info("conditions check complete")

	if err := r.fetchRemoteCluster(ctx, s); err != nil {
		log.Error(err, "unable to fetch remote cluster")
		if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
			v1alpha1.RemoteClusterExistsConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	statusMessage := "remote cluster fetched"
	log.Info(statusMessage)

	if err := r.updateRemoteArbiterStatusOnSuccess(ctx, s.remoteArbiter,
		v1alpha1.RemoteArbiterProgressingState, v1alpha1.RemoteClusterExistsConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}

	if s.remoteCluster.Status.State != v1alpha1.RemoteClusterReadyState {
		err := errors.New("remote cluster not ready")
		log.Error(err, "unable to use remote cluster state")
		if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
			v1alpha1.RemoteClusterReadyConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}
	log.Info("remote cluster ready")

	if err := r.makeRemoteClient(ctx, s); err != nil {
		log.Error(err, "unable to make remote client")
		if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
			v1alpha1.RemoteClusterReadyConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	statusMessage = "remote client usable"
	log.Info(statusMessage)

	if err := r.updateRemoteArbiterStatusOnSuccess(ctx, s.remoteArbiter,
		v1alpha1.RemoteArbiterProgressingState, v1alpha1.RemoteClusterReadyConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}

	if err := r.fetchCephCluster(ctx, s); err != nil {
		log.Error(err, "unable to fetch ceph cluster")
		if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
			v1alpha1.CephClusterExistsConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	statusMessage = "ceph cluster fetched"
	log.Info(statusMessage)

	if err := r.updateRemoteArbiterStatusOnSuccess(ctx, s.remoteArbiter,
		v1alpha1.RemoteArbiterProgressingState, v1alpha1.CephClusterExistsConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}

	if s.cephCluster.Status.Phase != rookv1.ConditionReady {
		err := errors.New("ceph cluster not ready")
		log.Error(err, "unable to use ceph cluster state")
		if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
			v1alpha1.CephClusterReadyConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	statusMessage = "ceph cluster ready"
	log.Info(statusMessage)

	if err := r.updateRemoteArbiterStatusOnSuccess(ctx, s.remoteArbiter,
		v1alpha1.RemoteArbiterProgressingState, v1alpha1.CephClusterReadyConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}

	if err := r.reserveExternalArbiterID(ctx, s); err != nil {
		log.Error(err, "unable to reserve external arbiter id")
		if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
			v1alpha1.CephClusterConfiguredConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	statusMessage = "ceph cluster configured"
	log.Info(statusMessage)

	if err := r.updateRemoteArbiterStatusOnSuccess(ctx, s.remoteArbiter,
		v1alpha1.RemoteArbiterProgressingState, v1alpha1.CephClusterConfiguredConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}

	if err := r.fetchMonitorDeployment(ctx, s); err != nil {
		log.Error(err, "unable to fetch monitor deployment")
		if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
			v1alpha1.MonitorDeploymentExistsConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	statusMessage = "monitor deployment fetched"
	log.Info(statusMessage)

	if err := r.updateRemoteArbiterStatusOnSuccess(ctx, s.remoteArbiter,
		v1alpha1.RemoteArbiterProgressingState, v1alpha1.MonitorDeploymentExistsConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}

	if s.monitorDeployment.Status.Replicas == 0 || s.monitorDeployment.Status.UnavailableReplicas != 0 {
		err := errors.New("monitor deployment is not ready")
		log.Error(err, "unable to use monitor deployment")
		if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
			v1alpha1.MonitorDeploymentReadyConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}
	if s.monitorDeployment.Status.UpdatedReplicas != s.monitorDeployment.Status.Replicas {
		err := errors.New("monitor deployment is updating self")
		log.Error(err, "unable to use monitor deployment")
		if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
			v1alpha1.MonitorDeploymentReadyConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	statusMessage = "monitor deployment is ready"
	log.Info(statusMessage)

	if err := r.updateRemoteArbiterStatusOnSuccess(ctx, s.remoteArbiter,
		v1alpha1.RemoteArbiterProgressingState, v1alpha1.MonitorDeploymentReadyConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}

	if err := r.fetchArbiterDeployment(ctx, s); err != nil {
		log.Error(err, "unable to fetch arbiter deployment")
		if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
			v1alpha1.ArbiterDeploymentExistsConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	statusMessage = "arbiter deployment fetched"
	log.Info(statusMessage)

	if err := r.updateRemoteArbiterStatusOnSuccess(ctx, s.remoteArbiter,
		v1alpha1.RemoteArbiterProgressingState, v1alpha1.ArbiterDeploymentExistsConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}

	if err := r.checkArbiterDeploymentUpToDate(ctx, s); err != nil {
		log.Error(err, "unable to check if arbiter deployment up to date")
		if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
			v1alpha1.ArbiterDeploymentReadyConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}
	log.Info("arbiter deployment should be up to date")

	if s.shouldRestart {
		log.Info("will restart arbiter deployment")
		if err := r.restartArbiterDeployment(ctx, s); err != nil {
			log.Error(err, "unable to restart arbiter deployment")
			if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
				v1alpha1.ArbiterDeploymentReadyConditionType, err); err != nil {
				log.Error(err, "unable to update resource with conditions")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, err
		}
		log.Info("arbiter deployment restart triggered")
	} else {
		log.Info("arbiter deployment restart will not be additionally triggered")
	}

	if s.arbiterDeployment.Status.Replicas == 0 || s.arbiterDeployment.Status.UnavailableReplicas != 0 {
		err := errors.New("arbiter deployment is not ready")
		log.Error(err, "unable to use arbiter deployment")
		if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
			v1alpha1.ArbiterDeploymentReadyConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}
	if s.arbiterDeployment.Status.UpdatedReplicas != s.arbiterDeployment.Status.Replicas {
		err := errors.New("arbiter deployment is updating self")
		log.Error(err, "unable to use arbiter deployment")
		if err := r.updateRemoteArbiterStatusOnFailure(ctx, s.remoteArbiter,
			v1alpha1.ArbiterDeploymentReadyConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	statusMessage = "arbiter deployment ready"
	log.Info(statusMessage)

	if err := r.updateRemoteArbiterStatusOnSuccess(ctx, s.remoteArbiter,
		v1alpha1.RemoteArbiterReadyState, v1alpha1.ArbiterDeploymentReadyConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: s.remoteArbiter.Spec.CheckInterval.Duration}, nil
}

func (r *RemoteArbiterReconciler) reserveExternalArbiterID(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	prefix := s.remoteArbiter.Spec.MonIDPrefix
	suffixFirstCode := byte(97)
	suffixLastCode := byte(122)

	externalID := s.remoteArbiter.Status.MonID
	if externalID == "" {
		for suffixCode := suffixFirstCode; suffixCode <= suffixLastCode; suffixCode++ {
			potentialID := prefix + string([]byte{suffixCode})
			if !slices.Contains(s.cephCluster.Spec.Mon.ExternalMonIDs, potentialID) {
				externalID = potentialID
				break
			}
		}
	}

	if externalID == "" {
		return fmt.Errorf("ids with prefix %s and suffixes from %s to %s are occupied", prefix, string([]byte{suffixFirstCode}), string([]byte{suffixLastCode}))
	}

	if slices.Contains(s.cephCluster.Spec.Mon.ExternalMonIDs, externalID) {
		s.log.Info("external remote arbiter id is already set", "external id", externalID)
		return nil
	}

	s.remoteArbiter.Status.MonID = externalID
	if err := r.Status().Update(ctx, s.remoteArbiter); err != nil {
		return err
	}

	s.cephCluster.Spec.Mon.ExternalMonIDs = append(s.cephCluster.Spec.Mon.ExternalMonIDs, externalID)
	if err := r.Update(ctx, s.cephCluster); err != nil {
		return err
	}

	return nil
}

func (r *RemoteArbiterReconciler) checkArbiterDeploymentUpToDate(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	lastAppliedVersion := s.arbiterKeyringSecret.Labels[RemoteArbiterResourceVersionLabel]
	if s.monitorKeyringSecret.ResourceVersion != lastAppliedVersion {
		s.log.Info("keyring secret is outdated")
		if err := r.updateArbiterKeyringSecret(ctx, s); err != nil {
			s.log.Error(err, "unable to update arbiter keyring secret")
			return err
		}
		s.outdated = true
		s.shouldRestart = true
	} else {
		s.log.Info("keyring secret is up to date")
	}

	lastAppliedVersion = s.arbiterEnvVarSecret.Labels[RemoteArbiterResourceVersionLabel]
	if s.monitorEnvVarSecret.ResourceVersion != lastAppliedVersion {
		s.log.Info("env var secret is outdated")
		if err := r.updateArbiterEnvVarSecret(ctx, s); err != nil {
			s.log.Error(err, "unable to update arbiter env var secret")
			return err
		}
		s.outdated = true
		s.shouldRestart = true
	} else {
		s.log.Info("env var secret is up to date")
	}

	lastAppliedVersion = s.arbiterOverrideConfigMap.Labels[RemoteArbiterResourceVersionLabel]
	if s.monitorOverrideConfigMap.ResourceVersion != lastAppliedVersion {
		s.log.Info("override configmap is outdated")
		if err := r.updateArbiterOverrideConfigMap(ctx, s); err != nil {
			s.log.Error(err, "unable to update arbiter override configmap")
			return err
		}
		s.outdated = true
		s.shouldRestart = true
	} else {
		s.log.Info("override configmap is up to date")
	}

	lastAppliedVersion = s.arbiterDeployment.Labels[RemoteArbiterResourceVersionLabel]
	if s.monitorDeployment.ResourceVersion != lastAppliedVersion {
		s.log.Info("deployment is outdated")
		if err := r.updateArbiterDeployment(ctx, s); err != nil {
			s.log.Error(err, "unable to update arbiter deployment")
			return err
		}
		s.outdated = true
		s.shouldRestart = false
	} else {
		s.log.Info("deployment is up to date")
	}

	s.log.Info("update decisions", "outdated", s.outdated, "should restart", s.shouldRestart)

	return nil
}

func (r *RemoteArbiterReconciler) updateArbiterKeyringSecret(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	s.arbiterKeyringSecret.Labels[RemoteArbiterResourceVersionLabel] = s.monitorKeyringSecret.ResourceVersion
	s.arbiterKeyringSecret.Data = s.monitorKeyringSecret.Data

	if err := s.remoteClusterClient.Update(ctx, s.arbiterKeyringSecret); err != nil {
		s.log.Error(err, "unable to update resource", SecretTypeName, s.arbiterKeyringSecretObjectKey)
		return err
	}

	return nil
}

func (r *RemoteArbiterReconciler) updateArbiterEnvVarSecret(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	s.arbiterEnvVarSecret.Labels[RemoteArbiterResourceVersionLabel] = s.monitorEnvVarSecret.ResourceVersion
	s.arbiterEnvVarSecret.Data = s.monitorEnvVarSecret.Data

	if err := s.remoteClusterClient.Update(ctx, s.arbiterEnvVarSecret); err != nil {
		s.log.Error(err, "unable to update resource", SecretTypeName, s.arbiterEnvVarSecretObjectKey)
		return err
	}

	return nil
}

func (r *RemoteArbiterReconciler) updateArbiterOverrideConfigMap(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	s.arbiterOverrideConfigMap.Labels[RemoteArbiterResourceVersionLabel] = s.monitorOverrideConfigMap.ResourceVersion
	s.arbiterOverrideConfigMap.Data = s.monitorOverrideConfigMap.Data

	if err := s.remoteClusterClient.Update(ctx, s.arbiterOverrideConfigMap); err != nil {
		s.log.Error(err, "unable to update resource", ConfigMapTypeName, s.arbiterOverrideConfigMapObjectKey)
		return err
	}

	return nil
}

func (r *RemoteArbiterReconciler) makeDeploymentSpec(s *RemoteArbiterReconcilationState) error {
	podLabels := map[string]string{
		RemoteArbiterLookupLabel: s.remoteArbiter.Name,
	}

	spec := s.monitorDeployment.Spec.DeepCopy()
	spec.Selector = &metav1.LabelSelector{
		MatchLabels: podLabels,
	}
	spec.Template.ObjectMeta = metav1.ObjectMeta{
		Labels: podLabels,
	}
	spec.Template.Spec.ServiceAccountName = ""
	spec.Template.Spec.DeprecatedServiceAccount = ""
	spec.Template.Spec.NodeSelector = s.remoteArbiter.Spec.Deployment.NodeSelector
	spec.Template.Spec.Affinity = s.remoteArbiter.Spec.Deployment.Affinity
	spec.Template.Spec.Resources = s.remoteArbiter.Spec.Deployment.Resources

	monID := s.remoteArbiter.Status.MonID
	volumes := spec.Template.Spec.Volumes
	volumesChanged := 0
	for idx := range volumes {
		switch volumes[idx].Name {
		case "rook-config-override":
			volumes[idx].Projected.Sources[0].ConfigMap.Name = s.arbiterOverrideConfigMap.Name
			volumesChanged++
		case "rook-ceph-mons-keyring":
			volumes[idx].Secret.SecretName = s.arbiterKeyringSecret.Name
			volumesChanged++
		case "ceph-daemon-data":
			volumes[idx].HostPath.Path = fmt.Sprintf("/var/lib/rook/mon-%s/data", monID)
			volumesChanged++
		}
		if volumesChanged == 3 {
			break
		}
	}

	monMapVolume := corev1.Volume{
		Name: RemoteArbiterMonMapVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	volumes = append(volumes, monMapVolume)
	spec.Template.Spec.Volumes = volumes

	r.modifyContainers(spec.Template.Spec.Containers, monID, s.arbiterEnvVarSecret.Name)
	r.modifyContainers(spec.Template.Spec.InitContainers, monID, s.arbiterEnvVarSecret.Name)

	var image string
	var fsid string
	for _, container := range spec.Template.Spec.Containers {
		if container.Name != "mon" {
			continue
		}
		image = container.Image

		fsidPrefix := "--fsid="
		for _, arg := range container.Args {
			if !strings.HasPrefix(arg, fsidPrefix) {
				continue
			}
			fsid = strings.TrimPrefix(arg, fsidPrefix)
			break
		}

		break
	}

	if fsid == "" {
		return errors.New("unable to determine fsid")
	}
	if image == "" {
		return errors.New("unable to determine image")
	}

	monMapInitContainer := r.getMonMapInitContainer(fsid, image, s.arbiterEnvVarSecret.Name)
	spec.Template.Spec.InitContainers = append([]corev1.Container{*monMapInitContainer}, spec.Template.Spec.InitContainers...)

	s.arbiterDeployment.Spec = *spec

	return nil
}

func (r *RemoteArbiterReconciler) modifyContainers(containers []corev1.Container, monID string, envVarSecretName string) {
	for containerIdx := range containers {
		volumeMounts := containers[containerIdx].VolumeMounts
		for volumeMountIdx := range volumeMounts {
			if volumeMounts[volumeMountIdx].Name == "ceph-daemon-data" {
				volumeMounts[volumeMountIdx].MountPath = fmt.Sprintf("/var/lib/ceph/mon/ceph-%s", monID)
				break
			}
		}

		monMapVolumeMount := corev1.VolumeMount{
			Name:      RemoteArbiterMonMapVolumeName,
			MountPath: RemoteArbiterMonMapMountPath,
		}
		volumeMounts = append(volumeMounts, monMapVolumeMount)
		containers[containerIdx].VolumeMounts = volumeMounts

		args := containers[containerIdx].Args
		for argIdx := range args {
			switch {
			case strings.HasPrefix(args[argIdx], "--id="):
				args[argIdx] = fmt.Sprintf("--id=%s", monID)
			case strings.HasPrefix(args[argIdx], "/var/lib/ceph/mon/ceph-"):
				args[argIdx] = fmt.Sprintf("/var/lib/ceph/mon/ceph-%s", monID)
			case strings.HasPrefix(args[argIdx], "--setuser-match-path="):
				args[argIdx] = fmt.Sprintf("--setuser-match-path=/var/lib/ceph/mon/ceph-%s/store.db", monID)
			case strings.HasPrefix(args[argIdx], "--public-addr="):
				args[argIdx] = "--public-addr=$(ROOK_POD_IP)"
			}
		}

		args = slices.DeleteFunc(args, func(arg string) bool {
			if strings.HasPrefix(arg, "--mon-host") {
				return true
			}
			if strings.HasPrefix(arg, "--mon-initial-members") {
				return true
			}
			return false
		})

		if containers[containerIdx].Name != "chown-container-data-dir" {
			args = append(args, "--monmap=/tmp/monmap/ext.monmap")
		}

		containers[containerIdx].Args = args

		hasPodIPEnvVar := false
		envVars := containers[containerIdx].Env
		for envVarIdx := range envVars {
			if envVars[envVarIdx].Name == "ROOK_POD_IP" {
				hasPodIPEnvVar = true
			}

			if envVars[envVarIdx].ValueFrom == nil {
				continue
			}
			if envVars[envVarIdx].ValueFrom.SecretKeyRef == nil {
				continue
			}
			envVars[envVarIdx].ValueFrom.SecretKeyRef.Name = envVarSecretName
		}

		if hasPodIPEnvVar {
			continue
		}
		podIPEnvVar := corev1.EnvVar{
			Name: "ROOK_POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "status.podIP",
				},
			},
		}
		envVars = append(envVars, podIPEnvVar)
		containers[containerIdx].Env = envVars
	}
}

func (r *RemoteArbiterReconciler) getMonMapInitContainer(fsid string, image string, envVarSecretName string) *corev1.Container {
	privileged := false
	container := &corev1.Container{
		Name:            "init-monmap",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"monmaptool"},
		Args: []string{
			"--create",
			"--clobber",
			"--set-initial-members",
			"--fsid",
			fsid,
			"--addv",
			"$(ROOK_CEPH_MON_INITIAL_MEMBERS)",
			"$(ROOK_CEPH_MON_HOST)",
			"/tmp/monmap/ext.monmap",
		},
		Env: []corev1.EnvVar{
			{
				Name: "ROOK_CEPH_MON_HOST",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key: "mon_host",
						LocalObjectReference: corev1.LocalObjectReference{
							Name: envVarSecretName,
						},
					},
				},
			},
			{
				Name: "ROOK_CEPH_MON_INITIAL_MEMBERS",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key: "mon_initial_members",
						LocalObjectReference: corev1.LocalObjectReference{
							Name: envVarSecretName,
						},
					},
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      RemoteArbiterMonMapVolumeName,
				MountPath: RemoteArbiterMonMapMountPath,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"NET_RAW"},
			},
			Privileged: &privileged,
		},
	}
	return container
}

func (r *RemoteArbiterReconciler) updateArbiterDeployment(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	if err := r.makeDeploymentSpec(s); err != nil {
		return err
	}

	s.arbiterDeployment.Labels[RemoteArbiterResourceVersionLabel] = s.monitorDeployment.ResourceVersion
	if s.arbiterDeployment.Spec.Template.Annotations == nil {
		s.arbiterDeployment.Spec.Template.Annotations = map[string]string{}
	}
	s.arbiterDeployment.Spec.Template.Annotations[RemoteArbiterRestartAnnotation] = time.Now().Format(time.RFC3339)

	if err := s.remoteClusterClient.Update(ctx, s.arbiterDeployment); err != nil {
		s.log.Error(err, "unable to update resource", ConfigMapTypeName, s.arbiterDeploymentObjectKey)
		return err
	}

	return nil
}

func (r *RemoteArbiterReconciler) restartArbiterDeployment(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	if s.arbiterDeployment.Spec.Template.Annotations == nil {
		s.arbiterDeployment.Spec.Template.Annotations = map[string]string{}
	}
	s.arbiterDeployment.Spec.Template.Annotations[RemoteArbiterRestartAnnotation] = time.Now().Format(time.RFC3339)

	if err := s.remoteClusterClient.Update(ctx, s.arbiterDeployment); err != nil {
		s.log.Error(err, "unable to update resource", ConfigMapTypeName, s.arbiterDeploymentObjectKey)
		return err
	}

	return nil
}

func (r *RemoteArbiterReconciler) fetchArbiterDeployment(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	deploymentList := &appsv1.DeploymentList{}
	// serviceList := &corev1.ServiceList{}
	keyringSecretList := &corev1.SecretList{}
	envVarSecretList := &corev1.SecretList{}
	overrideConfigMapList := &corev1.ConfigMapList{}

	namespaceSelector := client.InNamespace(s.remoteCluster.Spec.Namespace)
	arbiterLabelSelector := client.MatchingLabels{
		RemoteArbiterLookupLabel: s.remoteArbiter.Name,
	}
	keyringRoleLabelSelector := client.MatchingLabels{
		RemoteArbiterRoleLabel: RemoteArbiterKeyringRole,
	}
	envVarRoleLabelSelector := client.MatchingLabels{
		RemoteArbiterRoleLabel: RemoteArbiterEnvVarRole,
	}

	if err := s.remoteClusterClient.List(ctx, keyringSecretList, namespaceSelector, arbiterLabelSelector, keyringRoleLabelSelector); err != nil {
		return err
	}
	keyringSecretCount := len(keyringSecretList.Items)
	switch keyringSecretCount {
	case 0:
		if err := r.createArbiterKeyringSecret(ctx, s); err != nil {
			return err
		}
	case 1:
		s.arbiterKeyringSecret = &keyringSecretList.Items[0]
		s.arbiterKeyringSecretObjectKey = ObjectKeyFromObject(s.arbiterKeyringSecret)
	default:
		return fmt.Errorf("expected to get 1 keyring secret, but got %d", keyringSecretCount)
	}

	if err := s.remoteClusterClient.List(ctx, envVarSecretList, namespaceSelector, arbiterLabelSelector, envVarRoleLabelSelector); err != nil {
		return err
	}
	envVarSecretCount := len(envVarSecretList.Items)
	switch envVarSecretCount {
	case 0:
		if err := r.createArbiterEnvVarSecret(ctx, s); err != nil {
			return err
		}
	case 1:
		s.arbiterEnvVarSecret = &envVarSecretList.Items[0]
		s.arbiterEnvVarSecretObjectKey = ObjectKeyFromObject(s.arbiterEnvVarSecret)
	default:
		return fmt.Errorf("expected to get 1 env var secret, but got %d", envVarSecretCount)
	}

	if err := s.remoteClusterClient.List(ctx, overrideConfigMapList, namespaceSelector, arbiterLabelSelector); err != nil {
		return err
	}
	overrideConfigMapCount := len(overrideConfigMapList.Items)
	switch overrideConfigMapCount {
	case 0:
		if err := r.createArbiterOverrideConfigMap(ctx, s); err != nil {
			return err
		}
	case 1:
		s.arbiterOverrideConfigMap = &overrideConfigMapList.Items[0]
		s.arbiterOverrideConfigMapObjectKey = ObjectKeyFromObject(s.arbiterOverrideConfigMap)
	default:
		return fmt.Errorf("expected to get 1 override config, but got %d", overrideConfigMapCount)
	}

	// if err := s.remoteClusterClient.List(ctx, serviceList, namespaceSelector, arbiterLabelSelector); err != nil {
	// 	return err
	// }
	// arbiterServiceCount := len(serviceList.Items)
	// switch arbiterServiceCount {
	// case 0:
	// 	if err := r.createArbiterService(ctx, s); err != nil {
	// 		return err
	// 	}
	// case 1:
	// 	s.arbiterService = &serviceList.Items[0]
	// 	s.arbiterServiceObjectKey = ObjectKeyFromObject(s.arbiterService)
	// default:
	// 	return fmt.Errorf("expected to get 1 service, but got %d", arbiterServiceCount)
	// }

	if err := s.remoteClusterClient.List(ctx, deploymentList, namespaceSelector, arbiterLabelSelector); err != nil {
		return err
	}
	arbiterDeploymentCount := len(deploymentList.Items)
	switch arbiterDeploymentCount {
	case 0:
		if err := r.createArbiterDeployment(ctx, s); err != nil {
			return err
		}
	case 1:
		s.arbiterDeployment = &deploymentList.Items[0]
		s.arbiterDeploymentObjectKey = ObjectKeyFromObject(s.arbiterDeployment)
	default:
		return fmt.Errorf("expected to get 1 deployment, but got %d", arbiterDeploymentCount)
	}

	return nil
}

func (r *RemoteArbiterReconciler) createArbiterKeyringSecret(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	arbiterKeyringSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "arbiter-keyring-secret-",
			Namespace:    s.remoteCluster.Spec.Namespace,
			Labels: map[string]string{
				RemoteArbiterResourceVersionLabel: s.monitorKeyringSecret.ResourceVersion,
				RemoteArbiterLookupLabel:          s.remoteArbiter.Name,
				RemoteArbiterRoleLabel:            RemoteArbiterKeyringRole,
			},
			Finalizers: []string{RemoteArbiterFinalizer},
		},
		Data: s.monitorKeyringSecret.Data,
	}

	if err := s.remoteClusterClient.Create(ctx, arbiterKeyringSecret); err != nil {
		return err
	}

	s.arbiterKeyringSecret = arbiterKeyringSecret

	return nil
}

func (r *RemoteArbiterReconciler) createArbiterEnvVarSecret(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	arbiterEnvVarSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "arbiter-env-var-secret-",
			Namespace:    s.remoteCluster.Spec.Namespace,
			Labels: map[string]string{
				RemoteArbiterResourceVersionLabel: s.monitorEnvVarSecret.ResourceVersion,
				RemoteArbiterLookupLabel:          s.remoteArbiter.Name,
				RemoteArbiterRoleLabel:            RemoteArbiterEnvVarRole,
			},
			Finalizers: []string{RemoteArbiterFinalizer},
		},
		Data: s.monitorEnvVarSecret.Data,
	}

	if err := s.remoteClusterClient.Create(ctx, arbiterEnvVarSecret); err != nil {
		return err
	}

	s.arbiterEnvVarSecret = arbiterEnvVarSecret

	return nil
}

func (r *RemoteArbiterReconciler) createArbiterOverrideConfigMap(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	arbiterOverrideConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "arbiter-override-configmap-",
			Namespace:    s.remoteCluster.Spec.Namespace,
			Labels: map[string]string{
				RemoteArbiterResourceVersionLabel: s.monitorOverrideConfigMap.ResourceVersion,
				RemoteArbiterLookupLabel:          s.remoteArbiter.Name,
			},
			Finalizers: []string{RemoteArbiterFinalizer},
		},
		Data: s.monitorOverrideConfigMap.Data,
	}

	if err := s.remoteClusterClient.Create(ctx, arbiterOverrideConfigMap); err != nil {
		return err
	}

	s.arbiterOverrideConfigMap = arbiterOverrideConfigMap

	return nil
}

func (r *RemoteArbiterReconciler) createArbiterDeployment(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	s.arbiterDeployment = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "arbiter-deployment-",
			Namespace:    s.remoteCluster.Spec.Namespace,
			Labels: map[string]string{
				RemoteArbiterResourceVersionLabel: s.monitorOverrideConfigMap.ResourceVersion,
				RemoteArbiterLookupLabel:          s.remoteArbiter.Name,
			},
			Finalizers: []string{RemoteArbiterFinalizer},
		},
	}

	if err := r.makeDeploymentSpec(s); err != nil {
		return err
	}

	if err := s.remoteClusterClient.Create(ctx, s.arbiterDeployment); err != nil {
		return err
	}

	return nil
}

// func (r *RemoteArbiterReconciler) createArbiterService(ctx context.Context, s *RemoteArbiterReconcilationState) error {
// 	trafficPolicy := corev1.ServiceInternalTrafficPolicyCluster
// 	ipPolicy := corev1.IPFamilyPolicySingleStack

// 	s.arbiterService = &corev1.Service{
// 		ObjectMeta: metav1.ObjectMeta{
// 			GenerateName: "arbiter-service-",
// 			Namespace:    s.remoteCluster.Spec.Namespace,
// 			Labels: map[string]string{
// 				RemoteArbiterLookupLabel: s.remoteArbiter.Name,
// 			},
// 			Finalizers: []string{RemoteArbiterFinalizer},
// 		},
// 		Spec: corev1.ServiceSpec{
// 			Ports: []corev1.ServicePort{
// 				{
// 					Name:       "tcp-msgr1",
// 					Port:       6789,
// 					Protocol:   corev1.ProtocolTCP,
// 					TargetPort: intstr.FromInt32(6789),
// 				},
// 				{
// 					Name:       "tcp-msgr2",
// 					Port:       3300,
// 					Protocol:   corev1.ProtocolTCP,
// 					TargetPort: intstr.FromInt32(3300),
// 				},
// 			},
// 			Selector: map[string]string{
// 				RemoteArbiterLookupLabel: s.remoteArbiter.Name,
// 			},
// 			InternalTrafficPolicy: &trafficPolicy,
// 			IPFamilies:            []corev1.IPFamily{corev1.IPv4Protocol},
// 			IPFamilyPolicy:        &ipPolicy,
// 			SessionAffinity:       corev1.ServiceAffinityNone,
// 		},
// 	}

// 	if err := s.remoteClusterClient.Create(ctx, s.arbiterService); err != nil {
// 		return err
// 	}

// 	return nil
// }

func (r *RemoteArbiterReconciler) fetchMonitorDeployment(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	labelSelector := client.MatchingLabels{
		"ceph_daemon_type":          "mon",
		"app.kubernetes.io/part-of": s.remoteArbiter.Spec.CephCluster.Name,
	}
	namespaceSelector := client.InNamespace(s.remoteArbiter.Spec.CephCluster.Namespace)

	deploymentList := &appsv1.DeploymentList{}
	if err := r.List(ctx, deploymentList, labelSelector, namespaceSelector); err != nil {
		return err
	}

	if len(deploymentList.Items) == 0 {
		return errors.New("monitor deployments not found")
	}

	s.monitorDeployment = &deploymentList.Items[0]

	volumes := s.monitorDeployment.Spec.Template.Spec.Volumes

	keyringSecretVolumeName := "rook-ceph-mons-keyring"
	overrideConfigMapVolumeName := "rook-config-override"

	keyringSecretName := ""
	overrideConfigMapName := ""
	for _, volume := range volumes {
		if volume.Name == keyringSecretVolumeName {
			if volume.Secret == nil {
				return errors.New("keyring secret reference is nil")
			}
			keyringSecretName = volume.Secret.SecretName
			continue
		}
		if volume.Name == overrideConfigMapVolumeName {
			if volume.Projected == nil {
				return errors.New("override config volume reference is nil")
			}
			sourceCount := len(volume.Projected.Sources)
			if sourceCount != 1 {
				return fmt.Errorf("expected override config volume source to have len 1, got %d", sourceCount)
			}
			volumeSource := volume.Projected.Sources[0]
			if volumeSource.ConfigMap == nil {
				return errors.New("override config configmap reference is nil")
			}
			overrideConfigMapName = volumeSource.ConfigMap.Name
			continue
		}
		if keyringSecretName != "" && overrideConfigMapName != "" {
			break
		}
	}

	if keyringSecretName == "" {
		return errors.New("unable to find keyring secret volume")
	}
	if overrideConfigMapName == "" {
		return errors.New("unable to find override config secret volume")
	}

	keyringSecretObjectKey := types.NamespacedName{
		Name:      keyringSecretName,
		Namespace: s.monitorDeployment.Namespace,
	}
	keyringSecret := &corev1.Secret{}
	if err := r.Get(ctx, keyringSecretObjectKey, keyringSecret); err != nil {
		return err
	}

	s.monitorKeyringSecret = keyringSecret

	overrideConfigMapObjectKey := types.NamespacedName{
		Name:      overrideConfigMapName,
		Namespace: s.monitorDeployment.Namespace,
	}
	overrideConfigMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, overrideConfigMapObjectKey, overrideConfigMap); err != nil {
		return err
	}

	s.monitorOverrideConfigMap = overrideConfigMap

	containers := s.monitorDeployment.Spec.Template.Spec.Containers
	var monContainer *corev1.Container
	for _, container := range containers {
		if container.Name == "mon" {
			monContainer = &container
		}
	}

	if monContainer == nil {
		return errors.New("unable to find mon container in spec template")
	}

	envVarCount := len(monContainer.Env)
	if envVarCount == 0 {
		return errors.New("no env vars found")
	}

	monHostEnvVarName := "ROOK_CEPH_MON_HOST"
	envVarSecretName := ""
	for _, envVar := range monContainer.Env {
		if envVar.Name != monHostEnvVarName {
			continue
		}
		if envVar.ValueFrom == nil {
			return errors.New("env var value source is nil")
		}
		if envVar.ValueFrom.SecretKeyRef == nil {
			return errors.New("env var secret key ref is nil")
		}
		envVarSecretName = envVar.ValueFrom.SecretKeyRef.Name
	}

	if envVarSecretName == "" {
		return errors.New("unable to find env var secret")
	}

	envVarSecretObjectKey := types.NamespacedName{
		Name:      envVarSecretName,
		Namespace: s.monitorDeployment.Namespace,
	}
	envVarSecret := &corev1.Secret{}
	if err := r.Get(ctx, envVarSecretObjectKey, envVarSecret); err != nil {
		return err
	}

	s.monitorEnvVarSecret = envVarSecret

	return nil
}

func (r *RemoteArbiterReconciler) fetchCephCluster(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	s.cephClusterObjectKey = &types.NamespacedName{
		Name:      s.remoteArbiter.Spec.CephCluster.Name,
		Namespace: s.remoteArbiter.Spec.CephCluster.Namespace,
	}

	cephCluster := &rookv1.CephCluster{}

	if err := r.Get(ctx, *s.cephClusterObjectKey, cephCluster); err != nil {
		s.log.Error(err, "unable to get resource", "cephcluster", s.cephClusterObjectKey)
		return err
	}

	s.cephCluster = cephCluster

	return nil
}

func (r *RemoteArbiterReconciler) fetchArbiter(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	s.log.Info("requesting resource")
	remoteArbiter := &v1alpha1.RemoteArbiter{}
	err := r.Get(ctx, *s.remoteArbiterObjectKey, remoteArbiter)
	if err != nil {
		s.log.Error(err, "unable to get resource")
		return err
	}

	s.remoteArbiter = remoteArbiter

	return nil
}

func (r *RemoteArbiterReconciler) checkSelfFinalizer(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	if controllerutil.ContainsFinalizer(s.remoteArbiter, RemoteArbiterFinalizer) {
		s.log.Info("self finalizer exists, nothing to do")
		return nil
	}

	s.log.Info("adding finalizer on self")
	_ = controllerutil.AddFinalizer(s.remoteArbiter, RemoteArbiterFinalizer)
	if err := r.Update(ctx, s.remoteArbiter); err != nil {
		s.log.Error(err, "unable to update resource with finalizer")
		return err
	}

	return nil
}

func (r *RemoteArbiterReconciler) makeRemoteClient(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	s.remoteClusterSecretObjectKey = &types.NamespacedName{
		Name:      s.remoteCluster.Spec.AccessKeyRef.Name,
		Namespace: s.remoteCluster.Namespace,
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, *s.remoteClusterSecretObjectKey, secret); err != nil {
		s.log.Error(err, "unable to get secret", SecretTypeName, s.remoteClusterSecretObjectKey)
		return err
	}

	s.remoteClusterSecret = secret

	remoteKubeconfigBase64, ok := secret.Data[s.remoteCluster.Spec.AccessKeyRef.Key]
	if !ok {
		return fmt.Errorf("secret key %s not found", s.remoteCluster.Spec.AccessKeyRef.Key)
	}

	remoteRestConfig, err := clientcmd.RESTConfigFromKubeConfig(remoteKubeconfigBase64)
	if err != nil {
		s.log.Error(err, "unable to create rest config from secret", SecretTypeName, s.remoteClusterSecretObjectKey)
		return err
	}
	remoteRestConfig.Timeout = s.remoteCluster.Spec.Timeout.Duration

	remoteClient, err := client.New(remoteRestConfig, client.Options{})
	if err != nil {
		s.log.Error(err, "unable to create client from secret config", SecretTypeName, s.remoteClusterSecretObjectKey)
		return err
	}

	s.remoteClusterClient = client.NewNamespacedClient(remoteClient, s.remoteCluster.Spec.Namespace)

	return nil
}

func (r *RemoteArbiterReconciler) fetchRemoteCluster(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	remoteCluster, err := r.getRemoteCluster(ctx, s.remoteArbiter)
	if err != nil && !errors.Is(err, ErrorNotCreated) {
		s.log.Error(err, "unable to get remote cluster")
		return err
	}
	if remoteCluster != nil {
		s.remoteCluster = remoteCluster
		s.remoteClusterObjectKey = ObjectKeyFromObject(remoteCluster)
		s.log.Info("remote cluster found", RemoteClusterTypeName, s.remoteClusterObjectKey)

		added := controllerutil.AddFinalizer(s.remoteCluster, RemoteArbiterFinalizer)
		if !added {
			return nil
		}

		if err := r.Update(ctx, s.remoteCluster); err != nil {
			s.log.Error(err, "unable to update resource with finalizer")
			return err
		}

		return nil
	}
	s.log.Error(err, "remote cluster not found, will try to create")

	remoteCluster, err = r.createRemoteCluster(ctx, s.remoteArbiter)
	if err != nil {
		s.log.Error(err, "unable to create remote cluster")
		return err
	}

	s.remoteCluster = remoteCluster
	s.remoteClusterObjectKey = ObjectKeyFromObject(remoteCluster)
	s.log.Info("remote cluster created", RemoteClusterTypeName, s.remoteClusterObjectKey)

	return nil
}

func (r *RemoteArbiterReconciler) createRemoteCluster(ctx context.Context, remoteArbiter *v1alpha1.RemoteArbiter) (*v1alpha1.RemoteCluster, error) {
	if remoteArbiter.Spec.RemoteCluster.Spec == nil {
		return nil, errors.New("remote cluster spec is nil")
	}

	gvk, err := r.GroupVersionKindFor(remoteArbiter)
	if err != nil {
		return nil, err
	}
	ownerRef := metav1.NewControllerRef(remoteArbiter, gvk)

	remoteCluster := &v1alpha1.RemoteCluster{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "remote-cluster-",
			Namespace:    remoteArbiter.Namespace,
			Finalizers: []string{
				RemoteArbiterFinalizer,
			},
			OwnerReferences: []metav1.OwnerReference{
				*ownerRef,
			},
		},
		Spec: *remoteArbiter.Spec.RemoteCluster.Spec,
	}

	if err := r.Create(ctx, remoteCluster); err != nil {
		return nil, err
	}

	return remoteCluster, nil
}

func (r *RemoteArbiterReconciler) cleanUpRemoteArbiter(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	if err := r.updateRemoteArbiterState(ctx, s.remoteArbiter, v1alpha1.RemoteArbiterDeletingState, "deleting"); err != nil {
		s.log.Error(err, "unable to update remote arbiter state")
		return err
	}
	s.log.Info("delete state set")

	if updated := controllerutil.RemoveFinalizer(s.remoteArbiter, RemoteArbiterFinalizer); !updated {
		s.log.Info("no finalizer found, assuming cleaned up")
		return nil
	}
	s.log.Info("finalizer found, cleaning up remote cluster")

	if err := r.cleanUpRemoteCluster(ctx, s); err != nil {
		s.log.Error(err, "unable to clean up remote cluster")
		return err
	}
	s.log.Info("remote cluster cleaned up")

	if err := r.cleanUpCephCluster(ctx, s); err != nil {
		s.log.Error(err, "unable to clean up ceph cluster")
		return err
	}

	if err := r.Update(ctx, s.remoteArbiter); err != nil {
		s.log.Error(err, "unable to update resource after finalizer removal")
		return err
	}
	s.log.Info("resource updated, finalizer removed")

	return nil
}

func (r *RemoteArbiterReconciler) cleanUpCephCluster(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	if s.remoteArbiter.Status.MonID == "" {
		s.log.Info("remote arbiter mon id is not set, will not perform cleanup on ceph cluster")
		return nil
	}

	if err := r.fetchCephCluster(ctx, s); apierrors.IsNotFound(err) {
		s.log.Info("ceph cluster is not found, will not perform cleanup on ceph cluster")
		return nil
	} else if err != nil {
		s.log.Error(err, "unable to get remote cluster")
		return err
	}

	externalMonIDs := s.cephCluster.Spec.Mon.ExternalMonIDs
	idx := slices.Index(externalMonIDs, s.remoteArbiter.Status.MonID)
	if idx == -1 {
		s.log.Info("mon id is not found in ceph cluster configuration, will not perform cleanup on ceph cluster")
		return nil
	}

	s.cephCluster.Spec.Mon.ExternalMonIDs = slices.Delete(externalMonIDs, idx, idx+1)

	if err := r.Update(ctx, s.cephCluster); err != nil {
		s.log.Error(err, "unable to update ceph cluster")
		return err
	}

	return nil
}

func (r *RemoteArbiterReconciler) cleanUpRemoteCluster(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	remoteCluster, err := r.getRemoteCluster(ctx, s.remoteArbiter)
	if errors.Is(err, ErrorNotCreated) || apierrors.IsNotFound(err) {
		s.log.Info("remote cluster not found, assuming cleaned up")
		return nil
	} else if err != nil {
		s.log.Error(err, "unable to get remote cluster")
		return err
	}

	s.remoteCluster = remoteCluster
	s.remoteClusterObjectKey = ObjectKeyFromObject(s.remoteCluster)

	s.log.Info("remote cluster fetched", RemoteClusterTypeName, s.remoteClusterObjectKey)

	if updated := controllerutil.RemoveFinalizer(s.remoteCluster, RemoteArbiterFinalizer); !updated {
		s.log.Info("no finalizer found, assuming cleaned up", RemoteClusterTypeName, s.remoteClusterObjectKey)
		return nil
	}
	s.log.Info("finalizer found, cleaning up remote arbiter components", RemoteClusterTypeName, s.remoteClusterObjectKey)

	if err := r.cleanUpArbiterDeployment(ctx, s); err != nil {
		s.log.Error(err, "unable to clean up arbiter deployment components", RemoteClusterTypeName, s.remoteClusterObjectKey)
		return err
	}
	s.log.Info("arbiter deployment cleaned up", RemoteClusterTypeName, s.remoteClusterObjectKey)

	if err := r.Update(ctx, s.remoteCluster); err != nil {
		s.log.Error(err, "unable to update resource after finalizer removal")
		return err
	}
	s.log.Info("resource updated, finalizer removed", RemoteClusterTypeName, s.remoteClusterObjectKey)

	return nil
}

func (r *RemoteArbiterReconciler) cleanUpArbiterDeployment(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	if err := r.makeRemoteClient(ctx, s); err != nil {
		s.log.Error(err, "unable to make remote client, assume resource in bad health, will skip cluster cleanup", RemoteClusterTypeName, s.remoteClusterObjectKey)
		return nil
	}
	s.log.Info("remote client constructed, will request resource list in a bulk", RemoteClusterTypeName, s.remoteClusterObjectKey)

	namespaceSelector := client.InNamespace(s.remoteCluster.Spec.Namespace)
	labelSelector := client.MatchingLabels{
		RemoteArbiterLookupLabel: s.remoteArbiter.Name,
	}

	objectsToDelete := []client.Object{}
	clusterTypes := []client.Object{
		&corev1.Secret{},
		&corev1.ConfigMap{},
		&appsv1.Deployment{},
	}
	for _, clusterType := range clusterTypes {
		gvk, err := r.GroupVersionKindFor(clusterType)
		if err != nil {
			s.log.Error(err, "unable to get resource gvk", RemoteClusterTypeName, s.remoteClusterObjectKey)
			return err
		}

		objectList := &unstructured.UnstructuredList{}
		objectList.SetGroupVersionKind(gvk)
		err = r.List(ctx, objectList, namespaceSelector, labelSelector)
		if err != nil {
			s.log.Error(err, "unable to list remote resources", RemoteClusterTypeName, s.remoteClusterObjectKey)
			return err
		}

		for _, item := range objectList.Items {
			objectsToDelete = append(objectsToDelete, &item)
		}
	}

	for _, object := range objectsToDelete {
		objectKey := ObjectKeyFromObject(object)
		gvk, err := r.GroupVersionKindFor(object)
		if err != nil {
			s.log.Error(err, "unable to get resource gvk", RemoteClusterTypeName, s.remoteClusterObjectKey, "key", objectKey)
			return err
		}

		updated := controllerutil.RemoveFinalizer(object, RemoteArbiterFinalizer)
		if !updated {
			s.log.Info("resource has no finalizer, continue", RemoteClusterTypeName, s.remoteClusterObjectKey, gvk, objectKey)
			continue
		}

		if err := s.remoteClusterClient.Update(ctx, object); err != nil {
			s.log.Error(err, "unable to update resource after finalizer removal", RemoteClusterTypeName, s.remoteClusterObjectKey, gvk, objectKey)
			return err
		}

		if err := s.remoteClusterClient.Delete(ctx, object); err != nil {
			s.log.Error(err, "unable to delete resource after finalizer removal", RemoteClusterTypeName, s.remoteClusterObjectKey, gvk, objectKey)
			return err
		}
		s.log.Error(err, "remote resource is marked for deletion", RemoteClusterTypeName, s.remoteClusterObjectKey, gvk, objectKey)
	}

	s.log.Info("all remote resources are deleted", RemoteClusterTypeName, s.remoteClusterObjectKey)

	return nil
}

func (r *RemoteArbiterReconciler) checkStatusInitialized(ctx context.Context, s *RemoteArbiterReconcilationState) error {
	initialConditionsCount := len(s.remoteArbiter.Status.Conditions)
	conditionTypes := []string{
		v1alpha1.RemoteClusterExistsConditionType,
		v1alpha1.RemoteClusterReadyConditionType,
		v1alpha1.CephClusterExistsConditionType,
		v1alpha1.CephClusterReadyConditionType,
		v1alpha1.CephClusterConfiguredConditionType,
		v1alpha1.MonitorDeploymentExistsConditionType,
		v1alpha1.MonitorDeploymentReadyConditionType,
		v1alpha1.ArbiterDeploymentExistsConditionType,
		v1alpha1.ArbiterDeploymentReadyConditionType,
	}

	for _, conditionType := range conditionTypes {
		condition := NewInitCondition(conditionType, "init")
		set := r.setRemoteArbiterCondition(s.remoteArbiter, condition)
		if !set {
			s.log.Info("condition present, skipping", "condition", conditionType)
		} else {
			s.log.Info("condition not present, initializing", "condition", conditionType)
		}
	}

	if initialConditionsCount == len(s.remoteArbiter.Status.Conditions) {
		s.log.Info("all conditions present, nothing to update")
		return nil
	}

	s.log.Info("updating resource with init condition")
	if err := r.updateRemoteArbiterState(ctx, s.remoteArbiter, v1alpha1.RemoteArbiterInitState, "initialized"); err != nil {
		s.log.Error(err, "unable to update resource with conditions")
		return err
	}

	return nil
}

func (r *RemoteArbiterReconciler) getRemoteCluster(ctx context.Context, remoteArbiter *v1alpha1.RemoteArbiter) (*v1alpha1.RemoteCluster, error) {
	remoteClusterManagedByArbiter := remoteArbiter.Spec.RemoteCluster.Name == ""

	if remoteClusterManagedByArbiter {
		return r.getRemoteClusterByOwnerReference(ctx, remoteArbiter)
	}

	remoteClusterObjectKey := types.NamespacedName{
		Name:      remoteArbiter.Spec.RemoteCluster.Name,
		Namespace: remoteArbiter.Namespace,
	}
	return r.getRemoteClusterByName(ctx, remoteClusterObjectKey)
}

func (r *RemoteArbiterReconciler) getRemoteClusterByOwnerReference(ctx context.Context, remoteArbiter *v1alpha1.RemoteArbiter) (*v1alpha1.RemoteCluster, error) {
	remoteClusterList := &v1alpha1.RemoteClusterList{}
	if err := r.List(ctx, remoteClusterList, client.InNamespace(remoteArbiter.Namespace), client.MatchingFields{RemoteClusterOwnerKey: remoteArbiter.Name}); err != nil {
		return nil, err
	}

	if len(remoteClusterList.Items) == 0 {
		return nil, ErrorNotCreated
	}
	if len(remoteClusterList.Items) > 1 {
		return nil, errors.New("more than one remote cluster is linked to arbiter resource")
	}

	return &remoteClusterList.Items[0], nil
}

func (r *RemoteArbiterReconciler) getRemoteClusterByName(cxt context.Context, name types.NamespacedName) (*v1alpha1.RemoteCluster, error) {
	remoteCluster := &v1alpha1.RemoteCluster{}
	if err := r.Get(cxt, name, remoteCluster); err != nil {
		return nil, err
	}
	return remoteCluster, nil
}

func (r *RemoteArbiterReconciler) updateRemoteArbiterStatusOnFailure(
	ctx context.Context, remoteArbiter *v1alpha1.RemoteArbiter, conditionType string, err error) error {
	statusMessage := err.Error()
	stateSet := r.setRemoteArbiterState(remoteArbiter, v1alpha1.RemoteArbiterErrorState, statusMessage)
	condition := NewErrorCondition(conditionType, statusMessage)
	conditionSet := r.updateRemoteArbiterCondition(remoteArbiter, condition)
	if !stateSet && !conditionSet {
		return nil
	}
	if err := r.Status().Update(ctx, remoteArbiter); err != nil {
		return err
	}

	return nil
}

func (r *RemoteArbiterReconciler) updateRemoteArbiterStatusOnSuccess(
	ctx context.Context, remoteArbiter *v1alpha1.RemoteArbiter, state v1alpha1.RemoteArbiterState, conditionType string, statusMessage string) error {
	_ = r.setRemoteArbiterState(remoteArbiter, state, statusMessage)
	condition := NewOKCondition(conditionType, statusMessage)
	conditionSet := r.updateRemoteArbiterCondition(remoteArbiter, condition)
	if !conditionSet {
		return nil
	}
	if err := r.Status().Update(ctx, remoteArbiter); err != nil {
		return err
	}

	return nil
}

func (r *RemoteArbiterReconciler) updateRemoteArbiterState(ctx context.Context, remoteArbiter *v1alpha1.RemoteArbiter, state v1alpha1.RemoteArbiterState, message string) error {
	if set := r.setRemoteArbiterState(remoteArbiter, state, message); !set {
		return nil
	}
	if err := r.Status().Update(ctx, remoteArbiter); err != nil {
		return err
	}

	return nil
}

func (r *RemoteArbiterReconciler) setRemoteArbiterState(remoteArbiter *v1alpha1.RemoteArbiter, state v1alpha1.RemoteArbiterState, message string) bool {
	if remoteArbiter.Status.State == state && remoteArbiter.Status.Message == message {
		return false
	}

	remoteArbiter.Status.State = state
	remoteArbiter.Status.Message = message

	return true
}

func (r *RemoteArbiterReconciler) setRemoteArbiterCondition(remoteArbiter *v1alpha1.RemoteArbiter, condition metav1.Condition) bool {
	existingCondition := meta.FindStatusCondition(remoteArbiter.Status.Conditions, condition.Type)
	if existingCondition != nil {
		return false
	}

	_ = meta.SetStatusCondition(&remoteArbiter.Status.Conditions, condition)

	return true
}

func (r *RemoteArbiterReconciler) updateRemoteArbiterCondition(remoteArbiter *v1alpha1.RemoteArbiter, condition metav1.Condition) bool {
	if meta.IsStatusConditionPresentAndEqual(remoteArbiter.Status.Conditions, condition.Type, condition.Status) {
		return false
	}

	_ = meta.SetStatusCondition(&remoteArbiter.Status.Conditions, condition)

	return true
}

func (r *RemoteArbiterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1alpha1.RemoteCluster{}, RemoteClusterOwnerKey, func(rawObj client.Object) []string {
		remoteCluster := rawObj.(*v1alpha1.RemoteCluster)
		owner := metav1.GetControllerOf(remoteCluster)
		if owner == nil {
			return nil
		}
		if owner.APIVersion != v1alpha1.GroupVersion.String() || owner.Kind != "RemoteArbiter" {
			return nil
		}
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1alpha1.RemoteArbiter{}, RemoteArbiterCephClusterKey, func(rawObj client.Object) []string {
		remoteArbiter := rawObj.(*v1alpha1.RemoteArbiter)
		cephClusterRef := remoteArbiter.Spec.CephCluster.String()
		if cephClusterRef == "" {
			return nil
		}
		return []string{cephClusterRef}
	}); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1alpha1.RemoteArbiter{}, RemoteArbiterRemoteClusterKey, func(rawObj client.Object) []string {
		remoteArbiter := rawObj.(*v1alpha1.RemoteArbiter)
		remoteClusterName := remoteArbiter.Spec.RemoteCluster.Name
		if remoteClusterName == "" {
			return nil
		}
		return []string{remoteClusterName}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.RemoteArbiter{}).
		Named(RemoteArbiterTypeName).
		Owns(&v1alpha1.RemoteCluster{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findArbiterForCephClusterOwnedObject),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findArbiterForCephClusterOwnedObject),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(r.findArbiterForCephClusterOwnedObject),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&rookv1.CephCluster{},
			handler.EnqueueRequestsFromMapFunc(r.findArbiterForCephCluster),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&rookv1.CephCluster{},
			handler.EnqueueRequestsFromMapFunc(r.findArbiterForRemoteCluster),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

func (r *RemoteArbiterReconciler) findArbiterForCephClusterOwnedObject(ctx context.Context, obj client.Object) []reconcile.Request {
	owner := metav1.GetControllerOf(obj)
	if owner == nil {
		return nil
	}
	if owner.APIVersion != rookv1.SchemeGroupVersion.String() || owner.Kind != "CephCluster" {
		return nil
	}

	cephClusterRef := v1alpha1.NamespacedReference{
		Name:      owner.Name,
		Namespace: obj.GetNamespace(),
	}

	return r.findArbiterForCephClusterRef(ctx, cephClusterRef)
}

func (r *RemoteArbiterReconciler) findArbiterForCephCluster(ctx context.Context, obj client.Object) []reconcile.Request {
	cephClusterRef := v1alpha1.NamespacedReference{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}

	return r.findArbiterForCephClusterRef(ctx, cephClusterRef)
}

func (r *RemoteArbiterReconciler) findArbiterForCephClusterRef(ctx context.Context, ref v1alpha1.NamespacedReference) []reconcile.Request {
	selector := client.MatchingFields{RemoteArbiterCephClusterKey: ref.String()}

	arbiterList := &v1alpha1.RemoteArbiterList{}
	if err := r.List(ctx, arbiterList, selector, client.InNamespace(corev1.NamespaceAll)); err != nil {
		return nil
	}

	arbiterCount := len(arbiterList.Items)
	if arbiterCount == 0 {
		return nil
	}

	requests := make([]reconcile.Request, 0, arbiterCount)
	for _, item := range arbiterList.Items {
		request := reconcile.Request{
			NamespacedName: *ObjectKeyFromObject(&item),
		}
		requests = append(requests, request)
	}

	return requests
}

func (r *RemoteArbiterReconciler) findArbiterForRemoteCluster(ctx context.Context, obj client.Object) []reconcile.Request {
	owner := metav1.GetControllerOf(obj)
	if owner != nil {
		if owner.APIVersion != v1alpha1.GroupVersion.String() || owner.Kind != "RemoteArbiter" {
			return nil
		}

		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Name:      owner.Name,
					Namespace: obj.GetNamespace(),
				},
			},
		}
	}

	selector := client.MatchingFields{RemoteArbiterRemoteClusterKey: obj.GetName()}

	arbiterList := &v1alpha1.RemoteArbiterList{}
	if err := r.List(ctx, arbiterList, selector, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}

	arbiterCount := len(arbiterList.Items)
	if arbiterCount == 0 {
		return nil
	}

	requests := make([]reconcile.Request, 0, arbiterCount)
	for _, item := range arbiterList.Items {
		request := reconcile.Request{
			NamespacedName: *ObjectKeyFromObject(&item),
		}
		requests = append(requests, request)
	}

	return requests
}
