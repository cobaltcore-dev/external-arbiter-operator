// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// Package builders provides typed fixture builders for the controller tests.
//
// These builders replace the previously captured YAML fixtures under
// contrib/k8s/test (mon-deployment.yaml, env-var-secret.yaml,
// keyring-secret.yaml, override-configmap.yaml). They reproduce only the
// fields the RemoteArbiter controller actually consumes when reconciling a
// Rook Ceph monitor into an external arbiter, so the controller envtest suite
// exercises the same code paths without reading files at test time.
package builders

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Default values mirroring the captured Rook monitor fixtures.
const (
	defaultMonName        = "rook-ceph-mon-a"
	defaultPartOf         = "my-cluster"
	defaultFSID           = "03581802-01f9-44c6-9af0-cd8fb849d036"
	defaultImage          = "quay.io/ceph/ceph:v19"
	defaultEnvVarSecret   = "rook-ceph-config"
	defaultKeyringSecret  = "rook-ceph-mons-keyring"
	defaultOverrideConfig = "rook-config-override"

	// keyring value copied verbatim from the captured keyring-secret.yaml.
	defaultKeyringData = "Clttb24uXQoJa2V5ID0gQVFCVm1RTnAxSm94SkJBQW5GUHVZUGZhcUZORkJIWUhCb0N0VGc9PQoJY2FwcyBtb24gPSAiYWxsb3cgKiIKCgpbY2xpZW50LmFkbWluXQoJa2V5ID0gQVFCVm1RTnAxbEF2SlJBQVp0V3ZZdDZnYXpPMWdTR1M5SEtYSFE9PQoJY2FwcyBtZHMgPSAiYWxsb3cgKiIKCWNhcHMgbW9uID0gImFsbG93ICoiCgljYXBzIG9zZCA9ICJhbGxvdyAqIgoJY2FwcyBtZ3IgPSAiYWxsb3cgKiIK"
)

type options struct {
	namespace string
	name      string
	partOf    string
}

// Option customizes a builder.
type Option func(*options)

// WithNamespace sets the object namespace.
func WithNamespace(ns string) Option { return func(o *options) { o.namespace = ns } }

// WithName overrides the object name.
func WithName(name string) Option { return func(o *options) { o.name = name } }

// WithPartOf sets the app.kubernetes.io/part-of label (the CephCluster name the
// controller selects monitor deployments by).
func WithPartOf(partOf string) Option { return func(o *options) { o.partOf = partOf } }

func resolve(name, partOf string, opts []Option) options {
	o := options{name: name, partOf: partOf}
	for _, apply := range opts {
		apply(&o)
	}
	return o
}

// MonitorDeployment builds a Rook Ceph monitor Deployment faithful to the
// fields the controller reads: the ceph_daemon_type/part-of selector labels, a
// "mon" container carrying the --fsid arg and the ROOK_CEPH_MON_HOST env var
// (sourced from the env-var secret), and the three named volumes
// (rook-config-override, rook-ceph-mons-keyring, ceph-daemon-data) it rewrites.
func MonitorDeployment(opts ...Option) *appsv1.Deployment {
	o := resolve(defaultMonName, defaultPartOf, opts)

	labels := map[string]string{
		"app":                       "rook-ceph-mon",
		"app.kubernetes.io/part-of": o.partOf,
		"ceph_daemon_type":          "mon",
		"mon":                       "a",
	}
	podLabels := map[string]string{
		"app": "rook-ceph-mon",
		"mon": "a",
	}

	monContainer := corev1.Container{
		Name:  "mon",
		Image: defaultImage,
		Args: []string{
			"--fsid=" + defaultFSID,
			"--id=a",
			"--public-addr=10.101.147.96",
			"--setuser-match-path=/var/lib/ceph/mon/ceph-a/store.db",
			"--public-bind-addr=$(ROOK_POD_IP)",
		},
		Command: []string{"ceph-mon"},
		Env: []corev1.EnvVar{
			{
				Name: "ROOK_CEPH_MON_HOST",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:                  "mon_host",
						LocalObjectReference: corev1.LocalObjectReference{Name: defaultEnvVarSecret},
					},
				},
			},
			{
				Name: "ROOK_CEPH_MON_INITIAL_MEMBERS",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:                  "mon_initial_members",
						LocalObjectReference: corev1.LocalObjectReference{Name: defaultEnvVarSecret},
					},
				},
			},
			{
				Name: "ROOK_POD_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
				},
			},
		},
		VolumeMounts: monVolumeMounts(),
	}

	chownContainer := corev1.Container{
		Name:         "chown-container-data-dir",
		Image:        defaultImage,
		Command:      []string{"chown"},
		VolumeMounts: monVolumeMounts(),
	}

	defaultMode := int32(420)
	configMode := int32(292)
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      o.name,
			Namespace: o.namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: podLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
				Spec: corev1.PodSpec{
					Containers:     []corev1.Container{monContainer},
					InitContainers: []corev1.Container{chownContainer},
					Volumes: []corev1.Volume{
						{
							Name: "rook-config-override",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									DefaultMode: &defaultMode,
									Sources: []corev1.VolumeProjection{
										{
											ConfigMap: &corev1.ConfigMapProjection{
												LocalObjectReference: corev1.LocalObjectReference{Name: defaultOverrideConfig},
												Items: []corev1.KeyToPath{
													{Key: "config", Path: "ceph.conf", Mode: &configMode},
												},
											},
										},
									},
								},
							},
						},
						{
							Name: "rook-ceph-mons-keyring",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									DefaultMode: &defaultMode,
									SecretName:  defaultKeyringSecret,
								},
							},
						},
						{
							Name: "ceph-daemon-data",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/rook/mon-a/data"},
							},
						},
					},
				},
			},
		},
	}
}

func monVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "rook-config-override", MountPath: "/etc/ceph", ReadOnly: true},
		{Name: "rook-ceph-mons-keyring", MountPath: "/etc/ceph/keyring-store/", ReadOnly: true},
		{Name: "ceph-daemon-data", MountPath: "/var/lib/ceph/mon/ceph-a"},
	}
}

// MonitorEnvVarSecret builds the rook-ceph-config secret holding mon_host and
// mon_initial_members, referenced by the monitor deployment's env vars.
func MonitorEnvVarSecret(opts ...Option) *corev1.Secret {
	o := resolve(defaultEnvVarSecret, "", opts)
	return &corev1.Secret{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: o.name, Namespace: o.namespace},
		Data: map[string][]byte{
			// v2:10.101.147.96:3300,v1:10.101.147.96:6789
			"mon_host":            []byte("[v2:10.101.147.96:3300,v1:10.101.147.96:6789]"),
			"mon_initial_members": []byte("a"),
		},
	}
}

// MonitorKeyringSecret builds the rook-ceph-mons-keyring secret referenced by
// the monitor deployment's keyring volume.
func MonitorKeyringSecret(opts ...Option) *corev1.Secret {
	o := resolve(defaultKeyringSecret, "", opts)
	return &corev1.Secret{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: o.name, Namespace: o.namespace},
		Data:       map[string][]byte{"keyring": []byte(defaultKeyringData)},
	}
}

// MonitorOverrideConfigMap builds the rook-config-override ConfigMap referenced
// by the monitor deployment's projected config volume.
func MonitorOverrideConfigMap(opts ...Option) *corev1.ConfigMap {
	o := resolve(defaultOverrideConfig, "", opts)
	return &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: o.name, Namespace: o.namespace},
		Data:       map[string]string{"config": ""},
	}
}

func ptr[T any](v T) *T { return &v }
