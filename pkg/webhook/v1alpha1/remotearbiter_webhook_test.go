// Copyright 2026 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"
)

var _ = Describe("RemoteArbiter Webhook", func() {
	var (
		validator RemoteArbiterCustomValidator
		defaulter RemoteArbiterCustomDefaulter
	)

	BeforeEach(func() {
		validator = RemoteArbiterCustomValidator{}
		defaulter = RemoteArbiterCustomDefaulter{}
	})

	Context("When creating RemoteArbiter under Defaulting Webhook", func() {
		It("Should apply defaults when correspondign fields are empty", func() {
			remoteArbiter := &v1alpha1.RemoteArbiter{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-namespace",
				},
				Spec: v1alpha1.RemoteArbiterSpec{
					CephCluster: v1alpha1.NamespacedReference{
						Name: "ceph-cluster",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Name: "remote-cluster",
					},
				},
			}

			err := defaulter.Default(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			Expect(remoteArbiter.Spec.CheckInterval).NotTo(BeNil())
			Expect(remoteArbiter.Spec.CheckInterval.Duration).To(Equal(time.Minute))

			Expect(remoteArbiter.Spec.CephCluster.Namespace).To(Equal(remoteArbiter.Namespace))

			Expect(remoteArbiter.Spec.MonIDPrefix).To(Equal(DefaultMonIDPrefix))
		})

		It("Should not apply defaults when values are set", func() {
			remoteArbiter := &v1alpha1.RemoteArbiter{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-namespace",
				},
				Spec: v1alpha1.RemoteArbiterSpec{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "ceph-cluster",
						Namespace: "ceph-cluster-namespace",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Name: "remote-cluster",
					},
					MonIDPrefix: "custom-prefix",
				},
			}

			remoteArbiterCopy := remoteArbiter.DeepCopy()

			err := defaulter.Default(ctx, remoteArbiter)
			Expect(err).NotTo(HaveOccurred())

			Expect(remoteArbiter.Spec.CheckInterval.Duration).To(Equal(remoteArbiterCopy.Spec.CheckInterval.Duration))
			Expect(remoteArbiter.Spec.CephCluster.Namespace).To(Equal(remoteArbiterCopy.Spec.CephCluster.Namespace))
			Expect(remoteArbiter.Spec.MonIDPrefix).To(Equal(remoteArbiterCopy.Spec.MonIDPrefix))
		})
	})

	Context("When creating or updating RemoteArbiter under Validating Webhook", func() {
		It("Should deny creation if spec is not valid", func() {
			invalidSpecs := []v1alpha1.RemoteArbiterSpec{
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: -time.Hour,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "ceph-cluster",
						Namespace: "ceph-cluster-namespace",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Name: "remote-cluster",
					},
					MonIDPrefix: "custom-prefix",
				},
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "",
						Namespace: "ceph-cluster-namespace",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Name: "remote-cluster",
					},
					MonIDPrefix: "custom-prefix",
				},
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "ceph-cluster",
						Namespace: "ceph-cluster-namespace",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{},
					MonIDPrefix:   "custom-prefix",
				},
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      ".notdnslabel",
						Namespace: "ceph-cluster-namespace",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Name: "remote-cluster",
					},
					MonIDPrefix: "custom-prefix",
				},
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "normal",
						Namespace: ".notdnslabel",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Name: "remote-cluster",
					},
					MonIDPrefix: "custom-prefix",
				},
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "normal",
						Namespace: "normal",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Name: ".notdnslabel",
					},
					MonIDPrefix: "custom-prefix",
				},
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "normal",
						Namespace: "normal",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Name: "normal",
					},
					MonIDPrefix: ".notdnslabel",
				},
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "normal",
						Namespace: "normal",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Spec: &v1alpha1.RemoteClusterSpec{
							CheckInterval: &v1alpha1.Interval{
								Duration: -1,
							},
						},
					},
					MonIDPrefix: ".notdnslabel",
				},
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "normal",
						Namespace: "normal",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Spec: &v1alpha1.RemoteClusterSpec{
							CheckInterval: &v1alpha1.Interval{
								Duration: -1,
							},
						},
					},
					Service: &v1alpha1.ServiceConfiguration{
						Type: corev1.ServiceTypeNodePort,
					},
					MonIDPrefix: "normal",
				},
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "normal",
						Namespace: "normal",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Spec: &v1alpha1.RemoteClusterSpec{
							CheckInterval: &v1alpha1.Interval{
								Duration: -1,
							},
						},
					},
					Service: &v1alpha1.ServiceConfiguration{
						Type:   corev1.ServiceTypeNodePort,
						NodeIP: "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
					},
					MonIDPrefix: "normal",
				},
			}

			for idx, invalidSpec := range invalidSpecs {
				remoteArbiter := &v1alpha1.RemoteArbiter{
					Spec: invalidSpec,
				}
				err := defaulter.Default(ctx, remoteArbiter)
				Expect(err).NotTo(HaveOccurred())
				By(fmt.Sprintf("validating spec %d", idx))
				_, err = validator.ValidateCreate(ctx, remoteArbiter)
				Expect(err).To(HaveOccurred())
				_, err = validator.ValidateUpdate(ctx, nil, remoteArbiter)
				Expect(err).To(HaveOccurred())
				_, err = validator.ValidateDelete(ctx, remoteArbiter)
				Expect(err).To(HaveOccurred())
			}
		})

		It("Should allow creation if all required fields are present", func() {
			validSpecs := []v1alpha1.RemoteArbiterSpec{
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "normal",
						Namespace: "normal",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Name: "normal",
					},
					MonIDPrefix: "normal",
				},
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "normal",
						Namespace: "normal",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Spec: &v1alpha1.RemoteClusterSpec{
							Timeout: &v1alpha1.Interval{
								Duration: 0,
							},
							CheckInterval: &v1alpha1.Interval{
								Duration: 0,
							},
							AccessKeyRef: v1alpha1.KubeconfigSecretSource{
								Name: "normal",
								Key:  "any",
							},
							Namespace: "noraml",
						},
					},
					MonIDPrefix: "normal",
				},
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "normal",
						Namespace: "normal",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Spec: &v1alpha1.RemoteClusterSpec{
							Timeout: &v1alpha1.Interval{
								Duration: 0,
							},
							CheckInterval: &v1alpha1.Interval{
								Duration: 0,
							},
							AccessKeyRef: v1alpha1.KubeconfigSecretSource{
								Name: "normal",
								Key:  "any",
							},
							Namespace: "noraml",
						},
					},
					Service: &v1alpha1.ServiceConfiguration{
						Type: corev1.ServiceTypeClusterIP,
					},
					MonIDPrefix: "normal",
				},
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					CephCluster: v1alpha1.NamespacedReference{
						Name:      "normal",
						Namespace: "normal",
					},
					RemoteCluster: v1alpha1.RemoteClusterConfiguration{
						Spec: &v1alpha1.RemoteClusterSpec{
							Timeout: &v1alpha1.Interval{
								Duration: 0,
							},
							CheckInterval: &v1alpha1.Interval{
								Duration: 0,
							},
							AccessKeyRef: v1alpha1.KubeconfigSecretSource{
								Name: "normal",
								Key:  "any",
							},
							Namespace: "noraml",
						},
					},
					Service: &v1alpha1.ServiceConfiguration{
						Type:   corev1.ServiceTypeNodePort,
						NodeIP: "10.10.0.1",
					},
					MonIDPrefix: "normal",
				},
			}

			for idx, validSpec := range validSpecs {
				remoteArbiter := &v1alpha1.RemoteArbiter{
					Spec: validSpec,
				}
				By(fmt.Sprintf("validating spec %d", idx))
				_, err := validator.ValidateCreate(ctx, remoteArbiter)
				Expect(err).NotTo(HaveOccurred())
				_, err = validator.ValidateUpdate(ctx, nil, remoteArbiter)
				Expect(err).NotTo(HaveOccurred())
				_, err = validator.ValidateDelete(ctx, remoteArbiter)
				Expect(err).NotTo(HaveOccurred())
			}
		})
	})
})
