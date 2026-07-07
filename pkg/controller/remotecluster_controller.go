// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"
)

const (
	RemoteClusterFinalizer = "remotecluster.ceph.cobaltcore.sap.com/finalizer"

	RemoteClusterSecretKey = ".remotecluster.secret"
)

var (
	ErrorArbiterCleanUpNotComplete = errors.New("remote arbiter clean up is not complete")
)

type RemoteClusterReconcilationState struct {
	log                    *logr.Logger
	remoteClusterObjectKey *types.NamespacedName
	remoteCluster          *v1alpha1.RemoteCluster
	secretObjectKey        *types.NamespacedName
	secret                 *corev1.Secret
	remoteClient           *kubernetes.Clientset
}

// RemoteClusterReconciler reconciles a RemoteCluster object
type RemoteClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *RemoteClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).
		WithValues("reconcileID", uuid.NewUUID()).
		WithValues(RemoteClusterTypeName, req.NamespacedName)

	s := &RemoteClusterReconcilationState{
		log:                    &log,
		remoteClusterObjectKey: &req.NamespacedName,
	}

	if err := r.fetchCluster(ctx, s); apierrors.IsNotFound(err) {
		s.log.Info("resource not found, assuming it's gone")
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to fetch self: %w", err)
	}
	log.Info("self fetch complete")

	if s.remoteCluster.GetDeletionTimestamp() != nil {
		log.Info("deletion timestamp set, cleaning up")
		if err := r.cleanUpRemoteCluster(ctx, s); errors.Is(err, ErrorArbiterCleanUpNotComplete) {
			s.log.Info("remote arbiter clean up is not complete, will retry later")
			return ctrl.Result{RequeueAfter: time.Second * 5}, nil
		} else if err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to clean up: %w", err)
		}
		log.Info("clean up complete")
		return ctrl.Result{}, nil
	}
	log.Info("deletion check complete")

	if err := r.checkSelfFinalizer(ctx, s); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to check self finalizer: %w", err)
	}
	log.Info("finalizer check complete")

	if err := r.checkStatusInitialized(ctx, s); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to initialize conditions: %w", err)
	}
	log.Info("conditions check complete")

	if err := r.fetchSecret(ctx, s); err != nil {
		if statusErr := r.updateRemoteClusterStatusOnFailure(ctx, s.remoteCluster,
			v1alpha1.SecretAvailableConditionType, err); statusErr != nil {
			log.Error(err, "unable to fetch remote cluster")
			return ctrl.Result{}, fmt.Errorf("unable to update resource with conditions: %w", statusErr)
		}
		return ctrl.Result{}, fmt.Errorf("unable to fetch remote cluster: %w", err)
	}

	statusMessage := "secret is present"
	log.Info(statusMessage)

	if err := r.updateRemoteClusterStatusOnSuccess(ctx, s.remoteCluster,
		v1alpha1.RemoteClusterProgressingState, v1alpha1.SecretAvailableConditionType, statusMessage); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to update resource with conditions: %w", err)
	}

	if err := r.makeRemoteClient(s); err != nil {
		if statusErr := r.updateRemoteClusterStatusOnFailure(ctx, s.remoteCluster,
			v1alpha1.ConfigValidConditionType, err); statusErr != nil {
			log.Error(err, "unable to create remote client")
			return ctrl.Result{}, fmt.Errorf("unable to update resource with conditions: %w", statusErr)
		}
		return ctrl.Result{}, fmt.Errorf("unable to create remote client: %w", err)
	}

	statusMessage = "client initialized, config is valid"
	log.Info(statusMessage)

	if err := r.updateRemoteClusterStatusOnSuccess(ctx, s.remoteCluster,
		v1alpha1.RemoteClusterProgressingState, v1alpha1.ConfigValidConditionType, statusMessage); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to update resource with conditions: %w", err)
	}

	if err := r.checkClusterReady(ctx, s); err != nil {
		if statusErr := r.updateRemoteClusterStatusOnFailure(ctx, s.remoteCluster,
			v1alpha1.ClusterReachableConditionType, err); statusErr != nil {
			log.Error(err, "unable to check cluster ready")
			return ctrl.Result{}, fmt.Errorf("unable to update resource with conditions: %w", statusErr)
		}
		return ctrl.Result{}, fmt.Errorf("unable to check cluster ready: %w", err)
	}

	statusMessage = "cluster is ready"
	log.Info(statusMessage)

	if err := r.updateRemoteClusterStatusOnSuccess(ctx, s.remoteCluster,
		v1alpha1.RemoteClusterProgressingState, v1alpha1.ClusterReachableConditionType, statusMessage); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to update resource with conditions: %w", err)
	}

	if err := r.validatePermissions(ctx, s); err != nil {
		if statusErr := r.updateRemoteClusterStatusOnFailure(ctx, s.remoteCluster,
			v1alpha1.HasEnoughPermissionsConditionType, err); statusErr != nil {
			log.Error(err, "unable to check permissions")
			return ctrl.Result{}, fmt.Errorf("unable to update resource with conditions: %w", statusErr)
		}
		return ctrl.Result{}, fmt.Errorf("unable to check permissions: %w", err)
	}

	statusMessage = "cluster is ready and configured"
	log.Info(statusMessage)

	if err := r.updateRemoteClusterStatusOnSuccess(ctx, s.remoteCluster,
		v1alpha1.RemoteClusterReadyState, v1alpha1.HasEnoughPermissionsConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}
	log.Info("reconcile finished")

	return ctrl.Result{RequeueAfter: s.remoteCluster.Spec.CheckInterval.Duration}, nil
}

