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

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, meResponse{
		User:             user,
		DefaultNamespace: allowedNamespaceForUser(user, s.namespaceSuffix),
		Providers:        []string{"openrouter", "openai", "google", "google-vertex", "anthropic", "anthropic-vertex", "xai"},
	})
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFileFS(w, r, staticFiles, "static/index.html")
}

func (s *server) handleState(w http.ResponseWriter, r *http.Request) {
	identity, err := currentIdentity(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	namespace := r.URL.Query().Get("namespace")
	if err := validateNamespace(namespace); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.authorizeNamespace(identity.Name, namespace); err != nil {
		writeError(w, statusCodeFor(err), err.Error())
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		name = "instance"
	}
	if err := validateResourceName(name, "Claw name"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	state, err := s.getState(r.Context(), identity, namespace, name)
	if err != nil {
		var apiErr apiError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			writeJSON(w, http.StatusOK, stateResponse{Name: name, Exists: false})
			return
		}
		writeError(w, statusCodeFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *server) handleClaws(w http.ResponseWriter, r *http.Request) {
	identity, err := currentIdentity(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	namespace := r.URL.Query().Get("namespace")
	if err := validateNamespace(namespace); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.authorizeNamespace(identity.Name, namespace); err != nil {
		writeError(w, statusCodeFor(err), err.Error())
		return
	}

	claws, err := s.listClaws(r.Context(), identity, namespace)
	if err != nil {
		var apiErr apiError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusForbidden) {
			writeJSON(w, http.StatusOK, listResponse{Claws: []stateResponse{}})
			return
		}
		writeError(w, statusCodeFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Claws: claws})
}

func (s *server) handleProvision(w http.ResponseWriter, r *http.Request) {
	identity, err := currentIdentity(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	var req provisionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Provider = strings.ToLower(strings.TrimSpace(req.Provider))
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "instance"
	}
	req.Model = normalizeModelRef(req.Provider, req.Model)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.GCPProject = strings.TrimSpace(req.GCPProject)
	req.GCPLocation = strings.TrimSpace(req.GCPLocation)
	if err := validateNamespace(req.Namespace); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateResourceName(req.Name, "Claw name"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.AgentName = agentNameFromClawName(req.Name)
	if err := s.authorizeNamespace(identity.Name, req.Namespace); err != nil {
		writeError(w, statusCodeFor(err), err.Error())
		return
	}
	if _, ok := providers[req.Provider]; !ok {
		writeError(w, http.StatusBadRequest, "unsupported provider")
		return
	}
	provider := providers[req.Provider]
	if strings.TrimSpace(req.APIKey) == "" {
		if provider.RequiresGCP {
			writeError(w, http.StatusBadRequest, "GCP service account JSON is required")
		} else {
			writeError(w, http.StatusBadRequest, "API key is required")
		}
		return
	}
	if provider.RequiresGCP && (req.GCPProject == "" || req.GCPLocation == "") {
		writeError(w, http.StatusBadRequest, "GCP project and location are required")
		return
	}
	if provider.RequiresGCP {
		if err := validateGCPServiceAccountJSON(req.APIKey); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "Model is required")
		return
	}

	if err := s.ensureProject(r.Context(), identity, req.Namespace); err != nil {
		writeError(w, statusCodeFor(err), "failed to create project: "+err.Error())
		return
	}
	if err := s.applySecret(r.Context(), identity, req); err != nil {
		writeError(w, statusCodeFor(err), "failed to create provider secret: "+err.Error())
		return
	}
	if err := s.applyClaw(r.Context(), identity, req); err != nil {
		writeError(w, statusCodeFor(err), "failed to create Claw: "+err.Error())
		return
	}

	state, err := s.getState(r.Context(), identity, req.Namespace, req.Name)
	if err != nil {
		writeError(w, statusCodeFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *server) handleRestart(w http.ResponseWriter, r *http.Request) {
	identity, err := currentIdentity(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	namespace := r.URL.Query().Get("namespace")
	if err := validateNamespace(namespace); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.authorizeNamespace(identity.Name, namespace); err != nil {
		writeError(w, statusCodeFor(err), err.Error())
		return
	}
	name := r.URL.Query().Get("name")
	if err := validateResourceName(name, "Claw name"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.restartDeployments(r.Context(), identity, namespace, name); err != nil {
		writeError(w, statusCodeFor(err), "failed to restart OpenClaw: "+err.Error())
		return
	}
	state, err := s.getState(r.Context(), identity, namespace, name)
	if err != nil {
		writeError(w, statusCodeFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request) {
	identity, err := currentIdentity(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	namespace := r.URL.Query().Get("namespace")
	if err := validateNamespace(namespace); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.authorizeNamespace(identity.Name, namespace); err != nil {
		writeError(w, statusCodeFor(err), err.Error())
		return
	}
	name := r.URL.Query().Get("name")
	if err := validateResourceName(name, "Claw name"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	state, _ := s.getState(r.Context(), identity, namespace, name)
	if err := s.delete(r.Context(), identity, apiPath("apis/claw.sandbox.redhat.com/v1alpha1/namespaces", namespace, "claws", name)); err != nil {
		writeError(w, statusCodeFor(err), "failed to delete Claw: "+err.Error())
		return
	}
	secretNames := state.SecretNames
	if len(secretNames) == 0 {
		providers := state.Providers
		if len(providers) == 0 && state.Provider != "" {
			providers = []string{state.Provider}
		}
		for _, provider := range providers {
			secretNames = appendUnique(secretNames, secretName(name, provider))
		}
	}
	for _, secretName := range secretNames {
		_ = s.deleteManagedSecret(r.Context(), identity, namespace, name, secretName)
	}
	writeJSON(w, http.StatusOK, stateResponse{Exists: false})
}
