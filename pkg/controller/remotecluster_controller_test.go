// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"
	"regexp"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"
)

var _ = Describe("RemoteCluster Controller", func() {
	Context("When remote cluster is created", func() {
		namespaceName := "default"

		remoteClusterNamespacedName := types.NamespacedName{
			Name:      "remote-cluster",
			Namespace: namespaceName,
		}

		secretNamespacedName := types.NamespacedName{
			Name:      "cluster-secret",
			Namespace: namespaceName,
		}

		BeforeEach(func() {
			remoteCluster := &v1alpha1.RemoteCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      remoteClusterNamespacedName.Name,
					Namespace: remoteClusterNamespacedName.Namespace,
				},
				Spec: v1alpha1.RemoteClusterSpec{
					Namespace: ArbiterInstallationNamespaceName,
					AccessKeyRef: v1alpha1.KubeconfigSecretSource{
						Name: secretNamespacedName.Name,
						Key:  "kubeconfig.yaml",
					},
				},
			}
			err := sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			secretList := &corev1.SecretList{}
			err := sourceK8sClient.List(ctx, secretList, &client.ListOptions{Namespace: namespaceName})
			Expect(err).NotTo(HaveOccurred())

			for _, item := range secretList.Items {
				if len(item.GetFinalizers()) == 0 {
					continue
				}

				item.SetFinalizers([]string{})
				err := sourceK8sClient.Update(ctx, &item)
				Expect(err).NotTo(HaveOccurred())

				err = sourceK8sClient.Delete(ctx, &item)
				Expect(err).NotTo(HaveOccurred())
			}

			clusterList := &v1alpha1.RemoteClusterList{}
			err = sourceK8sClient.List(ctx, clusterList, &client.ListOptions{Namespace: namespaceName})
			Expect(err).NotTo(HaveOccurred())
			for _, item := range clusterList.Items {
				if len(item.GetFinalizers()) == 0 {
					continue
				}

				item.SetFinalizers([]string{})
				err := sourceK8sClient.Update(ctx, &item)
				Expect(err).NotTo(HaveOccurred())

				err = sourceK8sClient.Delete(ctx, &item)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("Should fail to get secret", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.ConfigAvailableConditionType:      metav1.ConditionFalse,
				v1alpha1.ConvigValidConditionType:          metav1.ConditionUnknown,
				v1alpha1.ClusterReachableConditionType:     metav1.ConditionUnknown,
				v1alpha1.HasEnoughPermissionsConditionType: metav1.ConditionUnknown,
			}

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

			Expect(remoteCluster.Status.Conditions).To(HaveLen(len(conditionsMap)))
			for _, condition := range remoteCluster.Status.Conditions {
				expectedCondition := conditionsMap[condition.Type]
				Expect(condition.Status).To(Equal(expectedCondition))
				if expectedCondition == metav1.ConditionFalse {
					Expect(condition.Message).NotTo(BeEmpty())
				}
			}
		})

		It("Should fail to find key secret", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.ConfigAvailableConditionType:      metav1.ConditionFalse,
				v1alpha1.ConvigValidConditionType:          metav1.ConditionUnknown,
				v1alpha1.ClusterReachableConditionType:     metav1.ConditionUnknown,
				v1alpha1.HasEnoughPermissionsConditionType: metav1.ConditionUnknown,
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretNamespacedName.Name,
					Namespace: secretNamespacedName.Namespace,
				},
				StringData: map[string]string{
					"kbeconfig.yaml": "misspelled kubeconfig key name",
				},
			}

			err := sourceK8sClient.Create(ctx, secret)
			Expect(err).NotTo(HaveOccurred())

			remoteClusterReconciler := &RemoteClusterReconciler{
				Client: sourceK8sClient,
				Scheme: sourceK8sClient.Scheme(),
			}

			_, err = remoteClusterReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: remoteClusterNamespacedName,
			})
			Expect(err).To(HaveOccurred())

			remoteCluster := &v1alpha1.RemoteCluster{}
			err = sourceK8sClient.Get(ctx, remoteClusterNamespacedName, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			Expect(remoteCluster.Status.Conditions).To(HaveLen(len(conditionsMap)))
			for _, condition := range remoteCluster.Status.Conditions {
				expectedCondition := conditionsMap[condition.Type]
				Expect(condition.Status).To(Equal(expectedCondition))
				if expectedCondition == metav1.ConditionFalse {
					Expect(condition.Message).NotTo(BeEmpty())
				}
			}
		})

		It("Should fail to parse secret", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.ConfigAvailableConditionType:      metav1.ConditionTrue,
				v1alpha1.ConvigValidConditionType:          metav1.ConditionFalse,
				v1alpha1.ClusterReachableConditionType:     metav1.ConditionUnknown,
				v1alpha1.HasEnoughPermissionsConditionType: metav1.ConditionUnknown,
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretNamespacedName.Name,
					Namespace: secretNamespacedName.Namespace,
				},
				StringData: map[string]string{
					"kubeconfig.yaml": "malformed kubeconfig",
				},
			}

			err := sourceK8sClient.Create(ctx, secret)
			Expect(err).NotTo(HaveOccurred())

			remoteClusterReconciler := &RemoteClusterReconciler{
				Client: sourceK8sClient,
				Scheme: sourceK8sClient.Scheme(),
			}

			_, err = remoteClusterReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: remoteClusterNamespacedName,
			})
			Expect(err).To(HaveOccurred())

			remoteCluster := &v1alpha1.RemoteCluster{}
			err = sourceK8sClient.Get(ctx, remoteClusterNamespacedName, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			Expect(remoteCluster.Status.Conditions).To(HaveLen(len(conditionsMap)))
			for _, condition := range remoteCluster.Status.Conditions {
				expectedCondition := conditionsMap[condition.Type]
				Expect(condition.Status).To(Equal(expectedCondition))
				if expectedCondition == metav1.ConditionFalse {
					Expect(condition.Message).NotTo(BeEmpty())
				}
			}
		})

		It("Should fail to check readiness", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.ConfigAvailableConditionType:      metav1.ConditionTrue,
				v1alpha1.ConvigValidConditionType:          metav1.ConditionTrue,
				v1alpha1.ClusterReachableConditionType:     metav1.ConditionFalse,
				v1alpha1.HasEnoughPermissionsConditionType: metav1.ConditionUnknown,
			}

			freePort, err := FreePort()
			Expect(err).NotTo(HaveOccurred())

			kubeconfigBytes, err := noPermissionsUser.KubeConfig()
			Expect(err).NotTo(HaveOccurred())
			// re, err := regexp.Compile("^.*:([0-9]{1,5}).*$")
			re, err := regexp.Compile("(.*:)[0-9]{1,5}(.*)")
			Expect(err).NotTo(HaveOccurred())
			modifiedKubeconfig := re.ReplaceAllString(string(kubeconfigBytes), fmt.Sprintf("${1}%d${2}", freePort))

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretNamespacedName.Name,
					Namespace: secretNamespacedName.Namespace,
				},
				StringData: map[string]string{
					"kubeconfig.yaml": modifiedKubeconfig,
				},
			}

			err = sourceK8sClient.Create(ctx, secret)
			Expect(err).NotTo(HaveOccurred())

			remoteClusterReconciler := &RemoteClusterReconciler{
				Client: sourceK8sClient,
				Scheme: sourceK8sClient.Scheme(),
			}

			_, err = remoteClusterReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: remoteClusterNamespacedName,
			})
			Expect(err).To(HaveOccurred())

			remoteCluster := &v1alpha1.RemoteCluster{}
			err = sourceK8sClient.Get(ctx, remoteClusterNamespacedName, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			Expect(remoteCluster.Status.Conditions).To(HaveLen(len(conditionsMap)))
			for _, condition := range remoteCluster.Status.Conditions {
				expectedCondition := conditionsMap[condition.Type]
				Expect(condition.Status).To(Equal(expectedCondition))
				if expectedCondition == metav1.ConditionFalse {
					Expect(condition.Message).NotTo(BeEmpty())
				}
			}
		})

		It("Should fail to validate permissions", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.ConfigAvailableConditionType:      metav1.ConditionTrue,
				v1alpha1.ConvigValidConditionType:          metav1.ConditionTrue,
				v1alpha1.ClusterReachableConditionType:     metav1.ConditionTrue,
				v1alpha1.HasEnoughPermissionsConditionType: metav1.ConditionFalse,
			}

			kubeconfig, err := noPermissionsUser.KubeConfig()
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretNamespacedName.Name,
					Namespace: secretNamespacedName.Namespace,
				},
				StringData: map[string]string{
					"kubeconfig.yaml": string(kubeconfig),
				},
			}

			err = sourceK8sClient.Create(ctx, secret)
			Expect(err).NotTo(HaveOccurred())

			remoteClusterReconciler := &RemoteClusterReconciler{
				Client: sourceK8sClient,
				Scheme: sourceK8sClient.Scheme(),
			}

			_, err = remoteClusterReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: remoteClusterNamespacedName,
			})
			Expect(err).To(HaveOccurred())

			remoteCluster := &v1alpha1.RemoteCluster{}
			err = sourceK8sClient.Get(ctx, remoteClusterNamespacedName, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			Expect(remoteCluster.Status.Conditions).To(HaveLen(len(conditionsMap)))
			for _, condition := range remoteCluster.Status.Conditions {
				expectedCondition := conditionsMap[condition.Type]
				Expect(condition.Status).To(Equal(expectedCondition))
				if expectedCondition == metav1.ConditionFalse {
					Expect(condition.Message).NotTo(BeEmpty())
				}
			}
		})

		It("Should be ready", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.ConfigAvailableConditionType:      metav1.ConditionTrue,
				v1alpha1.ConvigValidConditionType:          metav1.ConditionTrue,
				v1alpha1.ClusterReachableConditionType:     metav1.ConditionTrue,
				v1alpha1.HasEnoughPermissionsConditionType: metav1.ConditionTrue,
			}

			kubeconfig, err := arbiterInstallerUser.KubeConfig()
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretNamespacedName.Name,
					Namespace: secretNamespacedName.Namespace,
				},
				StringData: map[string]string{
					"kubeconfig.yaml": string(kubeconfig),
				},
			}

			err = sourceK8sClient.Create(ctx, secret)
			Expect(err).NotTo(HaveOccurred())

			remoteClusterReconciler := &RemoteClusterReconciler{
				Client: sourceK8sClient,
				Scheme: sourceK8sClient.Scheme(),
			}

			_, err = remoteClusterReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: remoteClusterNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			remoteCluster := &v1alpha1.RemoteCluster{}
			err = sourceK8sClient.Get(ctx, remoteClusterNamespacedName, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			Expect(remoteCluster.Status.Conditions).To(HaveLen(len(conditionsMap)))
			for _, condition := range remoteCluster.Status.Conditions {
				expectedCondition := conditionsMap[condition.Type]
				Expect(condition.Status).To(Equal(expectedCondition))
				if expectedCondition == metav1.ConditionFalse {
					Expect(condition.Message).NotTo(BeEmpty())
				}
			}
		})
	})
})