func (r *RemoteClusterReconciler) fetchCluster(ctx context.Context, s *RemoteClusterReconcilationState) error {
	s.log.Info("requesting resource")
	remoteCluster := &v1alpha1.RemoteCluster{}
	err := r.Get(ctx, *s.remoteClusterObjectKey, remoteCluster)
	if err != nil {
		return fmt.Errorf("unable to get resource: %w", err)
	}

	s.remoteCluster = remoteCluster

	return nil
}

func (r *RemoteClusterReconciler) cleanUpRemoteCluster(ctx context.Context, s *RemoteClusterReconcilationState) error {
	if err := r.updateRemoteClusterState(ctx, s.remoteCluster, v1alpha1.RemoteClusterDeletingState, "deleting"); err != nil {
		return fmt.Errorf("unable to update remote cluster state: %w", err)
	}
	s.log.Info("delete state set")

	if controllerutil.ContainsFinalizer(s.remoteCluster, RemoteArbiterFinalizer) {
		return ErrorArbiterCleanUpNotComplete
	}

	if updated := controllerutil.RemoveFinalizer(s.remoteCluster, RemoteClusterFinalizer); !updated {
		s.log.Info("no finalizer found, assuming cleaned up")
		return nil
	}
	s.log.Info("finalizer found, cleaning up secret")

	if err := r.cleanUpSecret(ctx, s); err != nil {
		return fmt.Errorf("unable to clean up secret: %w", err)
	}
	s.log.Info("secret cleaned up")

	if err := r.Update(ctx, s.remoteCluster); err != nil {
		return fmt.Errorf("unable to update %s %s after finalizer removal: %w", RemoteClusterTypeName, s.remoteClusterObjectKey, err)
	}
	s.log.Info("resource updated, finalizer removed")

	return nil
}

func (r *RemoteClusterReconciler) cleanUpSecret(ctx context.Context, s *RemoteClusterReconcilationState) error {
	if err := r.getSecret(ctx, s); apierrors.IsNotFound(err) {
		s.log.Info("referred resource not found, assuming it's gone", SecretTypeName, s.secretObjectKey)
		return nil
	} else if err != nil {
		return fmt.Errorf("unable to get %s %s to remove finalizer: %w", SecretTypeName, s.secretObjectKey, err)
	}

	if updated := controllerutil.RemoveFinalizer(s.secret, RemoteClusterFinalizer); !updated {
		s.log.Info("no finalizer on resource, proceeding", SecretTypeName, s.secretObjectKey)
		return nil
	}

	if err := r.Update(ctx, s.secret); err != nil {
		return fmt.Errorf("unable to update %s %s after finalizer removal: %w", SecretTypeName, s.secretObjectKey, err)
	}

	s.log.Info("finalizer removed, proceeding", SecretTypeName, s.secretObjectKey)

	return nil
}

