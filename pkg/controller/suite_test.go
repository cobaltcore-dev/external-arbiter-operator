// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"

	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

const (
	ArbiterInstallationNamespaceName = "target"

	Timeout  = time.Second * 30
	Interval = time.Millisecond * 250
)

var (
	ctx                       context.Context
	cancel                    context.CancelFunc
	sourceClusterTestEnv      *envtest.Environment
	targetClusterTestEnv      *envtest.Environment
	sourceCfg                 *rest.Config
	targetCfg                 *rest.Config
	sourceK8sClient           client.Client
	targetK8sClient           client.Client
	sourceK8sManager          ctrl.Manager
	arbiterInstallerK8sClient client.Client
	arbiterInstallerUser      *envtest.AuthenticatedUser
	noPermissionsUser         *envtest.AuthenticatedUser

	refMonitorOverrideConfigMap *corev1.ConfigMap
	refMonitorEnvVarSecret      *corev1.Secret
	refMonitorKeyringSecret     *corev1.Secret
	refMonitorDeployment        *appsv1.Deployment
)

func freePort() (int, error) {
	address, err := net.ResolveTCPAddr("tcp", "0.0.0.0:0")
	if err != nil {
		return 0, err
	}
	listener, err := net.ListenTCP("tcp", address)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = listener.Close()
	}()
	tcpAddress, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("not a tcp address")
	}
	return tcpAddress.Port, nil
}

func namespaceEmpty(k8sClient client.Client, clusterTypes []client.Object, namespace string) (bool, error) {
	for _, clusterType := range clusterTypes {
		gvk, err := k8sClient.GroupVersionKindFor(clusterType)
		if err != nil {
			return false, fmt.Errorf("unable to get gvk: %w", err)
		}

		objectList := &unstructured.UnstructuredList{}
		objectList.SetGroupVersionKind(gvk)
		err = k8sClient.List(ctx, objectList, &client.ListOptions{Namespace: namespace})
		if err != nil {
			return false, fmt.Errorf("unable to list resources: %w", err)
		}

		if len(objectList.Items) != 0 {
			return false, nil
		}
	}
	return true, nil
}

