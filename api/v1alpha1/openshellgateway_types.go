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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ConditionTypeOpenShellGatewayReady reports whether the OpenShell gateway deployment is ready.
	ConditionTypeOpenShellGatewayReady = "Ready"

	ConditionReasonOpenShellGatewayProvisioning = "Provisioning"
	ConditionReasonOpenShellGatewayReady        = "Ready"
)

// OpenShellGatewayOpenShiftSpec configures OpenShift-specific gateway setup.
type OpenShellGatewayOpenShiftSpec struct {
	// PrivilegedSandboxSCC grants the sandbox ServiceAccount access to the
	// OpenShift privileged SCC using the standard system ClusterRole.
	// +optional
	PrivilegedSandboxSCC bool `json:"privilegedSandboxSCC,omitempty"`
}

// OpenShellGatewaySpec defines the desired state of OpenShellGateway.
type OpenShellGatewaySpec struct {
	// GatewayImage is the OpenShell gateway image.
	// +kubebuilder:default="ghcr.io/nvidia/openshell/gateway:latest"
	// +optional
	GatewayImage string `json:"gatewayImage,omitempty"`

	// SupervisorImage is the OpenShell supervisor image injected into sandbox pods.
	// +kubebuilder:default="ghcr.io/nvidia/openshell/supervisor:latest"
	// +optional
	SupervisorImage string `json:"supervisorImage,omitempty"`

	// SandboxImage is the default image used for OpenShell-created sandbox pods.
	// +kubebuilder:default="quay.io/sallyom/openclaw-openshell-sandbox:latest"
	// +optional
	SandboxImage string `json:"sandboxImage,omitempty"`

	// GatewayImagePullPolicy controls gateway image pulls.
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +kubebuilder:default=IfNotPresent
	// +optional
	GatewayImagePullPolicy corev1.PullPolicy `json:"gatewayImagePullPolicy,omitempty"`

	// SandboxImagePullPolicy is passed to the OpenShell Kubernetes driver for sandbox pods.
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +kubebuilder:default=IfNotPresent
	// +optional
	SandboxImagePullPolicy corev1.PullPolicy `json:"sandboxImagePullPolicy,omitempty"`

	// ServicePort is the gateway gRPC/HTTP service port.
	// +kubebuilder:default=8080
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	ServicePort int32 `json:"servicePort,omitempty"`

	// HealthPort is the gateway health probe port.
	// +kubebuilder:default=8081
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	HealthPort int32 `json:"healthPort,omitempty"`

	// LogLevel is passed to the OpenShell gateway config.
	// +kubebuilder:default=info
	// +optional
	LogLevel string `json:"logLevel,omitempty"`

	// OpenShift configures OpenShift-specific resources.
	// +optional
	OpenShift *OpenShellGatewayOpenShiftSpec `json:"openShift,omitempty"`
}

// OpenShellGatewayStatus defines the observed state of OpenShellGateway.
type OpenShellGatewayStatus struct {
	// Endpoint is the in-cluster URL Claw instances can use to reach this gateway.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// ServiceName is the managed Service name.
	// +optional
	ServiceName string `json:"serviceName,omitempty"`

	// DeploymentName is the managed Deployment name.
	// +optional
	DeploymentName string `json:"deploymentName,omitempty"`

	// ReadyReplicas reports ready gateway deployment replicas.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// ObservedGeneration is the last generation reconciled into status.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the gateway state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=openshellgateways,scope=Namespaced
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status"
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".status.endpoint"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// OpenShellGateway is the Schema for operator-managed OpenShell gateway installs.
type OpenShellGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OpenShellGatewaySpec   `json:"spec,omitempty"`
	Status OpenShellGatewayStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OpenShellGatewayList contains a list of OpenShellGateway.
type OpenShellGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OpenShellGateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OpenShellGateway{}, &OpenShellGatewayList{})
}