func (r *RemoteClusterReconciler) getSecret(ctx context.Context, s *RemoteClusterReconcilationState) error {
	s.secretObjectKey = &types.NamespacedName{
		Name:      s.remoteCluster.Spec.AccessKeyRef.Name,
		Namespace: s.remoteCluster.Namespace,
	}

	secret := &corev1.Secret{}

	if err := r.Get(ctx, *s.secretObjectKey, secret); err != nil {
		return fmt.Errorf("unable to get resource: %w", err)
	}

	s.secret = secret

	return nil
}

func (r *RemoteClusterReconciler) checkSelfFinalizer(ctx context.Context, s *RemoteClusterReconcilationState) error {
	if updated := controllerutil.AddFinalizer(s.remoteCluster, RemoteClusterFinalizer); !updated {
		s.log.Info("self finalizer exists, nothing to do")
		return nil
	}
	s.log.Info("adding finalizer on self")

	if err := r.Update(ctx, s.remoteCluster); err != nil {
		return fmt.Errorf("unable to update resource with finalizer: %w", err)
	}

	return nil
}

func (r *RemoteClusterReconciler) checkStatusInitialized(ctx context.Context, s *RemoteClusterReconcilationState) error {
	initialConditionsCount := len(s.remoteCluster.Status.Conditions)
	conditionTypes := []string{
		v1alpha1.SecretAvailableConditionType,
		v1alpha1.ConfigValidConditionType,
		v1alpha1.ClusterReachableConditionType,
		v1alpha1.HasEnoughPermissionsConditionType,
	}

	for _, conditionType := range conditionTypes {
		condition := NewInitCondition(conditionType, "init")
		set := r.setRemoteClusterCondition(s.remoteCluster, condition)
		if !set {
			s.log.Info("condition present, skipping", "condition", conditionType)
		} else {
			s.log.Info("condition not present, initializing", "condition", conditionType)
		}
	}

	if initialConditionsCount == len(s.remoteCluster.Status.Conditions) {
		s.log.Info("all conditions present, nothing to update")
		return nil
	}

	s.log.Info("updating resource with init condition")
	if err := r.updateRemoteClusterState(ctx, s.remoteCluster, v1alpha1.RemoteClusterInitState, "initialized"); err != nil {
		return fmt.Errorf("unable to update resource with conditions: %w", err)
	}

	return nil
}

func (r RemoteClusterReconciler) fetchSecret(ctx context.Context, s *RemoteClusterReconcilationState) error {
	if err := r.getSecret(ctx, s); err != nil {
		return fmt.Errorf("unable to get secret: %w", err)
	}
	s.log.Info("resource found", SecretTypeName, s.secretObjectKey)

	if added := controllerutil.AddFinalizer(s.secret, RemoteClusterFinalizer); !added {
		s.log.Info("finalizer present", SecretTypeName, s.secretObjectKey)
		return nil
	}
	s.log.Info("adding finalizer", SecretTypeName, s.secretObjectKey)

	if err := r.Update(ctx, s.secret); err != nil {
		return fmt.Errorf("unable to update resource with finalizer: %w", err)
	}
	s.log.Info("finalizer added", SecretTypeName, s.secretObjectKey)

	return nil
}

func (r *RemoteClusterReconciler) makeRemoteClient(s *RemoteClusterReconcilationState) error {
	remoteKubeconfigBase64, ok := s.secret.Data[s.remoteCluster.Spec.AccessKeyRef.Key]
	if !ok {
		return fmt.Errorf("secret key %s not found", s.remoteCluster.Spec.AccessKeyRef.Key)
	}

	remoteRestConfig, err := clientcmd.RESTConfigFromKubeConfig(remoteKubeconfigBase64)
	if err != nil {
		return fmt.Errorf("unable to create rest config from %s %s: %w", SecretTypeName, s.secretObjectKey, err)
	}
	remoteRestConfig.Timeout = s.remoteCluster.Spec.Timeout.Duration

	remoteClient, err := kubernetes.NewForConfig(remoteRestConfig)
	if err != nil {
		return fmt.Errorf("unable to create client from %s %s: %w", SecretTypeName, s.secretObjectKey, err)
	}

	s.remoteClient = remoteClient

	return nil
}

