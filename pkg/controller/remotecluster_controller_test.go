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

		refRemoteCluster := &v1alpha1.RemoteCluster{
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

		AfterEach(func() {
			clusterTypes := []client.Object{
				&corev1.Secret{},
				&v1alpha1.RemoteCluster{},
			}
			err := namespaceCleanUp(sourceK8sClient, clusterTypes, namespaceName)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				empty, err := namespaceEmpty(sourceK8sClient, clusterTypes, namespaceName)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(empty).To(BeTrue())
			}, Timeout, Interval).Should(Succeed())
		})

		It("Should fail to get secret", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.ConfigAvailableConditionType:      metav1.ConditionFalse,
				v1alpha1.ConfigValidConditionType:          metav1.ConditionUnknown,
				v1alpha1.ClusterReachableConditionType:     metav1.ConditionUnknown,
				v1alpha1.HasEnoughPermissionsConditionType: metav1.ConditionUnknown,
			}

			remoteCluster := refRemoteCluster.DeepCopy()
			err := sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err = sourceK8sClient.Get(ctx, remoteClusterNamespacedName, remoteCluster)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteCluster.Status.State).To(Equal(v1alpha1.RemoteClusterErrorState))
				g.Expect(remoteCluster.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteCluster.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteCluster.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
					if expectedCondition == metav1.ConditionFalse {
						g.Expect(condition.Message).NotTo(BeEmpty())
					}
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("Should fail to find key secret", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.ConfigAvailableConditionType:      metav1.ConditionFalse,
				v1alpha1.ConfigValidConditionType:          metav1.ConditionUnknown,
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

			remoteCluster := refRemoteCluster.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err = sourceK8sClient.Get(ctx, remoteClusterNamespacedName, remoteCluster)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteCluster.Status.State).To(Equal(v1alpha1.RemoteClusterErrorState))
				g.Expect(remoteCluster.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteCluster.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteCluster.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
					if expectedCondition == metav1.ConditionFalse {
						g.Expect(condition.Message).NotTo(BeEmpty())
					}
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("Should fail to parse secret", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.ConfigAvailableConditionType:      metav1.ConditionTrue,
				v1alpha1.ConfigValidConditionType:          metav1.ConditionFalse,
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

			remoteCluster := refRemoteCluster.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err = sourceK8sClient.Get(ctx, remoteClusterNamespacedName, remoteCluster)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteCluster.Status.State).To(Equal(v1alpha1.RemoteClusterErrorState))
				g.Expect(remoteCluster.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteCluster.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteCluster.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
					if expectedCondition == metav1.ConditionFalse {
						g.Expect(condition.Message).NotTo(BeEmpty())
					}
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("Should fail to check readiness", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.ConfigAvailableConditionType:      metav1.ConditionTrue,
				v1alpha1.ConfigValidConditionType:          metav1.ConditionTrue,
				v1alpha1.ClusterReachableConditionType:     metav1.ConditionFalse,
				v1alpha1.HasEnoughPermissionsConditionType: metav1.ConditionUnknown,
			}

			freePort, err := freePort()
			Expect(err).NotTo(HaveOccurred())

			kubeconfigBytes, err := noPermissionsUser.KubeConfig()
			Expect(err).NotTo(HaveOccurred())

			replacePortRegexp, err := regexp.Compile("(.*:)[0-9]{1,5}(.*)")
			Expect(err).NotTo(HaveOccurred())
			modifiedKubeconfig := replacePortRegexp.ReplaceAllString(string(kubeconfigBytes), fmt.Sprintf("${1}%d${2}", freePort))

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

			remoteCluster := refRemoteCluster.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err = sourceK8sClient.Get(ctx, remoteClusterNamespacedName, remoteCluster)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteCluster.Status.State).To(Equal(v1alpha1.RemoteClusterErrorState))
				g.Expect(remoteCluster.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteCluster.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteCluster.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
					if expectedCondition == metav1.ConditionFalse {
						g.Expect(condition.Message).NotTo(BeEmpty())
					}
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("Should fail to validate permissions", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.ConfigAvailableConditionType:      metav1.ConditionTrue,
				v1alpha1.ConfigValidConditionType:          metav1.ConditionTrue,
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

			remoteCluster := refRemoteCluster.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err = sourceK8sClient.Get(ctx, remoteClusterNamespacedName, remoteCluster)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteCluster.Status.State).To(Equal(v1alpha1.RemoteClusterErrorState))
				g.Expect(remoteCluster.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteCluster.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteCluster.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
					if expectedCondition == metav1.ConditionFalse {
						g.Expect(condition.Message).NotTo(BeEmpty())
					}
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("Should be ready", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.ConfigAvailableConditionType:      metav1.ConditionTrue,
				v1alpha1.ConfigValidConditionType:          metav1.ConditionTrue,
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

			remoteCluster := refRemoteCluster.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err = sourceK8sClient.Get(ctx, remoteClusterNamespacedName, remoteCluster)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteCluster.Status.State).To(Equal(v1alpha1.RemoteClusterReadyState))
				g.Expect(remoteCluster.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteCluster.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteCluster.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
					if expectedCondition == metav1.ConditionFalse {
						g.Expect(condition.Message).NotTo(BeEmpty())
					}
				}
			}, Timeout, Interval).Should(Succeed())
		})
	})
})
