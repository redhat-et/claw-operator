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

package main

import "net/http"

const (
	apiKeySecretKey    = "api-key"
	gcpSecretKey       = "sa-key.json"
	fieldManager       = "openclaw-deployer"
	managedByLabel     = "app.kubernetes.io/managed-by"
	managedByValue     = "openclaw-deployer"
	instanceLabel      = "openclaw-deployer.redhat.com/instance"
	providerLabel      = "openclaw-deployer.redhat.com/provider"
	inClusterCAPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	inClusterTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultListenAddr  = ":8080"
	defaultNSSuffix    = "-claw"
)

type server struct {
	apiServer       string
	bearerToken     string
	impersonate     bool
	namespaceSuffix string
	client          *http.Client
	static          http.Handler
}

type userIdentity struct {
	Name   string
	Groups []string
}

type provisionRequest struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	AgentName   string `json:"agentName"`
	Model       string `json:"model"`
	Provider    string `json:"provider"`
	APIKey      string `json:"apiKey"`
	GCPProject  string `json:"gcpProject"`
	GCPLocation string `json:"gcpLocation"`
}

type meResponse struct {
	User             string   `json:"user,omitempty"`
	DefaultNamespace string   `json:"defaultNamespace,omitempty"`
	Providers        []string `json:"providers"`
}

type stateResponse struct {
	Name        string   `json:"name,omitempty"`
	Exists      bool     `json:"exists"`
	Ready       bool     `json:"ready"`
	Reason      string   `json:"reason,omitempty"`
	Message     string   `json:"message,omitempty"`
	GatewayURL  string   `json:"gatewayURL,omitempty"`
	Provider    string   `json:"provider,omitempty"`
	Providers   []string `json:"providers,omitempty"`
	Model       string   `json:"model,omitempty"`
	AgentName   string   `json:"agentName,omitempty"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	SecretNames []string `json:"-"`
}

type listResponse struct {
	Claws []stateResponse `json:"claws"`
}