func (r *RemoteClusterReconciler) checkClusterReady(ctx context.Context, s *RemoteClusterReconcilationState) error {
	readinessResponseBytes, err := s.remoteClient.RESTClient().Get().AbsPath("readyz").Do(ctx).Raw()
	if err != nil {
		return fmt.Errorf("unable to check cluster readiness: %w", err)
	}

	readinessResponse := string(readinessResponseBytes)
	if readinessResponse != "ok" {
		return fmt.Errorf("unable to validate cluster readiness, response is %s: %w", readinessResponse, err)
	}

	return nil
}

func (r *RemoteClusterReconciler) getPermissionReviewRequests(namespace string) []*authorizationv1.SelfSubjectAccessReview {
	verbs := []string{"create", "delete", "get", "list", "patch", "update", "watch"}
	reviewRequestTemplates := []*authorizationv1.SelfSubjectAccessReview{
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: namespace,
					Resource:  "deployments",
					Group:     "apps",
					Version:   "v1",
				},
			},
		},
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: namespace,
					Resource:  "secrets",
					Group:     "",
					Version:   "v1",
				},
			},
		},
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: namespace,
					Resource:  "configmaps",
					Group:     "",
					Version:   "v1",
				},
			},
		},
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: namespace,
					Resource:  "services",
					Group:     "",
					Version:   "v1",
				},
			},
		},
	}

	reviewRequests := make([]*authorizationv1.SelfSubjectAccessReview, 0, len(reviewRequestTemplates)*(2*len(verbs)+1))
	for _, reviewRequestTemplate := range reviewRequestTemplates {
		for _, verb := range verbs {
			resourceReviewRequest := reviewRequestTemplate.DeepCopy()
			resourceReviewRequest.Spec.ResourceAttributes.Verb = verb

			statusReviewRequest := reviewRequestTemplate.DeepCopy()
			statusReviewRequest.Spec.ResourceAttributes.Verb = verb
			statusReviewRequest.Spec.ResourceAttributes.Subresource = "status"

			reviewRequests = append(reviewRequests, resourceReviewRequest, statusReviewRequest)
		}

		finalizerReviewRequest := reviewRequestTemplate.DeepCopy()
		finalizerReviewRequest.Spec.ResourceAttributes.Verb = "update"
		finalizerReviewRequest.Spec.ResourceAttributes.Subresource = "finalizers"
		reviewRequests = append(reviewRequests, finalizerReviewRequest)
	}

	return reviewRequests
}

func (r *RemoteClusterReconciler) validatePermissions(ctx context.Context, s *RemoteClusterReconcilationState) error {
	permissionsReviewRequests := r.getPermissionReviewRequests(s.remoteCluster.Spec.Namespace)

	permissionEvalErrors := make([]error, 0)
	for _, permissionsReviewRequest := range permissionsReviewRequests {
		resp, err := s.remoteClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, permissionsReviewRequest, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("unable to perform permission evaluation: %w", err)
		}

		if resp.Status.Allowed {
			continue
		}

		denyReason := resp.Status.Reason
		evalFailureReason := resp.Status.EvaluationError
		errCount := len(permissionEvalErrors)
		if len(denyReason) > 0 {
			err := errors.New(denyReason)
			permissionEvalErrors = append(permissionEvalErrors, err)
			s.log.Error(err, "access denied")
		}
		if len(evalFailureReason) > 0 {
			err := errors.New(evalFailureReason)
			permissionEvalErrors = append(permissionEvalErrors, err)
			s.log.Error(err, "evaluation failed")
		}
		if errCount == len(permissionEvalErrors) {
			err := fmt.Errorf("not enough rights for %s", permissionsReviewRequest.Spec.ResourceAttributes.String())
			permissionEvalErrors = append(permissionEvalErrors, err)
			s.log.Error(err, "access denied")
		}
	}

	if len(permissionEvalErrors) == 0 {
		return nil
	}

	err := errors.Join(permissionEvalErrors...)

	return fmt.Errorf("unable to validate permissions: %w", err)
}

