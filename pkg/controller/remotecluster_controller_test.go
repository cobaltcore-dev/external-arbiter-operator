// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"
)

var _ = Describe("RemoteCluster Controller", func() {
	Context("When secret is missing", func ()  {
		missingSecretName := "missing-secret"

		remoteClusterNamespacedName := types.NamespacedName{
			Name:      "remote-cluster",
			Namespace: "default",
		}

		conditionsMap := map[string]metav1.ConditionStatus{
			v1alpha1.ConfigAvailableConditionType: metav1.ConditionFalse,
			v1alpha1.ConvigValidConditionType: metav1.ConditionUnknown,
			v1alpha1.ClusterReachableConditionType: metav1.ConditionUnknown,
			v1alpha1.HasEnoughPermissionsConditionType: metav1.ConditionUnknown,
		}

		BeforeEach(func() {
			remoteCluster := &v1alpha1.RemoteCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: remoteClusterNamespacedName.Name,
					Namespace: remoteClusterNamespacedName.Namespace,
				},
				Spec: v1alpha1.RemoteClusterSpec{
					AccessKeyRef: v1alpha1.KubeconfigSecretSource{
						Name: missingSecretName,
						Key: "kubeconfig.yaml",
					},
				},
			}
			err := sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func ()  {
			remoteCluster := &v1alpha1.RemoteCluster{}
			err := sourceK8sClient.Get(ctx, remoteClusterNamespacedName, remoteCluster)
			Expect(err).NotTo(HaveOccurred())
			err = sourceK8sClient.Delete(ctx, remoteCluster)
			Expect(err).To(Succeed())
		})

		It("Should fail to get secret", func() {
			remoteClusterReconciler := &RemoteClusterReconciler{
				Client: sourceK8sClient,
				Scheme: sourceK8sClient.Scheme(),
			}

			_, err := remoteClusterReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: remoteClusterNamespacedName,
			})
			Expect(err).To(HaveOccurred())

			remoteCluster := &v1alpha1.RemoteCluster{}
			err = sourceK8sClient.Get(ctx, remoteClusterNamespacedName, remoteCluster)
			Expect(err).NotTo(HaveOccurred())
			

			Expect(len(remoteCluster.Status.Conditions)).To(Equal(len(conditionsMap)))
			for _, condition := range remoteCluster.Status.Conditions {
				expectedCondition := conditionsMap[condition.Type]
				Expect(condition.Status).To(Equal(expectedCondition))
				if expectedCondition == metav1.ConditionFalse {
					Expect(condition.Message).NotTo(BeEmpty())
				}
			}
		})
	})

	Context("When secret is malformed", func ()  {
		It("Should fail to gparseet secret", func() {
		})
	})

	Context("When cluster is not accessible", func ()  {
		It("Should fail to check readiness", func() {
		})
	})

	Context("When user has not enough permissions", func ()  {
		It("Should fail to validate permissions", func() {
		})
	})

	Context("When cluster in properly setup", func ()  {
		It("Should be ready", func() {
		})
	})

	Context("When reconciling a resource", func() {

		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		remoteCluster := &v1alpha1.RemoteCluster{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind RemoteCluster")
			err := sourceK8sClient.Get(ctx, typeNamespacedName, remoteCluster)
			if err != nil && errors.IsNotFound(err) {
				resource := &v1alpha1.RemoteCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: v1alpha1.RemoteClusterSpec{
						AccessKeyRef: v1alpha1.KubeconfigSecretSource{
							Name: "secret",
							Key:  "kubeconfig.yaml",
						},
					},
				}
				Expect(sourceK8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &v1alpha1.RemoteCluster{}
			err := sourceK8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance RemoteCluster")
			Expect(sourceK8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &RemoteClusterReconciler{
				Client: sourceK8sClient,
				Scheme: sourceK8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})
