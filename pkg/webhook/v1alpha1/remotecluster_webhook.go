// Copyright 2026 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"
)

const (
	DefaultKubeconfigSecretKey = "kubeconfig.yaml"
	DefaultNamespace           = "default"
)

var remoteclusterlog = logf.Log.WithName("remotecluster-resource")

func SetupRemoteClusterWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &v1alpha1.RemoteCluster{}).
		WithValidator(&RemoteClusterCustomValidator{}).
		WithDefaulter(&RemoteClusterCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-ceph-cobaltcore-sap-com-v1alpha1-remotecluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=ceph.cobaltcore.sap.com,resources=remoteclusters,verbs=create;update,versions=v1alpha1,name=mremotecluster-v1alpha1.kb.io,admissionReviewVersions=v1

type RemoteClusterCustomDefaulter struct{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind RemoteCluster.
func (r *RemoteClusterCustomDefaulter) Default(_ context.Context, remoteCluster *v1alpha1.RemoteCluster) error {
	remoteclusterlog.Info("Defaulting for RemoteCluster", "name", remoteCluster.GetName())

	setRemoteClusterSpecDefaults(&remoteCluster.Spec, remoteCluster.Name)

	return nil
}

func setRemoteClusterSpecDefaults(remoteClusterSpec *v1alpha1.RemoteClusterSpec, accessKeyRefName string) {
	if remoteClusterSpec.CheckInterval == nil {
		remoteClusterSpec.CheckInterval = &v1alpha1.Interval{
			Duration: time.Minute,
		}
	}
	if remoteClusterSpec.Timeout == nil {
		remoteClusterSpec.Timeout = &v1alpha1.Interval{
			Duration: time.Second * 10,
		}
	}
	if remoteClusterSpec.AccessKeyRef.Key == "" {
		remoteClusterSpec.AccessKeyRef.Key = DefaultKubeconfigSecretKey
	}
	if remoteClusterSpec.AccessKeyRef.Name == "" {
		remoteClusterSpec.AccessKeyRef.Name = accessKeyRefName
	}
	if remoteClusterSpec.Namespace == "" {
		remoteClusterSpec.Namespace = DefaultNamespace
	}
}

// +kubebuilder:webhook:path=/validate-ceph-cobaltcore-sap-com-v1alpha1-remotecluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=ceph.cobaltcore.sap.com,resources=remoteclusters,verbs=create;update,versions=v1alpha1,name=vremotecluster-v1alpha1.kb.io,admissionReviewVersions=v1

type RemoteClusterCustomValidator struct{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type RemoteCluster.
func (r *RemoteClusterCustomValidator) ValidateCreate(_ context.Context, remoteCluster *v1alpha1.RemoteCluster) (admission.Warnings, error) {
	remoteclusterlog.Info("Validation for RemoteCluster upon creation", "name", remoteCluster.GetName())
	return r.validate(remoteCluster)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type RemoteCluster.
func (r *RemoteClusterCustomValidator) ValidateUpdate(_ context.Context, oldObj, remoteCluster *v1alpha1.RemoteCluster) (admission.Warnings, error) {
	remoteclusterlog.Info("Validation for RemoteCluster upon update", "name", remoteCluster.GetName())
	return r.validate(remoteCluster)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type RemoteCluster.
func (r *RemoteClusterCustomValidator) ValidateDelete(_ context.Context, remoteCluster *v1alpha1.RemoteCluster) (admission.Warnings, error) {
	remoteclusterlog.Info("Validation for RemoteCluster upon deletion", "name", remoteCluster.GetName())

	return r.validate(remoteCluster)
}

func (r *RemoteClusterCustomValidator) validate(remoteCluster *v1alpha1.RemoteCluster) (admission.Warnings, error) {
	rootPath := field.NewPath("spec")
	validationErrors := validateRemoteClusterSpec(&remoteCluster.Spec, rootPath)

	if len(validationErrors) > 0 {
		gvk := remoteCluster.GroupVersionKind()
		gk := schema.GroupKind{
			Group: gvk.Group,
			Kind:  gvk.Kind,
		}
		return nil, apierrors.NewInvalid(gk, remoteCluster.Name, validationErrors)
	}

	return nil, nil
}

func validateRemoteClusterSpec(remoteClusterSpec *v1alpha1.RemoteClusterSpec, rootPath *field.Path) field.ErrorList {
	var validationErrors field.ErrorList
	if remoteClusterSpec.CheckInterval == nil {
		validationErrors = append(validationErrors, field.Invalid(
			rootPath.Child("checkInterval"), remoteClusterSpec.CheckInterval, "check interval should not be empty"))
	} else if remoteClusterSpec.CheckInterval.Duration < 0 {
		validationErrors = append(validationErrors, field.Invalid(
			rootPath.Child("checkInterval"), remoteClusterSpec.CheckInterval, "check interval should not be negative"))
	}

	if remoteClusterSpec.Timeout == nil {
		validationErrors = append(validationErrors, field.Invalid(
			rootPath.Child("timeout"), remoteClusterSpec.Timeout, "timeout should not be empty"))
	} else if remoteClusterSpec.Timeout.Duration < 0 {
		validationErrors = append(validationErrors, field.Invalid(
			rootPath.Child("timeout"), remoteClusterSpec.Timeout, "timeout should not be negative"))
	}

	errMsgs := validation.NameIsDNSLabel(remoteClusterSpec.Namespace, false)
	if len(errMsgs) != 0 {
		for _, errMsg := range errMsgs {
			validationErrors = append(validationErrors, field.Invalid(
				rootPath.Child("namespace"), remoteClusterSpec.Namespace, errMsg))
		}
	}

	errMsgs = validation.NameIsDNSLabel(remoteClusterSpec.AccessKeyRef.Name, false)
	if len(errMsgs) != 0 {
		for _, errMsg := range errMsgs {
			validationErrors = append(validationErrors, field.Invalid(
				rootPath.Child("accessKeyRef", "name"), remoteClusterSpec.AccessKeyRef.Name, errMsg))
		}
	}

	return validationErrors
}