func (r *RemoteClusterReconciler) updateRemoteClusterStatusOnFailure(
	ctx context.Context, remoteCluster *v1alpha1.RemoteCluster, conditionType string, err error) error {
	statusMessage := err.Error()
	stateSet := r.setRemoteClusterState(remoteCluster, v1alpha1.RemoteClusterErrorState, statusMessage)
	condition := NewErrorCondition(conditionType, statusMessage)
	conditionSet := r.updateRemoteClusterCondition(remoteCluster, condition)
	if !stateSet && !conditionSet {
		return nil
	}
	if err := r.Status().Update(ctx, remoteCluster); err != nil {
		return err
	}

	return nil
}

func (r *RemoteClusterReconciler) updateRemoteClusterStatusOnSuccess(
	ctx context.Context, remoteCluster *v1alpha1.RemoteCluster, state v1alpha1.RemoteClusterState, conditionType string, statusMessage string) error {
	_ = r.setRemoteClusterState(remoteCluster, state, statusMessage)
	condition := NewOKCondition(conditionType, statusMessage)
	if conditionSet := r.updateRemoteClusterCondition(remoteCluster, condition); !conditionSet {
		return nil
	}
	if err := r.Status().Update(ctx, remoteCluster); err != nil {
		return err
	}

	return nil
}

func (r *RemoteClusterReconciler) updateRemoteClusterState(
	ctx context.Context, remoteCluster *v1alpha1.RemoteCluster, state v1alpha1.RemoteClusterState, statusMessage string) error {
	if set := r.setRemoteClusterState(remoteCluster, state, statusMessage); !set {
		return nil
	}
	if err := r.Status().Update(ctx, remoteCluster); err != nil {
		return fmt.Errorf("unable to update status: %s", err)
	}

	return nil
}

func (r *RemoteClusterReconciler) setRemoteClusterState(remoteCluster *v1alpha1.RemoteCluster, state v1alpha1.RemoteClusterState, message string) bool {
	if remoteCluster.Status.State == state && remoteCluster.Status.Message == message {
		return false
	}

	remoteCluster.Status.State = state
	remoteCluster.Status.Message = message

	return true
}

func (r *RemoteClusterReconciler) setRemoteClusterCondition(remoteCluster *v1alpha1.RemoteCluster, condition metav1.Condition) bool {
	existingCondition := meta.FindStatusCondition(remoteCluster.Status.Conditions, condition.Type)
	if existingCondition != nil {
		return false
	}

	_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)

	return true
}

func (r *RemoteClusterReconciler) updateRemoteClusterCondition(remoteCluster *v1alpha1.RemoteCluster, condition metav1.Condition) bool {
	if meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
		return false
	}

	_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)

	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *RemoteClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1alpha1.RemoteCluster{}, RemoteClusterSecretKey, func(rawObj client.Object) []string {
		remoteCluster := rawObj.(*v1alpha1.RemoteCluster)
		secretName := remoteCluster.Spec.AccessKeyRef.Name
		if secretName == "" {
			return nil
		}
		return []string{secretName}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.RemoteCluster{}).
		Named(RemoteClusterTypeName).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findClusterForSecret),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

func (r *RemoteClusterReconciler) findClusterForSecret(ctx context.Context, object client.Object) []reconcile.Request {
	secret, ok := object.(*corev1.Secret)
	if !ok {
		return nil
	}

	remoteClusterList := &v1alpha1.RemoteClusterList{}
	if err := r.List(ctx, remoteClusterList, client.InNamespace(secret.Namespace), client.MatchingFields{RemoteClusterSecretKey: secret.Name}); err != nil {
		return nil
	}

	itemCount := len(remoteClusterList.Items)
	if itemCount == 0 {
		return nil
	}

	requests := make([]reconcile.Request, 0, itemCount)
	for _, remoteCluster := range remoteClusterList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&remoteCluster),
		})
	}

	return requests
}
