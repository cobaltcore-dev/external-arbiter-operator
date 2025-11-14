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

var _ = Describe("RemoteArbiter Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		remoteArbiter := &v1alpha1.RemoteArbiter{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind RemoteArbiter")
			err := sourceK8sClient.Get(ctx, typeNamespacedName, remoteArbiter)
			if err != nil && errors.IsNotFound(err) {
				resource := &v1alpha1.RemoteArbiter{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: v1alpha1.RemoteArbiterSpec{
						RemoteCluster: v1alpha1.RemoteClusterSpec{
							AccessKeyRef: v1alpha1.KubeconfigSecretSource{
								Name: "secret",
								Key:  "kubeconfig.yaml",
							},
						},
					},
				}
				Expect(sourceK8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &v1alpha1.RemoteArbiter{}
			err := sourceK8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance RemoteArbiter")
			Expect(sourceK8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &RemoteArbiterReconciler{
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
