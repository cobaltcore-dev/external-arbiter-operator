// Copyright 2026 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"
)

var _ = Describe("RemoteCluster Webhook", func() {
	var (
		validator RemoteClusterCustomValidator
		defaulter RemoteClusterCustomDefaulter
	)

	BeforeEach(func() {
		validator = RemoteClusterCustomValidator{}
		defaulter = RemoteClusterCustomDefaulter{}
	})

	Context("When creating RemoteCluster under Defaulting Webhook", func() {
		It("Should apply defaults when correspondign fields are empty", func() {
			remoteCluster := &v1alpha1.RemoteCluster{
				ObjectMeta: v1.ObjectMeta{
					Name: "test",
				},
			}

			err := defaulter.Default(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			Expect(remoteCluster.Spec.CheckInterval).NotTo(BeNil())
			Expect(remoteCluster.Spec.CheckInterval.Duration).To(Equal(time.Minute))

			Expect(remoteCluster.Spec.Timeout).NotTo(BeNil())
			Expect(remoteCluster.Spec.Timeout.Duration).To(Equal(time.Second * 10))

			Expect(remoteCluster.Spec.AccessKeyRef.Key).To(Equal(DefaultKubeconfigSecretKey))
			Expect(remoteCluster.Spec.AccessKeyRef.Name).To(Equal(remoteCluster.Name))

			Expect(remoteCluster.Spec.Namespace).To(Equal(DefaultNamespace))
		})

		It("Should not apply defaults when values are set", func() {
			remoteCluster := &v1alpha1.RemoteCluster{
				ObjectMeta: v1.ObjectMeta{
					Name: "test",
				},
				Spec: v1alpha1.RemoteClusterSpec{
					Timeout: &v1alpha1.Interval{
						Duration: 0,
					},
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					AccessKeyRef: v1alpha1.KubeconfigSecretSource{
						Key:  "secret-key",
						Name: "secret-name",
					},
					Namespace: "arbiter-namespace",
				},
			}

			remoteClusterCopy := remoteCluster.DeepCopy()

			err := defaulter.Default(ctx, remoteCluster)
			Expect(err).NotTo(HaveOccurred())

			Expect(remoteCluster.Spec.CheckInterval.Duration).To(Equal(remoteClusterCopy.Spec.CheckInterval.Duration))

			Expect(remoteCluster.Spec.Timeout.Duration).To(Equal(remoteClusterCopy.Spec.Timeout.Duration))

			Expect(remoteCluster.Spec.AccessKeyRef.Key).To(Equal(remoteClusterCopy.Spec.AccessKeyRef.Key))
			Expect(remoteCluster.Spec.AccessKeyRef.Name).To(Equal(remoteClusterCopy.Spec.AccessKeyRef.Name))

			Expect(remoteCluster.Spec.Namespace).To(Equal(remoteClusterCopy.Spec.Namespace))
		})
	})

	Context("When creating or updating RemoteCluster under Validating Webhook", func() {
		It("Should deny creation if spec is not valid", func() {
			invalidSpecs := []v1alpha1.RemoteClusterSpec{
				{
					Timeout: &v1alpha1.Interval{
						Duration: -time.Hour,
					},
				},
				{
					CheckInterval: &v1alpha1.Interval{
						Duration: -time.Hour,
					},
				},
				{
					Namespace: ".notadnslabel",
				},
				{
					AccessKeyRef: v1alpha1.KubeconfigSecretSource{
						Name: ".notadnslabel",
					},
				},
			}

			for idx, invalidSpec := range invalidSpecs {
				remoteCluster := &v1alpha1.RemoteCluster{
					Spec: invalidSpec,
				}
				err := defaulter.Default(ctx, remoteCluster)
				Expect(err).NotTo(HaveOccurred())
				By(fmt.Sprintf("validating spec %d", idx))
				_, err = validator.ValidateCreate(ctx, remoteCluster)
				Expect(err).To(HaveOccurred())
				_, err = validator.ValidateUpdate(ctx, nil, remoteCluster)
				Expect(err).To(HaveOccurred())
				_, err = validator.ValidateDelete(ctx, remoteCluster)
				Expect(err).To(HaveOccurred())
			}
		})

		It("Should allow creation if all required fields are present", func() {
			validSpecs := []v1alpha1.RemoteClusterSpec{
				{
					Timeout: &v1alpha1.Interval{
						Duration: time.Hour,
					},
					CheckInterval: &v1alpha1.Interval{
						Duration: time.Microsecond,
					},
					AccessKeyRef: v1alpha1.KubeconfigSecretSource{
						Name: "valid-k8s-name",
						Key:  "any.key",
					},
					Namespace: "valid-k8s-name",
				},
				{
					Timeout: &v1alpha1.Interval{
						Duration: 0,
					},
					CheckInterval: &v1alpha1.Interval{
						Duration: 0,
					},
					AccessKeyRef: v1alpha1.KubeconfigSecretSource{
						Name: "valid-k8s-name",
						Key:  "any.key",
					},
					Namespace: "valid-k8s-name",
				},
			}

			for idx, validSpec := range validSpecs {
				remoteCluster := &v1alpha1.RemoteCluster{
					Spec: validSpec,
				}
				By(fmt.Sprintf("validating spec %d", idx))
				_, err := validator.ValidateCreate(ctx, remoteCluster)
				Expect(err).NotTo(HaveOccurred())
				_, err = validator.ValidateUpdate(ctx, nil, remoteCluster)
				Expect(err).NotTo(HaveOccurred())
				_, err = validator.ValidateDelete(ctx, remoteCluster)
				Expect(err).NotTo(HaveOccurred())
			}
		})
	})
})