func namespaceCleanUp(k8sClient client.Client, clusterTypes []client.Object, namespace string) error {
	for _, clusterType := range clusterTypes {
		gvk, err := k8sClient.GroupVersionKindFor(clusterType)
		if err != nil {
			return fmt.Errorf("unable to get gvk: %w", err)
		}

		objectList := &unstructured.UnstructuredList{}
		objectList.SetGroupVersionKind(gvk)
		err = k8sClient.List(ctx, objectList, &client.ListOptions{Namespace: namespace})
		if err != nil {
			return fmt.Errorf("unable to list resources: %w", err)
		}

		for _, item := range objectList.Items {
			if err = k8sClient.Delete(ctx, &item); err != nil {
				return fmt.Errorf("unable to delete resource: %w", err)
			}
		}
	}
	return nil
}

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	err = v1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = rookv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	By("bootstrapping test environment")
	sourceClusterTestEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "contrib", "k8s", "crd"),
			filepath.Join("..", "..", "contrib", "k8s", "3rdparty"),
		},
		ErrorIfCRDPathMissing: true,
	}

	targetClusterTestEnv = &envtest.Environment{}

	// Retrieve the first found binary directory to allow running tests from IDEs
	envTestBinaryDir := getFirstFoundEnvTestBinaryDir()
	if envTestBinaryDir != "" {
		sourceClusterTestEnv.BinaryAssetsDirectory = envTestBinaryDir
		targetClusterTestEnv.BinaryAssetsDirectory = envTestBinaryDir
	}

	// cfg is defined in this file globally.
	sourceCfg, err = sourceClusterTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(sourceCfg).NotTo(BeNil())

	targetCfg, err = targetClusterTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(sourceCfg).NotTo(BeNil())

	sourceK8sClient, err = client.New(sourceCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(sourceK8sClient).NotTo(BeNil())

	targetK8sClient, err = client.New(targetCfg, client.Options{})
	Expect(err).NotTo(HaveOccurred())
	Expect(sourceK8sClient).NotTo(BeNil())

	sourceK8sManager, err = ctrl.NewManager(sourceCfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	remoteClusterReconciler := &RemoteClusterReconciler{
		Client: sourceK8sManager.GetClient(),
		Scheme: sourceK8sManager.GetScheme(),
	}
	err = remoteClusterReconciler.SetupWithManager(sourceK8sManager)
	Expect(err).NotTo(HaveOccurred())

	remoteArbiterReconciler := &RemoteArbiterReconciler{
		Client: sourceK8sManager.GetClient(),
		Scheme: sourceK8sManager.GetScheme(),
	}
	err = remoteArbiterReconciler.SetupWithManager(sourceK8sManager)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err := sourceK8sManager.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()

	noPermissionsUserName := "no-permissions"
	noPermissionsUser, err = targetClusterTestEnv.AddUser(envtest.User{Name: noPermissionsUserName}, targetCfg)
	Expect(err).NotTo(HaveOccurred())

	targetNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: ArbiterInstallationNamespaceName,
		},
	}
	err = targetK8sClient.Create(ctx, targetNamespace, &client.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	arbiterInstallerRoleName := "arbiter-installer-role"
	arbiterInstallerRole := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      arbiterInstallerRoleName,
			Namespace: ArbiterInstallationNamespaceName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
				APIGroups: []string{"apps"},
				Resources: []string{"deployments"},
			},
			{
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
				APIGroups: []string{"apps"},
				Resources: []string{"deployments/status"},
			},
			{
				Verbs:     []string{"update"},
				APIGroups: []string{"apps"},
				Resources: []string{"deployments/finalizers"},
			},
			{
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
				APIGroups: []string{""},
				Resources: []string{"configmaps", "secrets", "services"},
			},
			{
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
				APIGroups: []string{""},
				Resources: []string{"configmaps/status", "secrets/status", "services/status"},
			},
			{
				Verbs:     []string{"update"},
				APIGroups: []string{""},
				Resources: []string{"configmaps/finalizers", "secrets/finalizers", "services/finalizers"},
			},
		},
	}
	err = targetK8sClient.Create(ctx, arbiterInstallerRole, &client.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	arbiterInstallerUserName := "arbiter-installer-user"
	arbiterInstallerUser, err = targetClusterTestEnv.AddUser(envtest.User{Name: arbiterInstallerUserName}, targetCfg)
	Expect(err).NotTo(HaveOccurred())

	roleGVK, err := targetK8sClient.GroupVersionKindFor(arbiterInstallerRole)
	Expect(err).NotTo(HaveOccurred())

	arbiterInstallerRoleBindingName := "arbiter-installer-rolebinding"
	arbiterIntallerRoleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      arbiterInstallerRoleBindingName,
			Namespace: ArbiterInstallationNamespaceName,
		},
		Subjects: []rbacv1.Subject{
			{
				Name: arbiterInstallerUserName,
				Kind: "User",
			},
		},
		RoleRef: rbacv1.RoleRef{
			Name:     arbiterInstallerRoleName,
			Kind:     roleGVK.Kind,
			APIGroup: roleGVK.Group,
		},
	}

	err = targetK8sClient.Create(ctx, arbiterIntallerRoleBinding, &client.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	arbiterInstallerK8sClient, err = client.New(arbiterInstallerUser.Config(), client.Options{})
	Expect(err).NotTo(HaveOccurred())
	Expect(sourceK8sClient).NotTo(BeNil())

	monitorDeploymentBytes, err := os.ReadFile("../../contrib/k8s/test/mon-deployment.yaml")
	Expect(err).NotTo(HaveOccurred())
	monitorEnvVarSecretBytes, err := os.ReadFile("../../contrib/k8s/test/env-var-secret.yaml")
	Expect(err).NotTo(HaveOccurred())
	monitorKeyringSecretBytes, err := os.ReadFile("../../contrib/k8s/test/keyring-secret.yaml")
	Expect(err).NotTo(HaveOccurred())
	monitorOverrideConfigMapBytes, err := os.ReadFile("../../contrib/k8s/test/override-configmap.yaml")
	Expect(err).NotTo(HaveOccurred())

	resourceDeserializer := serializer.NewCodecFactory(scheme.Scheme).UniversalDeserializer()

	monitorDeploymentObject, _, err := resourceDeserializer.Decode(monitorDeploymentBytes, nil, nil)
	Expect(err).NotTo(HaveOccurred())
	monitorEnvVarSecretObject, _, err := resourceDeserializer.Decode(monitorEnvVarSecretBytes, nil, nil)
	Expect(err).NotTo(HaveOccurred())
	monitorKeyringSecretObject, _, err := resourceDeserializer.Decode(monitorKeyringSecretBytes, nil, nil)
	Expect(err).NotTo(HaveOccurred())
	monitorOverrideConfigMapObject, _, err := resourceDeserializer.Decode(monitorOverrideConfigMapBytes, nil, nil)
	Expect(err).NotTo(HaveOccurred())

	var ok bool
	refMonitorDeployment, ok = monitorDeploymentObject.(*appsv1.Deployment)
	Expect(ok).To(BeTrue())
	refMonitorEnvVarSecret, ok = monitorEnvVarSecretObject.(*corev1.Secret)
	Expect(ok).To(BeTrue())
	refMonitorKeyringSecret, ok = monitorKeyringSecretObject.(*corev1.Secret)
	Expect(ok).To(BeTrue())
	refMonitorOverrideConfigMap, ok = monitorOverrideConfigMapObject.(*corev1.ConfigMap)
	Expect(ok).To(BeTrue())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := sourceClusterTestEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
	err = targetClusterTestEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Makefile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
//
// This function streamlines the process by finding the required binaries, similar to
// setting the 'KUBEBUILDER_ASSETS' environment variable. To ensure the binaries are
// properly set up, run 'make setup-envtest' beforehand.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", ".env", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
