// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestRewriteProbeSockets(t *testing.T) {
	execProbe := func(command ...string) *corev1.Probe {
		return &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{Command: command},
			},
		}
	}

	tests := []struct {
		name      string
		container corev1.Container
		monID     string
		want      []string // expected command of the modified probe (nil = unchanged/absent)
	}{
		{
			name: "mon socket is rewritten to arbiter id",
			container: corev1.Container{
				LivenessProbe: execProbe("env", "-i", "sh", "-c",
					"ceph --admin-daemon /run/ceph/ceph-mon.c.asok mon_status"),
			},
			monID: "ext-a",
			want:  []string{"env", "-i", "sh", "-c", "ceph --admin-daemon /run/ceph/ceph-mon.ext-a.asok mon_status"},
		},
		{
			name: "cluster prefixed socket is rewritten",
			container: corev1.Container{
				StartupProbe: execProbe("sh", "-c", "ceph --admin-daemon /run/ceph/rook-ceph-mon.a.asok mon_status"),
			},
			monID: "ext-a",
			want:  []string{"sh", "-c", "ceph --admin-daemon /run/ceph/rook-ceph-mon.ext-a.asok mon_status"},
		},
		{
			name: "non-exec probes are untouched",
			container: corev1.Container{
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{Path: "/health"},
					},
				},
			},
			monID: "ext-a",
			want:  nil,
		},
		{
			name: "commands without a socket are unchanged",
			container: corev1.Container{
				ReadinessProbe: execProbe("sh", "-c", "echo ok"),
			},
			monID: "ext-a",
			want:  []string{"sh", "-c", "echo ok"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.container.DeepCopy()
			rewriteProbeSockets(&tt.container, tt.monID)

			if tt.want == nil {
				if tt.container.LivenessProbe != nil && tt.container.LivenessProbe.HTTPGet == nil {
					t.Fatalf("expected non-exec liveness probe to remain untouched")
				}
				return
			}

			var got []string
			probe := tt.container.LivenessProbe
			if probe == nil {
				probe = tt.container.StartupProbe
			}
			if probe == nil {
				probe = tt.container.ReadinessProbe
			}
			if probe == nil || probe.Exec == nil {
				t.Fatalf("expected an exec probe, got none")
			}
			got = probe.Exec.Command

			if len(got) != len(tt.want) {
				t.Fatalf("command length mismatch: got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("command[%d] mismatch: got %q, want %q", i, got[i], tt.want[i])
				}
			}

			// untouched parts of the container must remain identical
			if string(before.Name) != tt.container.Name {
				t.Fatalf("container name must not change")
			}
		})
	}
}
