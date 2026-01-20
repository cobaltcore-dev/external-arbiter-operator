// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"
)

var _ = Describe("RemoteArbiter Controller", func() {
	Context("When reconciling a resource", func() {
		namespaceName := "default"

		secretNamespacedName := types.NamespacedName{
			Name:      "cluster-secret",
			Namespace: namespaceName,
		}

		cephClusterNamespacedName := types.NamespacedName{
			Name:      "ceph-cluster",
			Namespace: namespaceName,
		}

		remoteClusterNamespacedName := types.NamespacedName{
			Name:      "remote-cluster",
			Namespace: namespaceName,
		}

		remoteArbiterNamespacedName := types.NamespacedName{
			Name:      "remote-arbiter",
			Namespace: namespaceName,
		}

		refRemoteSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretNamespacedName.Name,
				Namespace: secretNamespacedName.Namespace,
			},
			StringData: map[string]string{
				"kubeconfig.yaml": "",
			},
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

		refRemoteArbiter := &v1alpha1.RemoteArbiter{
			ObjectMeta: metav1.ObjectMeta{
				Name:      remoteArbiterNamespacedName.Name,
				Namespace: remoteArbiterNamespacedName.Namespace,
			},
			Spec: v1alpha1.RemoteArbiterSpec{
				CephCluster: v1alpha1.NamespacedReference{
					Name:      cephClusterNamespacedName.Name,
					Namespace: cephClusterNamespacedName.Namespace,
				},
				RemoteCluster: v1alpha1.RemoteClusterConfiguration{
					Name: remoteClusterNamespacedName.Name,
				},
			},
		}

		refCephCluster := &rookv1.CephCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cephClusterNamespacedName.Name,
				Namespace: cephClusterNamespacedName.Namespace,
			},
		}

		AfterEach(func() {
			sourceClusterTypes := []client.Object{
				&corev1.Secret{},
				&corev1.ConfigMap{},
				&appsv1.Deployment{},
				&rookv1.CephCluster{},
				&v1alpha1.RemoteCluster{},
				&v1alpha1.RemoteArbiter{},
			}
			err := namespaceCleanUp(sourceK8sClient, sourceClusterTypes, namespaceName)
			Expect(err).NotTo(HaveOccurred())

			targetClusterTypes := []client.Object{
				&corev1.Secret{},
				&corev1.Service{},
				&corev1.ConfigMap{},
				&appsv1.Deployment{},
			}
			err = namespaceCleanUp(targetK8sClient, targetClusterTypes, ArbiterInstallationNamespaceName)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				empty, err := namespaceEmpty(sourceK8sClient, sourceClusterTypes, namespaceName)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(empty).To(BeTrue())
			}, Timeout, Interval).Should(Succeed())

			Eventually(func(g Gomega) {
				empty, err := namespaceEmpty(sourceK8sClient, sourceClusterTypes, ArbiterInstallationNamespaceName)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(empty).To(BeTrue())
			}, Timeout, Interval).Should(Succeed())
		})

		It("should fail to find remote cluster", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.RemoteClusterExistsConditionType:     metav1.ConditionFalse,
				v1alpha1.RemoteClusterReadyConditionType:      metav1.ConditionUnknown,
				v1alpha1.CephClusterExistsConditionType:       metav1.ConditionUnknown,
				v1alpha1.CephClusterReadyConditionType:        metav1.ConditionUnknown,
				v1alpha1.CephClusterConfiguredConditionType:   metav1.ConditionUnknown,
				v1alpha1.MonitorDeploymentExistsConditionType: metav1.ConditionUnknown,
				v1alpha1.MonitorDeploymentReadyConditionType:  metav1.ConditionUnknown,
				v1alpha1.ArbiterDeploymentExistsConditionType: metav1.ConditionUnknown,
				v1alpha1.ArbiterDeploymentReadyConditionType:  metav1.ConditionUnknown,
			}

			remoteArbiter := refRemoteArbiter.DeepCopy()

			remoteArbiter.Spec.RemoteCluster = v1alpha1.RemoteClusterConfiguration{
				Name: "missing-remotecluster",
			}

			err := sourceK8sClient.Create(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err = sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterErrorState))
				g.Expect(remoteArbiter.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteArbiter.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteArbiter.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
					if expectedCondition == metav1.ConditionFalse {
						g.Expect(condition.Message).NotTo(BeEmpty())
					}
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("should fail to check if remote cluster ready", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.RemoteClusterExistsConditionType:     metav1.ConditionTrue,
				v1alpha1.RemoteClusterReadyConditionType:      metav1.ConditionFalse,
				v1alpha1.CephClusterExistsConditionType:       metav1.ConditionUnknown,
				v1alpha1.CephClusterReadyConditionType:        metav1.ConditionUnknown,
				v1alpha1.CephClusterConfiguredConditionType:   metav1.ConditionUnknown,
				v1alpha1.MonitorDeploymentExistsConditionType: metav1.ConditionUnknown,
				v1alpha1.MonitorDeploymentReadyConditionType:  metav1.ConditionUnknown,
				v1alpha1.ArbiterDeploymentExistsConditionType: metav1.ConditionUnknown,
				v1alpha1.ArbiterDeploymentReadyConditionType:  metav1.ConditionUnknown,
			}

			remoteArbiter := refRemoteArbiter.DeepCopy()

			remoteArbiter.Spec.RemoteCluster = v1alpha1.RemoteClusterConfiguration{
				Spec: &v1alpha1.RemoteClusterSpec{
					Namespace: ArbiterInstallationNamespaceName,
					AccessKeyRef: v1alpha1.KubeconfigSecretSource{
						Name: secretNamespacedName.Name,
						Key:  "missing-key.yaml",
					},
				},
			}

			err := sourceK8sClient.Create(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err = sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterErrorState))
				g.Expect(remoteArbiter.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteArbiter.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteArbiter.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
					if expectedCondition == metav1.ConditionFalse {
						g.Expect(condition.Message).NotTo(BeEmpty())
					}
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("should fail to check if ceph cluster exists", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.RemoteClusterExistsConditionType:     metav1.ConditionTrue,
				v1alpha1.RemoteClusterReadyConditionType:      metav1.ConditionTrue,
				v1alpha1.CephClusterExistsConditionType:       metav1.ConditionFalse,
				v1alpha1.CephClusterReadyConditionType:        metav1.ConditionUnknown,
				v1alpha1.CephClusterConfiguredConditionType:   metav1.ConditionUnknown,
				v1alpha1.MonitorDeploymentExistsConditionType: metav1.ConditionUnknown,
				v1alpha1.MonitorDeploymentReadyConditionType:  metav1.ConditionUnknown,
				v1alpha1.ArbiterDeploymentExistsConditionType: metav1.ConditionUnknown,
				v1alpha1.ArbiterDeploymentReadyConditionType:  metav1.ConditionUnknown,
			}

			kubeconfig, err := arbiterInstallerUser.KubeConfig()
			Expect(err).NotTo(HaveOccurred())

			remoteSecret := refRemoteSecret.DeepCopy()
			remoteSecret.StringData["kubeconfig.yaml"] = string(kubeconfig)
			err = sourceK8sClient.Create(ctx, remoteSecret)
			Expect(err).NotTo(HaveOccurred())

			remoteCluster := refRemoteCluster.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			remoteArbiter := refRemoteArbiter.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterErrorState))
				g.Expect(remoteArbiter.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteArbiter.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteArbiter.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
					if expectedCondition == metav1.ConditionFalse {
						g.Expect(condition.Message).NotTo(BeEmpty())
					}
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("should fail to check if remote cluster ready", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.RemoteClusterExistsConditionType:     metav1.ConditionTrue,
				v1alpha1.RemoteClusterReadyConditionType:      metav1.ConditionTrue,
				v1alpha1.CephClusterExistsConditionType:       metav1.ConditionTrue,
				v1alpha1.CephClusterReadyConditionType:        metav1.ConditionFalse,
				v1alpha1.CephClusterConfiguredConditionType:   metav1.ConditionUnknown,
				v1alpha1.MonitorDeploymentExistsConditionType: metav1.ConditionUnknown,
				v1alpha1.MonitorDeploymentReadyConditionType:  metav1.ConditionUnknown,
				v1alpha1.ArbiterDeploymentExistsConditionType: metav1.ConditionUnknown,
				v1alpha1.ArbiterDeploymentReadyConditionType:  metav1.ConditionUnknown,
			}

			cephCluster := refCephCluster.DeepCopy()
			err := sourceK8sClient.Create(ctx, cephCluster)
			Expect(err).NotTo(HaveOccurred())

			kubeconfig, err := arbiterInstallerUser.KubeConfig()
			Expect(err).NotTo(HaveOccurred())

			remoteSecret := refRemoteSecret.DeepCopy()
			remoteSecret.StringData["kubeconfig.yaml"] = string(kubeconfig)
			err = sourceK8sClient.Create(ctx, remoteSecret)
			Expect(err).NotTo(HaveOccurred())

			remoteCluster := refRemoteCluster.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			remoteArbiter := refRemoteArbiter.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterErrorState))
				g.Expect(remoteArbiter.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteArbiter.Status.Conditions).To(HaveLen(len(conditionsMap)))
				fmt.Println("conditions", remoteArbiter.Status.Conditions)
				for _, condition := range remoteArbiter.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
					if expectedCondition == metav1.ConditionFalse {
						g.Expect(condition.Message).NotTo(BeEmpty())
					}
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("should fail to check if monitor deployment exists", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.RemoteClusterExistsConditionType:     metav1.ConditionTrue,
				v1alpha1.RemoteClusterReadyConditionType:      metav1.ConditionTrue,
				v1alpha1.CephClusterExistsConditionType:       metav1.ConditionTrue,
				v1alpha1.CephClusterReadyConditionType:        metav1.ConditionTrue,
				v1alpha1.CephClusterConfiguredConditionType:   metav1.ConditionTrue,
				v1alpha1.MonitorDeploymentExistsConditionType: metav1.ConditionFalse,
				v1alpha1.MonitorDeploymentReadyConditionType:  metav1.ConditionUnknown,
				v1alpha1.ArbiterDeploymentExistsConditionType: metav1.ConditionUnknown,
				v1alpha1.ArbiterDeploymentReadyConditionType:  metav1.ConditionUnknown,
			}

			cephCluster := refCephCluster.DeepCopy()
			err := sourceK8sClient.Create(ctx, cephCluster)
			Expect(err).NotTo(HaveOccurred())

			cephCluster.Status.Phase = rookv1.ConditionReady
			err = sourceK8sClient.Status().Update(ctx, cephCluster)
			Expect(err).NotTo(HaveOccurred())

			kubeconfig, err := arbiterInstallerUser.KubeConfig()
			Expect(err).NotTo(HaveOccurred())

			remoteSecret := refRemoteSecret.DeepCopy()
			remoteSecret.StringData["kubeconfig.yaml"] = string(kubeconfig)
			err = sourceK8sClient.Create(ctx, remoteSecret)
			Expect(err).NotTo(HaveOccurred())

			remoteCluster := refRemoteCluster.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			remoteArbiter := refRemoteArbiter.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterErrorState))
				g.Expect(remoteArbiter.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteArbiter.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteArbiter.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
					if expectedCondition == metav1.ConditionFalse {
						g.Expect(condition.Message).NotTo(BeEmpty())
					}
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("should fail to check if monitor deployment ready", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.RemoteClusterExistsConditionType:     metav1.ConditionTrue,
				v1alpha1.RemoteClusterReadyConditionType:      metav1.ConditionTrue,
				v1alpha1.CephClusterExistsConditionType:       metav1.ConditionTrue,
				v1alpha1.CephClusterReadyConditionType:        metav1.ConditionTrue,
				v1alpha1.CephClusterConfiguredConditionType:   metav1.ConditionTrue,
				v1alpha1.MonitorDeploymentExistsConditionType: metav1.ConditionTrue,
				v1alpha1.MonitorDeploymentReadyConditionType:  metav1.ConditionFalse,
				v1alpha1.ArbiterDeploymentExistsConditionType: metav1.ConditionUnknown,
				v1alpha1.ArbiterDeploymentReadyConditionType:  metav1.ConditionUnknown,
			}

			cephCluster := refCephCluster.DeepCopy()
			err := sourceK8sClient.Create(ctx, cephCluster)
			Expect(err).NotTo(HaveOccurred())

			monitorOverrideConfigMap := refMonitorOverrideConfigMap.DeepCopy()
			monitorOverrideConfigMap.Namespace = cephClusterNamespacedName.Namespace
			err = sourceK8sClient.Create(ctx, monitorOverrideConfigMap)
			Expect(err).NotTo(HaveOccurred())

			monitorKeyringSecret := refMonitorKeyringSecret.DeepCopy()
			monitorKeyringSecret.Namespace = cephClusterNamespacedName.Namespace
			err = sourceK8sClient.Create(ctx, monitorKeyringSecret)
			Expect(err).NotTo(HaveOccurred())

			monitorEnvVarSecret := refMonitorEnvVarSecret.DeepCopy()
			monitorEnvVarSecret.Namespace = cephClusterNamespacedName.Namespace
			err = sourceK8sClient.Create(ctx, monitorEnvVarSecret)
			Expect(err).NotTo(HaveOccurred())

			monitorDeployment := refMonitorDeployment.DeepCopy()
			monitorDeployment.Namespace = cephClusterNamespacedName.Namespace
			monitorDeployment.Labels["app.kubernetes.io/part-of"] = cephClusterNamespacedName.Name
			err = sourceK8sClient.Create(ctx, monitorDeployment)
			Expect(err).NotTo(HaveOccurred())

			cephCluster.Status.Phase = rookv1.ConditionReady
			err = sourceK8sClient.Status().Update(ctx, cephCluster)
			Expect(err).NotTo(HaveOccurred())

			kubeconfig, err := arbiterInstallerUser.KubeConfig()
			Expect(err).NotTo(HaveOccurred())

			remoteSecret := refRemoteSecret.DeepCopy()
			remoteSecret.StringData["kubeconfig.yaml"] = string(kubeconfig)
			err = sourceK8sClient.Create(ctx, remoteSecret)
			Expect(err).NotTo(HaveOccurred())

			remoteCluster := refRemoteCluster.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			remoteArbiter := refRemoteArbiter.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterErrorState))
				g.Expect(remoteArbiter.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteArbiter.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteArbiter.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
					if expectedCondition == metav1.ConditionFalse {
						g.Expect(condition.Message).NotTo(BeEmpty())
					}
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("should fail to check if arbiter deployment ready", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.RemoteClusterExistsConditionType:     metav1.ConditionTrue,
				v1alpha1.RemoteClusterReadyConditionType:      metav1.ConditionTrue,
				v1alpha1.CephClusterExistsConditionType:       metav1.ConditionTrue,
				v1alpha1.CephClusterReadyConditionType:        metav1.ConditionTrue,
				v1alpha1.CephClusterConfiguredConditionType:   metav1.ConditionTrue,
				v1alpha1.MonitorDeploymentExistsConditionType: metav1.ConditionTrue,
				v1alpha1.MonitorDeploymentReadyConditionType:  metav1.ConditionTrue,
				v1alpha1.ArbiterDeploymentExistsConditionType: metav1.ConditionTrue,
				v1alpha1.ArbiterDeploymentReadyConditionType:  metav1.ConditionFalse,
			}

			cephCluster := refCephCluster.DeepCopy()
			err := sourceK8sClient.Create(ctx, cephCluster)
			Expect(err).NotTo(HaveOccurred())

			monitorOverrideConfigMap := refMonitorOverrideConfigMap.DeepCopy()
			monitorOverrideConfigMap.Namespace = cephClusterNamespacedName.Namespace
			err = sourceK8sClient.Create(ctx, monitorOverrideConfigMap)
			Expect(err).NotTo(HaveOccurred())

			monitorKeyringSecret := refMonitorKeyringSecret.DeepCopy()
			monitorKeyringSecret.Namespace = cephClusterNamespacedName.Namespace
			err = sourceK8sClient.Create(ctx, monitorKeyringSecret)
			Expect(err).NotTo(HaveOccurred())

			monitorEnvVarSecret := refMonitorEnvVarSecret.DeepCopy()
			monitorEnvVarSecret.Namespace = cephClusterNamespacedName.Namespace
			err = sourceK8sClient.Create(ctx, monitorEnvVarSecret)
			Expect(err).NotTo(HaveOccurred())

			monitorDeployment := refMonitorDeployment.DeepCopy()
			monitorDeployment.Namespace = cephClusterNamespacedName.Namespace
			monitorDeployment.Labels["app.kubernetes.io/part-of"] = cephClusterNamespacedName.Name
			err = sourceK8sClient.Create(ctx, monitorDeployment)
			Expect(err).NotTo(HaveOccurred())

			monitorDeployment.Status.UpdatedReplicas = 1
			monitorDeployment.Status.Replicas = 1
			err = sourceK8sClient.Status().Update(ctx, monitorDeployment)
			Expect(err).NotTo(HaveOccurred())

			cephCluster.Status.Phase = rookv1.ConditionReady
			err = sourceK8sClient.Status().Update(ctx, cephCluster)
			Expect(err).NotTo(HaveOccurred())

			kubeconfig, err := arbiterInstallerUser.KubeConfig()
			Expect(err).NotTo(HaveOccurred())

			remoteSecret := refRemoteSecret.DeepCopy()
			remoteSecret.StringData["kubeconfig.yaml"] = string(kubeconfig)
			err = sourceK8sClient.Create(ctx, remoteSecret)
			Expect(err).NotTo(HaveOccurred())

			remoteCluster := refRemoteCluster.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			remoteArbiter := refRemoteArbiter.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterErrorState))
				g.Expect(remoteArbiter.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteArbiter.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteArbiter.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
					if expectedCondition == metav1.ConditionFalse {
						g.Expect(condition.Message).NotTo(BeEmpty())
					}
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("should succeed", func() {
			conditionsMap := map[string]metav1.ConditionStatus{
				v1alpha1.RemoteClusterExistsConditionType:     metav1.ConditionTrue,
				v1alpha1.RemoteClusterReadyConditionType:      metav1.ConditionTrue,
				v1alpha1.CephClusterExistsConditionType:       metav1.ConditionTrue,
				v1alpha1.CephClusterReadyConditionType:        metav1.ConditionTrue,
				v1alpha1.CephClusterConfiguredConditionType:   metav1.ConditionTrue,
				v1alpha1.MonitorDeploymentExistsConditionType: metav1.ConditionTrue,
				v1alpha1.MonitorDeploymentReadyConditionType:  metav1.ConditionTrue,
				v1alpha1.ArbiterDeploymentExistsConditionType: metav1.ConditionTrue,
				v1alpha1.ArbiterDeploymentReadyConditionType:  metav1.ConditionTrue,
			}

			cephCluster := refCephCluster.DeepCopy()
			err := sourceK8sClient.Create(ctx, cephCluster)
			Expect(err).NotTo(HaveOccurred())

			monitorOverrideConfigMap := refMonitorOverrideConfigMap.DeepCopy()
			monitorOverrideConfigMap.Namespace = cephClusterNamespacedName.Namespace
			err = sourceK8sClient.Create(ctx, monitorOverrideConfigMap)
			Expect(err).NotTo(HaveOccurred())

			monitorKeyringSecret := refMonitorKeyringSecret.DeepCopy()
			monitorKeyringSecret.Namespace = cephClusterNamespacedName.Namespace
			err = sourceK8sClient.Create(ctx, monitorKeyringSecret)
			Expect(err).NotTo(HaveOccurred())

			monitorEnvVarSecret := refMonitorEnvVarSecret.DeepCopy()
			monitorEnvVarSecret.Namespace = cephClusterNamespacedName.Namespace
			err = sourceK8sClient.Create(ctx, monitorEnvVarSecret)
			Expect(err).NotTo(HaveOccurred())

			monitorDeployment := refMonitorDeployment.DeepCopy()
			monitorDeployment.Namespace = cephClusterNamespacedName.Namespace
			monitorDeployment.Labels["app.kubernetes.io/part-of"] = cephClusterNamespacedName.Name
			err = sourceK8sClient.Create(ctx, monitorDeployment)
			Expect(err).NotTo(HaveOccurred())

			monitorDeployment.Status.UpdatedReplicas = 1
			monitorDeployment.Status.Replicas = 1
			err = sourceK8sClient.Status().Update(ctx, monitorDeployment)
			Expect(err).NotTo(HaveOccurred())

			cephCluster.Status.Phase = rookv1.ConditionReady
			err = sourceK8sClient.Status().Update(ctx, cephCluster)
			Expect(err).NotTo(HaveOccurred())

			kubeconfig, err := arbiterInstallerUser.KubeConfig()
			Expect(err).NotTo(HaveOccurred())

			remoteSecret := refRemoteSecret.DeepCopy()
			remoteSecret.StringData["kubeconfig.yaml"] = string(kubeconfig)
			err = sourceK8sClient.Create(ctx, remoteSecret)
			Expect(err).NotTo(HaveOccurred())

			remoteCluster := refRemoteCluster.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			remoteArbiter := refRemoteArbiter.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			var arbiterDeployment *appsv1.Deployment
			Eventually(func(g Gomega) {
				arbiterDeploymentList := &appsv1.DeploymentList{}
				namespaceSelector := client.InNamespace(remoteCluster.Spec.Namespace)
				labelSelector := client.MatchingLabels{
					RemoteArbiterLookupLabel: remoteArbiter.Name,
				}
				err := targetK8sClient.List(ctx, arbiterDeploymentList, namespaceSelector, labelSelector)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(arbiterDeploymentList.Items).To(HaveLen(1))
				arbiterDeployment = &arbiterDeploymentList.Items[0]
			}, Timeout, Interval).Should(Succeed())

			arbiterDeployment.Status.UpdatedReplicas = 1
			arbiterDeployment.Status.Replicas = 1
			err = targetK8sClient.Status().Update(ctx, arbiterDeployment)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterReadyState))
				g.Expect(remoteArbiter.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteArbiter.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteArbiter.Status.Conditions {
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
