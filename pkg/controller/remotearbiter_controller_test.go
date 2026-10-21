// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
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
			targetClusterTypes := []client.Object{
				&corev1.Secret{},
				&corev1.Service{},
				&corev1.ConfigMap{},
				&appsv1.Deployment{},
			}

			// The controller's deletion cleanup can be delayed well beyond
			// the Eventually timeouts below by its reconcile error backoff,
			// so force-delete (finalizers included) on both clusters instead
			// of waiting for it. The specs assert the real cleanup behavior;
			// the AfterEach only guarantees a clean slate for the next spec.
			// Retry inside Eventually: the controller races the finalizer
			// removal with its own updates, and objects stuck in Terminating
			// would break the Create calls of the next spec.
			Eventually(func(g Gomega) {
				g.Expect(forceNamespaceCleanUp(sourceK8sClient, sourceClusterTypes, namespaceName)).To(Succeed())
				g.Expect(forceNamespaceCleanUp(targetK8sClient, targetClusterTypes, ArbiterInstallationNamespaceName)).To(Succeed())

				empty, err := namespaceEmpty(sourceK8sClient, sourceClusterTypes, namespaceName)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(empty).To(BeTrue())
			}, Timeout, Interval).Should(Succeed())

			Eventually(func(g Gomega) {
				empty, err := namespaceEmpty(targetK8sClient, targetClusterTypes, ArbiterInstallationNamespaceName)
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

			// Mark the arbiter deployment ready. Retry on conflict and
			// re-assert on every poll: the controller updates the deployment
			// while rolling it out. Its reconcile error backoff (accumulated
			// from earlier "remote cluster not ready" retries) can delay the
			// observation of the ready status beyond the default timeout,
			// so the ready wait below uses a longer timeout.
			Eventually(func(g Gomega) {
				arbiterDeploymentList := &appsv1.DeploymentList{}
				namespaceSelector := client.InNamespace(remoteCluster.Spec.Namespace)
				labelSelector := client.MatchingLabels{
					RemoteArbiterLookupLabel: remoteArbiter.Name,
				}
				err := targetK8sClient.List(ctx, arbiterDeploymentList, namespaceSelector, labelSelector)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(arbiterDeploymentList.Items).To(HaveLen(1))
				arbiterDeployment := &arbiterDeploymentList.Items[0]
				if arbiterDeployment.Status.Replicas != 1 || arbiterDeployment.Status.UpdatedReplicas != 1 {
					arbiterDeployment.Status.UpdatedReplicas = 1
					arbiterDeployment.Status.Replicas = 1
					err = targetK8sClient.Status().Update(ctx, arbiterDeployment)
					g.Expect(err).NotTo(HaveOccurred())
				}
			}, Timeout, Interval).Should(Succeed())

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
			}, time.Minute*2, Interval).Should(Succeed())
		})

		It("should create service with correct type and use ClusterIP as public address", func() {
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

			// Create RemoteArbiter with explicit Service type
			remoteArbiter := refRemoteArbiter.DeepCopy()
			remoteArbiter.Spec.Service = &v1alpha1.ServiceConfiguration{
				Type: corev1.ServiceTypeClusterIP,
			}
			err = sourceK8sClient.Create(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			// Wait for the Service to appear on the target cluster
			var arbiterService *corev1.Service
			Eventually(func(g Gomega) {
				serviceList := &corev1.ServiceList{}
				namespaceSelector := client.InNamespace(remoteCluster.Spec.Namespace)
				labelSelector := client.MatchingLabels{
					RemoteArbiterLookupLabel: remoteArbiter.Name,
				}
				err := targetK8sClient.List(ctx, serviceList, namespaceSelector, labelSelector)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(serviceList.Items).To(HaveLen(1))
				arbiterService = &serviceList.Items[0]
			}, Timeout, Interval).Should(Succeed())

			// Verify Service type is ClusterIP (not defaulted by K8s)
			Expect(arbiterService.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
			// Verify Service has both Ceph messenger ports
			expectArbiterServicePorts(arbiterService, false)
			// Verify ClusterIP was allocated
			Expect(arbiterService.Spec.ClusterIP).NotTo(BeEmpty())

			// Wait for the arbiter deployment to appear on the target cluster
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

			// Verify --public-addr uses the Service ClusterIP, not $(ROOK_POD_IP)
			foundPublicAddr := false
			for _, container := range arbiterDeployment.Spec.Template.Spec.Containers {
				for _, arg := range container.Args {
					if strings.HasPrefix(arg, "--public-addr=") {
						addr := strings.TrimPrefix(arg, "--public-addr=")
						Expect(addr).To(Equal(arbiterService.Spec.ClusterIP),
							fmt.Sprintf("expected --public-addr to use Service ClusterIP %s, got %s", arbiterService.Spec.ClusterIP, addr))
						Expect(addr).NotTo(Equal("$(ROOK_POD_IP)"),
							"--public-addr should not use pod IP when Service is configured")
						foundPublicAddr = true
					}
				}
			}
			Expect(foundPublicAddr).To(BeTrue(), "--public-addr argument not found in container args")

			// Simulate arbiter deployment becoming ready
			arbiterDeployment.Status.UpdatedReplicas = 1
			arbiterDeployment.Status.Replicas = 1
			err = targetK8sClient.Status().Update(ctx, arbiterDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify final Ready state with all conditions True
			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterReadyState))
				g.Expect(remoteArbiter.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteArbiter.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteArbiter.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("should rebuild a legacy deployment with the service address when a service is added", func() {
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

			// Create a legacy RemoteArbiter without a service, as created by
			// the controller before service based exposure was available.
			remoteArbiter := refRemoteArbiter.DeepCopy()
			err = sourceK8sClient.Create(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			// Wait for the arbiter deployment to appear on the target cluster
			// and to advertise the ephemeral pod IP. A deployment left over
			// from another spec may be adopted by the controller first; the
			// public address drift check converges it to the desired address.
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
				g.Expect(deploymentPublicAddress(arbiterDeployment)).To(Equal("$(ROOK_POD_IP)"))
			}, Timeout, Interval).Should(Succeed())

			// Simulate arbiter deployment becoming ready. The controller may
			// update the deployment concurrently, so tolerate update conflicts.
			Eventually(func(g Gomega) {
				deployment := arbiterDeployment.DeepCopy()
				deployment.Status.UpdatedReplicas = 1
				deployment.Status.Replicas = 1
				g.Expect(targetK8sClient.Status().Update(ctx, deployment)).To(Succeed())
			}, Timeout, Interval).Should(Succeed())

			// Verify the legacy arbiter reaches the Ready state
			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterReadyState))
			}, Timeout, Interval).Should(Succeed())

			// Add a service to the legacy RemoteArbiter, as the defaulting
			// webhook would do when the resource is updated after an upgrade.
			// The controller updates the resource status concurrently, so
			// tolerate update conflicts.
			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				remoteArbiter.Spec.Service = &v1alpha1.ServiceConfiguration{
					Type: corev1.ServiceTypeClusterIP,
				}
				g.Expect(sourceK8sClient.Update(ctx, remoteArbiter)).To(Succeed())
			}, Timeout, Interval).Should(Succeed())

			// Wait for the Service to appear on the target cluster
			var arbiterService *corev1.Service
			Eventually(func(g Gomega) {
				serviceList := &corev1.ServiceList{}
				namespaceSelector := client.InNamespace(remoteCluster.Spec.Namespace)
				labelSelector := client.MatchingLabels{
					RemoteArbiterLookupLabel: remoteArbiter.Name,
				}
				err := targetK8sClient.List(ctx, serviceList, namespaceSelector, labelSelector)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(serviceList.Items).To(HaveLen(1))
				arbiterService = &serviceList.Items[0]
				g.Expect(arbiterService.Spec.ClusterIP).NotTo(BeEmpty())
			}, Timeout, Interval).Should(Succeed())

			// The deployment is rebuilt with the Service ClusterIP instead of
			// the pod IP placeholder
			Eventually(func(g Gomega) {
				arbiterDeploymentList := &appsv1.DeploymentList{}
				namespaceSelector := client.InNamespace(remoteCluster.Spec.Namespace)
				labelSelector := client.MatchingLabels{
					RemoteArbiterLookupLabel: remoteArbiter.Name,
				}
				err := targetK8sClient.List(ctx, arbiterDeploymentList, namespaceSelector, labelSelector)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(arbiterDeploymentList.Items).To(HaveLen(1))
				g.Expect(deploymentPublicAddress(&arbiterDeploymentList.Items[0])).
					To(Equal(arbiterService.Spec.ClusterIP))
			}, Timeout, Interval).Should(Succeed())

			// Verify the arbiter returns to the Ready state
			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterReadyState))
				g.Expect(remoteArbiter.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteArbiter.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteArbiter.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("should create service with NodePort type, pinned node ports, and use the node IP as public address", func() {
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

			// Create RemoteArbiter with a non-default Service type, so the
			// test verifies the type is propagated from the CR rather than
			// defaulted by Kubernetes
			remoteArbiter := refRemoteArbiter.DeepCopy()
			remoteArbiter.Spec.Service = &v1alpha1.ServiceConfiguration{
				Type:   corev1.ServiceTypeNodePort,
				NodeIP: "192.0.2.10",
			}
			err = sourceK8sClient.Create(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			// Wait for the Service to appear on the target cluster. A service
			// left over from another spec may be adopted by the controller
			// first; the service reconciliation converges it to the desired
			// type.
			var arbiterService *corev1.Service
			Eventually(func(g Gomega) {
				serviceList := &corev1.ServiceList{}
				namespaceSelector := client.InNamespace(remoteCluster.Spec.Namespace)
				labelSelector := client.MatchingLabels{
					RemoteArbiterLookupLabel: remoteArbiter.Name,
				}
				err := targetK8sClient.List(ctx, serviceList, namespaceSelector, labelSelector)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(serviceList.Items).To(HaveLen(1))
				g.Expect(serviceList.Items[0].Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
				arbiterService = &serviceList.Items[0]
			}, Timeout, Interval).Should(Succeed())
			// The node ports are pinned to the Ceph messenger ports in the
			// same update as the type
			expectArbiterServicePorts(arbiterService, true)

			// Wait for the arbiter deployment to appear on the target
			// cluster and to use the node IP. A deployment left over from
			// another spec may be adopted by the controller first; the
			// public address drift check converges it to the desired address.
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
				g.Expect(deploymentPublicAddress(&arbiterDeploymentList.Items[0])).To(Equal("192.0.2.10"))
				arbiterDeployment = &arbiterDeploymentList.Items[0]
			}, Timeout, Interval).Should(Succeed())

			// Simulate arbiter deployment becoming ready. The controller may
			// update the deployment concurrently, so tolerate update conflicts.
			Eventually(func(g Gomega) {
				deployment := &appsv1.Deployment{}
				err := targetK8sClient.Get(ctx, client.ObjectKeyFromObject(arbiterDeployment), deployment)
				g.Expect(err).NotTo(HaveOccurred())
				deployment.Status.UpdatedReplicas = 1
				deployment.Status.Replicas = 1
				g.Expect(targetK8sClient.Status().Update(ctx, deployment)).To(Succeed())
			}, Timeout, Interval).Should(Succeed())

			// Verify the arbiter reaches the Ready state
			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterReadyState))
				g.Expect(remoteArbiter.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteArbiter.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteArbiter.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("should repair the service in place and rebuild the deployment when the service type changes", func() {
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

			// Create RemoteArbiter with a ClusterIP service
			remoteArbiter := refRemoteArbiter.DeepCopy()
			remoteArbiter.Spec.Service = &v1alpha1.ServiceConfiguration{
				Type: corev1.ServiceTypeClusterIP,
			}
			err = sourceK8sClient.Create(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			// Wait for the Service to appear on the target cluster
			var arbiterService *corev1.Service
			Eventually(func(g Gomega) {
				serviceList := &corev1.ServiceList{}
				namespaceSelector := client.InNamespace(remoteCluster.Spec.Namespace)
				labelSelector := client.MatchingLabels{
					RemoteArbiterLookupLabel: remoteArbiter.Name,
				}
				err := targetK8sClient.List(ctx, serviceList, namespaceSelector, labelSelector)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(serviceList.Items).To(HaveLen(1))
				arbiterService = &serviceList.Items[0]
			}, Timeout, Interval).Should(Succeed())
			serviceName := arbiterService.Name

			// Wait for the arbiter deployment to appear on the target cluster
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

			// Simulate arbiter deployment becoming ready. The controller may
			// update the deployment concurrently, so tolerate update conflicts.
			Eventually(func(g Gomega) {
				deployment := &appsv1.Deployment{}
				err := targetK8sClient.Get(ctx, client.ObjectKeyFromObject(arbiterDeployment), deployment)
				g.Expect(err).NotTo(HaveOccurred())
				deployment.Status.UpdatedReplicas = 1
				deployment.Status.Replicas = 1
				g.Expect(targetK8sClient.Status().Update(ctx, deployment)).To(Succeed())
			}, Timeout, Interval).Should(Succeed())

			// Verify the arbiter reaches the Ready state
			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterReadyState))
			}, Timeout, Interval).Should(Succeed())

			// Change the service type on the CR, as a user would when
			// changing the exposure strategy
			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				remoteArbiter.Spec.Service = &v1alpha1.ServiceConfiguration{
					Type:   corev1.ServiceTypeNodePort,
					NodeIP: "192.0.2.10",
				}
				g.Expect(sourceK8sClient.Update(ctx, remoteArbiter)).To(Succeed())
			}, Timeout, Interval).Should(Succeed())

			// The existing Service is repaired in place, keeping its name
			Eventually(func(g Gomega) {
				serviceList := &corev1.ServiceList{}
				namespaceSelector := client.InNamespace(remoteCluster.Spec.Namespace)
				labelSelector := client.MatchingLabels{
					RemoteArbiterLookupLabel: remoteArbiter.Name,
				}
				err := targetK8sClient.List(ctx, serviceList, namespaceSelector, labelSelector)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(serviceList.Items).To(HaveLen(1))
				g.Expect(serviceList.Items[0].Name).To(Equal(serviceName))
				g.Expect(serviceList.Items[0].Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
			}, Timeout, Interval).Should(Succeed())

			// The node ports of the repaired Service are pinned to the Ceph
			// messenger ports
			serviceList := &corev1.ServiceList{}
			namespaceSelector := client.InNamespace(remoteCluster.Spec.Namespace)
			labelSelector := client.MatchingLabels{
				RemoteArbiterLookupLabel: remoteArbiter.Name,
			}
			err = targetK8sClient.List(ctx, serviceList, namespaceSelector, labelSelector)
			Expect(err).NotTo(HaveOccurred())
			Expect(serviceList.Items).To(HaveLen(1))
			expectArbiterServicePorts(&serviceList.Items[0], true)

			// The deployment is rebuilt with the node IP as public address
			Eventually(func(g Gomega) {
				arbiterDeploymentList := &appsv1.DeploymentList{}
				namespaceSelector := client.InNamespace(remoteCluster.Spec.Namespace)
				labelSelector := client.MatchingLabels{
					RemoteArbiterLookupLabel: remoteArbiter.Name,
				}
				err := targetK8sClient.List(ctx, arbiterDeploymentList, namespaceSelector, labelSelector)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(arbiterDeploymentList.Items).To(HaveLen(1))
				g.Expect(deploymentPublicAddress(&arbiterDeploymentList.Items[0])).To(Equal("192.0.2.10"))
			}, Timeout, Interval).Should(Succeed())

			// Verify the arbiter returns to the Ready state
			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterReadyState))
				g.Expect(remoteArbiter.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteArbiter.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteArbiter.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
				}
			}, Timeout, Interval).Should(Succeed())
		})

		It("should follow the load balancer ingress address", func() {
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

			// Create RemoteArbiter with a LoadBalancer service and a short
			// check interval, so that ingress address drift is caught by the
			// periodic reconcile quickly without a source cluster event
			remoteArbiter := refRemoteArbiter.DeepCopy()
			remoteArbiter.Spec.Service = &v1alpha1.ServiceConfiguration{
				Type: corev1.ServiceTypeLoadBalancer,
			}
			remoteArbiter.Spec.CheckInterval = &v1alpha1.Interval{
				Duration: time.Second,
			}
			err = sourceK8sClient.Create(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			// The Service is created on the target cluster, but while the
			// load balancer has no ingress address the reconcile cannot
			// determine the public address and errors out. A service left
			// over from another spec may be adopted by the controller first;
			// the service reconciliation converges it to the desired type.
			var arbiterService *corev1.Service
			Eventually(func(g Gomega) {
				serviceList := &corev1.ServiceList{}
				namespaceSelector := client.InNamespace(remoteCluster.Spec.Namespace)
				labelSelector := client.MatchingLabels{
					RemoteArbiterLookupLabel: remoteArbiter.Name,
				}
				err := targetK8sClient.List(ctx, serviceList, namespaceSelector, labelSelector)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(serviceList.Items).To(HaveLen(1))
				g.Expect(serviceList.Items[0].Spec.Type).To(Equal(corev1.ServiceTypeLoadBalancer))
				arbiterService = &serviceList.Items[0]
			}, Timeout, Interval).Should(Succeed())

			// Assign the load balancer ingress address, as the cloud
			// controller manager would
			Eventually(func(g Gomega) {
				service := &corev1.Service{}
				err := targetK8sClient.Get(ctx, client.ObjectKeyFromObject(arbiterService), service)
				g.Expect(err).NotTo(HaveOccurred())
				service.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
					{IP: "192.0.2.20"},
				}
				g.Expect(targetK8sClient.Status().Update(ctx, service)).To(Succeed())
			}, Timeout, Interval).Should(Succeed())

			// The arbiter deployment is created with the load balancer
			// address as public address
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
				g.Expect(deploymentPublicAddress(&arbiterDeploymentList.Items[0])).To(Equal("192.0.2.20"))
				arbiterDeployment = &arbiterDeploymentList.Items[0]
			}, Timeout, Interval).Should(Succeed())

			// Simulate arbiter deployment becoming ready. The controller may
			// update the deployment concurrently, so tolerate update conflicts.
			Eventually(func(g Gomega) {
				deployment := &appsv1.Deployment{}
				err := targetK8sClient.Get(ctx, client.ObjectKeyFromObject(arbiterDeployment), deployment)
				g.Expect(err).NotTo(HaveOccurred())
				deployment.Status.UpdatedReplicas = 1
				deployment.Status.Replicas = 1
				g.Expect(targetK8sClient.Status().Update(ctx, deployment)).To(Succeed())
			}, Timeout, Interval).Should(Succeed())

			// Verify the arbiter reaches the Ready state
			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterReadyState))
			}, Timeout, Interval).Should(Succeed())

			// The load balancer is re-allocated to a different address. No
			// source cluster event happens, so the periodic reconcile must
			// catch the drift and rebuild the deployment
			Eventually(func(g Gomega) {
				service := &corev1.Service{}
				err := targetK8sClient.Get(ctx, client.ObjectKeyFromObject(arbiterService), service)
				g.Expect(err).NotTo(HaveOccurred())
				service.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
					{IP: "192.0.2.21"},
				}
				g.Expect(targetK8sClient.Status().Update(ctx, service)).To(Succeed())
			}, Timeout, Interval).Should(Succeed())

			// The deployment is rebuilt with the new load balancer address
			Eventually(func(g Gomega) {
				arbiterDeploymentList := &appsv1.DeploymentList{}
				namespaceSelector := client.InNamespace(remoteCluster.Spec.Namespace)
				labelSelector := client.MatchingLabels{
					RemoteArbiterLookupLabel: remoteArbiter.Name,
				}
				err := targetK8sClient.List(ctx, arbiterDeploymentList, namespaceSelector, labelSelector)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(arbiterDeploymentList.Items).To(HaveLen(1))
				g.Expect(deploymentPublicAddress(&arbiterDeploymentList.Items[0])).To(Equal("192.0.2.21"))
			}, Timeout, Interval).Should(Succeed())

			// Verify the arbiter returns to the Ready state
			Eventually(func(g Gomega) {
				err := sourceK8sClient.Get(ctx, remoteArbiterNamespacedName, remoteArbiter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteArbiter.Status.State).To(Equal(v1alpha1.RemoteArbiterReadyState))
				g.Expect(remoteArbiter.Status.Message).NotTo(BeEmpty())
				g.Expect(remoteArbiter.Status.Conditions).To(HaveLen(len(conditionsMap)))
				for _, condition := range remoteArbiter.Status.Conditions {
					expectedCondition := conditionsMap[condition.Type]
					g.Expect(condition.Status).To(Equal(expectedCondition))
				}
			}, Timeout, Interval).Should(Succeed())
		})
	})
})

// expectArbiterServicePorts asserts the full Ceph messenger port
// configuration of the arbiter service, including the pinned node ports
// when the service is of type NodePort.
func expectArbiterServicePorts(service *corev1.Service, pinnedNodePorts bool) {
	Expect(service.Spec.Ports).To(HaveLen(2))
	portsByName := make(map[string]corev1.ServicePort, len(service.Spec.Ports))
	for _, port := range service.Spec.Ports {
		portsByName[port.Name] = port
	}

	for name, number := range map[string]int32{"tcp-msgr1": 6789, "tcp-msgr2": 3300} {
		port, ok := portsByName[name]
		Expect(ok).To(BeTrue(), "service port %s should exist", name)
		Expect(port.Port).To(Equal(number), "service port %s should have number %d", name, number)
		Expect(port.Protocol).To(Equal(corev1.ProtocolTCP), "service port %s should use TCP", name)
		Expect(port.TargetPort).To(Equal(intstr.FromInt32(number)), "service port %s should target port %d", name, number)
		if pinnedNodePorts {
			Expect(port.NodePort).To(Equal(number), "service port %s should have node port %d pinned", name, number)
		} else {
			Expect(port.NodePort).To(BeZero(), "service port %s should not have a node port", name)
		}
	}
}
