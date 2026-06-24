/*
Copyright 2026 Red Hat.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

const (
	defaultOpenShellGatewayImage    = "ghcr.io/nvidia/openshell/gateway:latest"
	defaultOpenShellSupervisorImage = "ghcr.io/nvidia/openshell/supervisor:latest"
	defaultOpenShellSandboxImage    = "quay.io/sallyom/openclaw-openshell-sandbox:latest"
	openShellGatewayFinalizer       = "claw.sandbox.redhat.com/openshell-gateway-finalizer"
	openShellGatewayAppName         = "openshell"
	openShellGatewayFieldManager    = "claw-operator-openshell-gateway"
	openShiftPrivilegedSCCRole      = "system:openshift:scc:privileged"
)

// OpenShellGatewayReconciler reconciles an OpenShellGateway object.
type OpenShellGatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=openshellgateways,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=openshellgateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=openshellgateways/finalizers,verbs=update
// +kubebuilder:rbac:groups=agents.x-k8s.io,resources=sandboxes;sandboxes/status,verbs=create;delete;get;list;patch;update;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=privileged,verbs=use
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=create;delete;get;list;patch;update;watch

// Reconcile creates the minimal OpenShell gateway resources needed for a
// namespace-scoped, OpenShift-friendly gateway install.
func (r *OpenShellGatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	gateway := &clawv1alpha1.OpenShellGateway{}
	if err := r.Get(ctx, req.NamespacedName, gateway); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !gateway.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, gateway)
	}

	if !controllerutil.ContainsFinalizer(gateway, openShellGatewayFinalizer) {
		controllerutil.AddFinalizer(gateway, openShellGatewayFinalizer)
		if err := r.Update(ctx, gateway); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer to OpenShellGateway %s/%s: %w", gateway.Namespace, gateway.Name, err)
		}
	}

	cfg := openShellGatewayConfigFor(gateway)
	if err := r.ensureJWTSecret(ctx, gateway); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure JWT secret for OpenShellGateway %s/%s: %w", gateway.Namespace, gateway.Name, err)
	}
	for _, obj := range []client.Object{
		r.gatewayServiceAccount(gateway, cfg),
		r.sandboxServiceAccount(gateway, cfg),
		r.gatewayConfigMap(gateway, cfg),
		r.sandboxRole(gateway, cfg),
		r.sandboxRoleBinding(gateway, cfg),
		r.nodeReaderClusterRole(gateway, cfg),
		r.nodeReaderClusterRoleBinding(gateway, cfg),
		r.service(gateway, cfg),
		r.deployment(gateway, cfg),
	} {
		if err := r.applyObject(ctx, gateway, obj); err != nil {
			return ctrl.Result{}, fmt.Errorf("apply %s/%s %s: %w", obj.GetNamespace(), obj.GetName(), obj.GetObjectKind().GroupVersionKind().String(), err)
		}
	}
	if cfg.PrivilegedSandboxSCC {
		obj := r.privilegedSCCRoleBinding(gateway, cfg)
		if err := r.applyObject(ctx, gateway, obj); err != nil {
			return ctrl.Result{}, fmt.Errorf("apply %s/%s %s: %w", obj.GetNamespace(), obj.GetName(), obj.GetObjectKind().GroupVersionKind().String(), err)
		}
	}

	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: cfg.Name, Namespace: gateway.Namespace}, deployment); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, r.updateStatus(ctx, gateway, cfg, deployment)
}

func (r *OpenShellGatewayReconciler) reconcileDelete(ctx context.Context, gateway *clawv1alpha1.OpenShellGateway) error {
	cfg := openShellGatewayConfigFor(gateway)
	for _, obj := range []client.Object{
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: cfg.ClusterNodeReaderName}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: cfg.ClusterNodeReaderName}},
	} {
		if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	controllerutil.RemoveFinalizer(gateway, openShellGatewayFinalizer)
	return r.Update(ctx, gateway)
}

func (r *OpenShellGatewayReconciler) applyObject(ctx context.Context, owner *clawv1alpha1.OpenShellGateway, obj client.Object) error {
	if obj.GetNamespace() != "" {
		if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
			return err
		}
	}
	return r.Patch(ctx, obj, client.Apply, &client.PatchOptions{
		FieldManager: openShellGatewayFieldManager,
		Force:        ptrTo(true),
	})
}

func (r *OpenShellGatewayReconciler) ensureJWTSecret(ctx context.Context, owner *clawv1alpha1.OpenShellGateway) error {
	name := openShellJWTSecretName(owner.Name)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: owner.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		secret.Labels = openShellGatewayLabels(owner.Name)
		secret.Type = corev1.SecretTypeOpaque
		if !hasJWTSecretData(secret) {
			secretData, err := generateOpenShellJWTSecretData()
			if err != nil {
				return err
			}
			secret.Data = secretData
		}
		return controllerutil.SetControllerReference(owner, secret, r.Scheme)
	})
	return err
}

func (r *OpenShellGatewayReconciler) updateStatus(
	ctx context.Context,
	gateway *clawv1alpha1.OpenShellGateway,
	cfg openShellGatewayConfig,
	deployment *appsv1.Deployment,
) error {
	latest := &clawv1alpha1.OpenShellGateway{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(gateway), latest); err != nil {
		return err
	}
	ready := deployment.Status.ReadyReplicas > 0 &&
		deployment.Status.ReadyReplicas == *deployment.Spec.Replicas
	status := metav1.ConditionFalse
	reason := clawv1alpha1.ConditionReasonOpenShellGatewayProvisioning
	message := "OpenShell gateway deployment is not ready"
	if ready {
		status = metav1.ConditionTrue
		reason = clawv1alpha1.ConditionReasonOpenShellGatewayReady
		message = "OpenShell gateway deployment is ready"
	}
	latest.Status.Endpoint = cfg.Endpoint
	latest.Status.ServiceName = cfg.Name
	latest.Status.DeploymentName = cfg.Name
	latest.Status.ReadyReplicas = deployment.Status.ReadyReplicas
	latest.Status.ObservedGeneration = latest.Generation
	meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
		Type:               clawv1alpha1.ConditionTypeOpenShellGatewayReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: latest.Generation,
	})
	return r.Status().Update(ctx, latest)
}

// SetupWithManager sets up the controller with the Manager.
func (r *OpenShellGatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clawv1alpha1.OpenShellGateway{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

type openShellGatewayConfig struct {
	Name                   string
	Namespace              string
	GatewayImage           string
	SupervisorImage        string
	SandboxImage           string
	GatewayImagePullPolicy corev1.PullPolicy
	SandboxImagePullPolicy corev1.PullPolicy
	ServicePort            int32
	HealthPort             int32
	LogLevel               string
	Endpoint               string
	ClusterNodeReaderName  string
	PrivilegedSandboxSCC   bool
	ConfigHash             string
}

func openShellGatewayConfigFor(gateway *clawv1alpha1.OpenShellGateway) openShellGatewayConfig {
	name := gateway.Name
	port := gateway.Spec.ServicePort
	if port == 0 {
		port = 8080
	}
	healthPort := gateway.Spec.HealthPort
	if healthPort == 0 {
		healthPort = 8081
	}
	gatewayImage := valueOrDefault(gateway.Spec.GatewayImage, defaultOpenShellGatewayImage)
	supervisorImage := valueOrDefault(gateway.Spec.SupervisorImage, defaultOpenShellSupervisorImage)
	sandboxImage := valueOrDefault(gateway.Spec.SandboxImage, defaultOpenShellSandboxImage)
	gatewayPullPolicy := gateway.Spec.GatewayImagePullPolicy
	if gatewayPullPolicy == "" {
		gatewayPullPolicy = corev1.PullIfNotPresent
	}
	sandboxPullPolicy := gateway.Spec.SandboxImagePullPolicy
	if sandboxPullPolicy == "" {
		sandboxPullPolicy = corev1.PullIfNotPresent
	}
	logLevel := valueOrDefault(gateway.Spec.LogLevel, "info")
	endpoint := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", name, gateway.Namespace, port)
	cfg := openShellGatewayConfig{
		Name:                   name,
		Namespace:              gateway.Namespace,
		GatewayImage:           gatewayImage,
		SupervisorImage:        supervisorImage,
		SandboxImage:           sandboxImage,
		GatewayImagePullPolicy: gatewayPullPolicy,
		SandboxImagePullPolicy: sandboxPullPolicy,
		ServicePort:            port,
		HealthPort:             healthPort,
		LogLevel:               logLevel,
		Endpoint:               endpoint,
		ClusterNodeReaderName:  fmt.Sprintf("openshell-%s-%s-node-reader", gateway.Namespace, name),
		PrivilegedSandboxSCC:   gateway.Spec.OpenShift != nil && gateway.Spec.OpenShift.PrivilegedSandboxSCC,
	}
	cfg.ConfigHash = sha256Hex(renderOpenShellGatewayTOML(cfg))
	return cfg
}

func (r *OpenShellGatewayReconciler) gatewayServiceAccount(
	owner *clawv1alpha1.OpenShellGateway,
	cfg openShellGatewayConfig,
) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"},
		ObjectMeta: namespacedMeta(cfg.Name, owner.Namespace, owner.Name),
	}
}

func (r *OpenShellGatewayReconciler) sandboxServiceAccount(
	owner *clawv1alpha1.OpenShellGateway,
	cfg openShellGatewayConfig,
) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"},
		ObjectMeta: namespacedMeta(openShellSandboxServiceAccountName(owner.Name), owner.Namespace, owner.Name),
	}
}

func (r *OpenShellGatewayReconciler) gatewayConfigMap(
	owner *clawv1alpha1.OpenShellGateway,
	cfg openShellGatewayConfig,
) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      openShellConfigMapName(owner.Name),
			Namespace: owner.Namespace,
			Labels:    openShellGatewayLabels(owner.Name),
		},
		Data: map[string]string{"gateway.toml": renderOpenShellGatewayTOML(cfg)},
	}
}

func (r *OpenShellGatewayReconciler) sandboxRole(
	owner *clawv1alpha1.OpenShellGateway,
	cfg openShellGatewayConfig,
) *rbacv1.Role {
	return &rbacv1.Role{
		TypeMeta:   metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "Role"},
		ObjectMeta: namespacedMeta(openShellSandboxRoleName(owner.Name), owner.Namespace, owner.Name),
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"agents.x-k8s.io"},
				Resources: []string{"sandboxes", "sandboxes/status"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get"},
			},
		},
	}
}

func (r *OpenShellGatewayReconciler) sandboxRoleBinding(
	owner *clawv1alpha1.OpenShellGateway,
	cfg openShellGatewayConfig,
) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta:   metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "RoleBinding"},
		ObjectMeta: namespacedMeta(openShellSandboxRoleName(owner.Name), owner.Namespace, owner.Name),
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     openShellSandboxRoleName(owner.Name),
		},
		Subjects: []rbacv1.Subject{
			{Kind: "ServiceAccount", Name: cfg.Name, Namespace: owner.Namespace},
		},
	}
}

func (r *OpenShellGatewayReconciler) privilegedSCCRoleBinding(
	owner *clawv1alpha1.OpenShellGateway,
	cfg openShellGatewayConfig,
) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta:   metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "RoleBinding"},
		ObjectMeta: namespacedMeta(openShellPrivilegedSCCRoleBindingName(owner.Name), owner.Namespace, owner.Name),
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     openShiftPrivilegedSCCRole,
		},
		Subjects: []rbacv1.Subject{
			{Kind: "ServiceAccount", Name: openShellSandboxServiceAccountName(owner.Name), Namespace: owner.Namespace},
		},
	}
}

func (r *OpenShellGatewayReconciler) nodeReaderClusterRole(
	owner *clawv1alpha1.OpenShellGateway,
	cfg openShellGatewayConfig,
) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRole"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   cfg.ClusterNodeReaderName,
			Labels: openShellGatewayLabels(owner.Name),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"authentication.k8s.io"},
				Resources: []string{"tokenreviews"},
				Verbs:     []string{"create"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"nodes"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
}

func (r *OpenShellGatewayReconciler) nodeReaderClusterRoleBinding(
	owner *clawv1alpha1.OpenShellGateway,
	cfg openShellGatewayConfig,
) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRoleBinding"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   cfg.ClusterNodeReaderName,
			Labels: openShellGatewayLabels(owner.Name),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     cfg.ClusterNodeReaderName,
		},
		Subjects: []rbacv1.Subject{
			{Kind: "ServiceAccount", Name: cfg.Name, Namespace: owner.Namespace},
		},
	}
}

func (r *OpenShellGatewayReconciler) service(
	owner *clawv1alpha1.OpenShellGateway,
	cfg openShellGatewayConfig,
) *corev1.Service {
	return &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: namespacedMeta(cfg.Name, owner.Namespace, owner.Name),
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{Name: "grpc", Port: cfg.ServicePort, TargetPort: intstr.FromString("grpc"), Protocol: corev1.ProtocolTCP, AppProtocol: ptrTo("grpc")},
			},
			Selector: openShellGatewaySelector(owner.Name),
		},
	}
}

func (r *OpenShellGatewayReconciler) deployment(
	owner *clawv1alpha1.OpenShellGateway,
	cfg openShellGatewayConfig,
) *appsv1.Deployment {
	replicas := int32(1)
	defaultMode := int32(0400)
	labels := openShellGatewayLabels(owner.Name)
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.Name,
			Namespace: owner.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: openShellGatewaySelector(owner.Name)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						"checksum/gateway-config": cfg.ConfigHash,
					},
				},
				Spec: corev1.PodSpec{
					EnableServiceLinks:           ptrTo(false),
					AutomountServiceAccountToken: ptrTo(true),
					ServiceAccountName:           cfg.Name,
					SecurityContext: &corev1.PodSecurityContext{
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{
						{
							Name:            "openshell-gateway",
							Image:           cfg.GatewayImage,
							ImagePullPolicy: cfg.GatewayImagePullPolicy,
							Args: []string{
								"--config", "/etc/openshell/gateway.toml",
								"--db-url", "sqlite:/var/openshell/openshell.db",
							},
							Ports: []corev1.ContainerPort{
								{Name: "grpc", ContainerPort: cfg.ServicePort, Protocol: corev1.ProtocolTCP},
								{Name: "health", ContainerPort: cfg.HealthPort, Protocol: corev1.ProtocolTCP},
							},
							StartupProbe:   openShellHTTPProbe("/healthz", "health", 30, 2),
							LivenessProbe:  openShellHTTPProbe("/healthz", "health", 3, 5),
							ReadinessProbe: openShellHTTPProbe("/readyz", "health", 3, 2),
							SecurityContext: &corev1.SecurityContext{
								RunAsNonRoot:             ptrTo(true),
								AllowPrivilegeEscalation: ptrTo(false),
								Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "openshell-data", MountPath: "/var/openshell"},
								{Name: "gateway-config", MountPath: "/etc/openshell", ReadOnly: true},
								{Name: "sandbox-jwt", MountPath: "/etc/openshell-jwt", ReadOnly: true},
							},
						},
					},
					Volumes: []corev1.Volume{
						{Name: "openshell-data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: "gateway-config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: openShellConfigMapName(owner.Name)},
						}}},
						{Name: "sandbox-jwt", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
							SecretName:  openShellJWTSecretName(owner.Name),
							DefaultMode: &defaultMode,
						}}},
					},
				},
			},
		},
	}
}

func openShellHTTPProbe(path, port string, failureThreshold int32, periodSeconds int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{Path: path, Port: intstr.FromString(port)},
		},
		FailureThreshold: failureThreshold,
		PeriodSeconds:    periodSeconds,
		TimeoutSeconds:   1,
	}
}

func renderOpenShellGatewayTOML(cfg openShellGatewayConfig) string {
	return fmt.Sprintf(`[openshell]
version = 1

[openshell.gateway]
bind_address = "0.0.0.0:%d"
health_bind_address = "0.0.0.0:%d"
log_level = %q
sandbox_namespace = %q
default_image = %q
supervisor_image = %q
disable_tls = true
enable_loopback_service_http = true

[openshell.gateway.auth]
allow_unauthenticated_users = true

[openshell.gateway.gateway_jwt]
signing_key_path = "/etc/openshell-jwt/signing.pem"
public_key_path = "/etc/openshell-jwt/public.pem"
kid_path = "/etc/openshell-jwt/kid"
gateway_id = %q
ttl_secs = 3600

[openshell.drivers.kubernetes]
grpc_endpoint = %q
service_account_name = %q
supervisor_sideload_method = "init-container"
sa_token_ttl_secs = 3600
image_pull_policy = %q
app_armor_profile = "Unconfined"
supervisor_image_pull_policy = %q
`,
		cfg.ServicePort,
		cfg.HealthPort,
		cfg.LogLevel,
		cfg.Namespace,
		cfg.SandboxImage,
		cfg.SupervisorImage,
		cfg.Name,
		cfg.Endpoint,
		openShellSandboxServiceAccountName(cfg.Name),
		string(cfg.SandboxImagePullPolicy),
		string(cfg.GatewayImagePullPolicy),
	)
}

func generateOpenShellJWTSecretData() (map[string][]byte, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	kid := make([]byte, 8)
	if _, err := rand.Read(kid); err != nil {
		return nil, err
	}
	return map[string][]byte{
		"signing.pem": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}),
		"public.pem":  pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}),
		"kid":         []byte(hex.EncodeToString(kid)),
	}, nil
}

func hasJWTSecretData(secret *corev1.Secret) bool {
	return len(secret.Data["signing.pem"]) > 0 &&
		len(secret.Data["public.pem"]) > 0 &&
		len(secret.Data["kid"]) > 0
}

func namespacedMeta(name, namespace, gatewayName string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		Labels:    openShellGatewayLabels(gatewayName),
	}
}

func openShellGatewayLabels(gatewayName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       openShellGatewayAppName,
		"app.kubernetes.io/managed-by": "claw-operator",
		"app.kubernetes.io/instance":   sanitizeLabelValue(gatewayName),
		InstanceLabelKey:               sanitizeLabelValue(gatewayName),
	}
}

func openShellGatewaySelector(gatewayName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     openShellGatewayAppName,
		"app.kubernetes.io/instance": sanitizeLabelValue(gatewayName),
	}
}

func openShellConfigMapName(name string) string {
	return name + "-config"
}

func openShellJWTSecretName(name string) string {
	return name + "-jwt-keys"
}

func openShellSandboxServiceAccountName(name string) string {
	return name + "-sandbox"
}

func openShellSandboxRoleName(name string) string {
	return name + "-sandbox"
}

func openShellPrivilegedSCCRoleBindingName(name string) string {
	return name + "-privileged-scc"
}

func valueOrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func ptrTo[T any](value T) *T {
	return &value
}
