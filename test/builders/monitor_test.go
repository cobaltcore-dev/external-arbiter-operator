// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package builders

import (
	"strings"
	"testing"
)

// TestMonitorDeploymentContract asserts the fields the RemoteArbiter controller
// relies on when reconciling a monitor deployment. If a builder change breaks
// one of these, the controller envtest suite would fail far less obviously.
func TestMonitorDeploymentContract(t *testing.T) {
	d := MonitorDeployment(WithNamespace("rook-ceph"), WithPartOf("prod"))

	if got := d.Labels["ceph_daemon_type"]; got != "mon" {
		t.Errorf("ceph_daemon_type label = %q, want mon", got)
	}
	if got := d.Labels["app.kubernetes.io/part-of"]; got != "prod" {
		t.Errorf("part-of label = %q, want prod", got)
	}

	containers := d.Spec.Template.Spec.Containers
	if len(containers) != 1 || containers[0].Name != "mon" {
		t.Fatalf("expected single mon container, got %+v", containers)
	}
	mon := containers[0]
	if mon.Image == "" {
		t.Error("mon container image must be set (controller reads it)")
	}

	var hasFSID bool
	for _, a := range mon.Args {
		if len(a) >= 7 && a[:7] == "--fsid=" && len(a) > 7 {
			hasFSID = true
		}
	}
	if !hasFSID {
		t.Error("mon container must carry a non-empty --fsid arg")
	}

	// modifyContainers rewrites these args in place by prefix match; a rewrite of
	// an absent arg silently no-ops, producing a wrong Deployment with no error.
	// Assert the builder carries the prefixes the controller expects to find.
	for _, prefix := range []string{"--id=", "--public-addr=", "--setuser-match-path="} {
		var found bool
		for _, a := range mon.Args {
			if strings.HasPrefix(a, prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("mon container must carry a %q arg for the controller to rewrite", prefix)
		}
	}

	var envSecret string
	for _, e := range mon.Env {
		if e.Name == "ROOK_CEPH_MON_HOST" {
			if e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil {
				t.Fatal("ROOK_CEPH_MON_HOST must source from a secretKeyRef")
			}
			envSecret = e.ValueFrom.SecretKeyRef.Name
		}
	}
	if envSecret == "" {
		t.Error("mon container must expose ROOK_CEPH_MON_HOST from the env-var secret")
	}

	// The chown initContainer must carry a /var/lib/ceph/mon/ceph-… path arg:
	// modifyContainers rewrites it to the arbiter's mon ID by prefix match, and
	// an absent arg makes that rewrite silently no-op. Guard against the fixture
	// regressing to an initContainer with no rewritable path.
	initContainers := d.Spec.Template.Spec.InitContainers
	if len(initContainers) != 1 || initContainers[0].Name != "chown-container-data-dir" {
		t.Fatalf("expected single chown initContainer, got %+v", initContainers)
	}
	var hasMonPath bool
	for _, a := range initContainers[0].Args {
		if strings.HasPrefix(a, "/var/lib/ceph/mon/ceph-") {
			hasMonPath = true
			break
		}
	}
	if !hasMonPath {
		t.Error("chown initContainer must carry a /var/lib/ceph/mon/ceph- path arg for the controller to rewrite")
	}

	vols := map[string]bool{}
	for _, v := range d.Spec.Template.Spec.Volumes {
		vols[v.Name] = true
		switch v.Name {
		case "rook-config-override":
			if v.Projected == nil || len(v.Projected.Sources) != 1 || v.Projected.Sources[0].ConfigMap == nil {
				t.Error("rook-config-override must be a projected volume with one configMap source")
			}
		case "rook-ceph-mons-keyring":
			if v.Secret == nil || v.Secret.SecretName == "" {
				t.Error("rook-ceph-mons-keyring must reference a secret by name")
			}
		case "ceph-daemon-data":
			if v.HostPath == nil {
				t.Error("ceph-daemon-data must be a hostPath volume")
			}
		}
	}
	for _, name := range []string{"rook-config-override", "rook-ceph-mons-keyring", "ceph-daemon-data"} {
		if !vols[name] {
			t.Errorf("missing required volume %q", name)
		}
	}
}

func TestMonitorSecretsAndConfigMap(t *testing.T) {
	if s := MonitorEnvVarSecret(); s.Data["mon_host"] == nil || s.Data["mon_initial_members"] == nil {
		t.Error("env-var secret must carry mon_host and mon_initial_members")
	}
	if s := MonitorKeyringSecret(); s.Data["keyring"] == nil {
		t.Error("keyring secret must carry a keyring key")
	}
	if c := MonitorOverrideConfigMap(); c.Data == nil {
		t.Error("override configmap must have a data map")
	}
}
