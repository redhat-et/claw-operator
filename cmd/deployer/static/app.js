const state = {
  namespace: localStorage.getItem("openclaw-deployer.namespace") || "",
  provider: localStorage.getItem("openclaw-deployer.provider") || "openrouter",
  selectedName: localStorage.getItem("openclaw-deployer.name") || "instance",
  model: localStorage.getItem("openclaw-deployer.model") || "",
  gcpProject: localStorage.getItem("openclaw-deployer.gcpProject") || "",
  gcpLocation: localStorage.getItem("openclaw-deployer.gcpLocation") || "",
  claws: [],
  exists: false,
  ready: false,
};

const modelDefaults = {
  anthropic: "anthropic/claude-sonnet-4-6",
  "anthropic-vertex": "anthropic-vertex/claude-sonnet-4-6",
  google: "google/gemini-3.1-pro-preview",
  "google-vertex": "google/gemini-3.1-pro-preview",
  openai: "openai/gpt-5.5",
  openrouter: "openrouter/anthropic/claude-sonnet-4-6",
  xai: "xai/grok-4.3",
};

const modelOptions = {
  anthropic: ["anthropic/claude-sonnet-4-6", "anthropic/claude-haiku-4-5"],
  "anthropic-vertex": ["anthropic-vertex/claude-sonnet-4-6", "anthropic-vertex/claude-opus-4-8", "anthropic-vertex/claude-opus-4-7"],
  google: ["google/gemini-3.1-pro-preview", "google/gemini-3.5-flash", "google/gemini-3.1-flash-lite"],
  "google-vertex": ["google/gemini-3.1-pro-preview", "google/gemini-3.5-flash", "google/gemini-3.1-flash-lite"],
  openai: ["openai/gpt-5.5", "openai/gpt-5.4", "openai/gpt-5.4-mini"],
  openrouter: ["openrouter/anthropic/claude-sonnet-4-6", "openrouter/openai/gpt-5.5", "openrouter/google/gemini-3.5-flash", "openrouter/auto"],
  xai: ["xai/grok-4.3", "xai/grok-4.20"],
};

const googleVertexProviders = new Set(["anthropic-vertex", "google-vertex"]);

const defaultGCPLocations = {
  "anthropic-vertex": "us-east5",
  "google-vertex": "us-central1",
};

const els = {
  card: document.getElementById("card"),
  user: document.getElementById("user"),
  namespace: document.getElementById("namespace"),
  clawName: document.getElementById("clawName"),
  provider: document.getElementById("provider"),
  model: document.getElementById("model"),
  modelOptions: document.getElementById("model-options"),
  gcpProjectRow: document.getElementById("gcp-project-row"),
  gcpLocationRow: document.getElementById("gcp-location-row"),
  gcpProject: document.getElementById("gcpProject"),
  gcpLocation: document.getElementById("gcpLocation"),
  credentialLabel: document.getElementById("credential-label"),
  apiKey: document.getElementById("apiKey"),
  gcpCredentials: document.getElementById("gcpCredentials"),
  status: document.getElementById("status"),
  running: document.getElementById("running"),
  clawList: document.getElementById("claw-list"),
  provision: document.getElementById("provision"),
  restart: document.getElementById("restart"),
  delete: document.getElementById("delete"),
};

els.namespace.value = state.namespace;
els.clawName.value = state.selectedName;
els.provider.value = state.provider;
els.model.value = state.model || modelDefaults[state.provider] || "";
els.gcpProject.value = state.gcpProject;
els.gcpLocation.value = state.gcpLocation || defaultGCPLocations[state.provider] || "";
renderModelOptions();
renderCredentialFields();

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(payload.error || `Request failed: ${response.status}`);
  }
  return payload;
}

async function init() {
  try {
    const me = await api("/api/me");
    if (!state.namespace && me.defaultNamespace) {
      state.namespace = me.defaultNamespace;
      els.namespace.value = state.namespace;
    }
    if (me.user) {
      els.user.textContent = me.user;
    }
    els.namespace.readOnly = true;
  } catch (error) {
    setStatus(error.message, true);
    setBusy(false);
    return;
  }
  await refresh();
}

async function refresh() {
  state.namespace = els.namespace.value.trim();
  state.selectedName = els.clawName.value.trim() || "instance";
  state.provider = els.provider.value;
  state.model = els.model.value.trim() || modelDefaults[state.provider] || "";
  state.gcpProject = els.gcpProject.value.trim();
  state.gcpLocation = els.gcpLocation.value.trim();
  localStorage.setItem("openclaw-deployer.namespace", state.namespace);
  localStorage.setItem("openclaw-deployer.name", state.selectedName);
  localStorage.setItem("openclaw-deployer.provider", state.provider);
  localStorage.setItem("openclaw-deployer.model", state.model);
  localStorage.setItem("openclaw-deployer.gcpProject", state.gcpProject);
  localStorage.setItem("openclaw-deployer.gcpLocation", state.gcpLocation);

  if (!state.namespace) {
    setStatus("Choose the namespace where your OpenClaw should run.");
    renderList([]);
    return;
  }

  setStatus("Checking status...");
  try {
    const current = await api(`/api/claws?namespace=${encodeURIComponent(state.namespace)}`);
    renderList(current.claws || []);
  } catch (error) {
    renderList([]);
    setStatus(error.message, true);
  }
}

function renderList(claws) {
  state.claws = claws;
  const selected = claws.find((claw) => claw.name === state.selectedName) || null;
  state.exists = Boolean(selected);
  state.ready = Boolean(selected && selected.ready);
  if (selected) {
    if (selected.model) {
      els.model.value = selected.model;
      state.model = selected.model;
      localStorage.setItem("openclaw-deployer.model", selected.model);
    }
  }

  els.card.classList.toggle("ready", state.exists && state.ready);
  els.restart.disabled = !state.exists;
  els.delete.disabled = !state.exists;
  els.provision.textContent = state.exists ? "Add/update provider" : "Create";
  renderClaws(claws);

  if (!state.exists) {
    setStatus("No OpenClaw is running in your namespace.");
    return;
  }
  if (selected.ready) {
    setStatus(
      `Your OpenClaw ${selected.name} is now running in project ${state.namespace}. Further customizations can be made from the OpenClaw Control UI or the Claw CR.`,
    );
    return;
  }
  setStatus(selected.message || selected.reason || `${selected.name} is provisioning.`);
}

function renderClaws(claws) {
  els.running.hidden = claws.length === 0;
  els.clawList.innerHTML = "";
  for (const claw of claws) {
    const row = document.createElement("div");
    row.className = `claw-row${claw.name === state.selectedName ? " selected" : ""}`;
    const details = document.createElement("button");
    details.type = "button";
    details.className = "claw-pick";
    details.textContent = `${claw.name} · ${claw.ready ? "Ready" : claw.reason || "Provisioning"}`;
    details.addEventListener("click", () => {
      state.selectedName = claw.name;
      els.clawName.value = claw.name;
      localStorage.setItem("openclaw-deployer.name", claw.name);
      renderList(state.claws);
    });
    row.appendChild(details);
    if (claw.gatewayURL) {
      const link = document.createElement("a");
      link.href = claw.gatewayURL;
      link.target = "_blank";
      link.rel = "noopener noreferrer";
      link.textContent = "Open Control UI";
      row.appendChild(link);
    }
    els.clawList.appendChild(row);
  }
}

function renderModelOptions() {
  els.modelOptions.innerHTML = "";
  for (const model of modelOptions[els.provider.value] || []) {
    const option = document.createElement("option");
    option.value = model;
    els.modelOptions.appendChild(option);
  }
}

function renderCredentialFields() {
  const isGoogleVertex = googleVertexProviders.has(els.provider.value);
  els.gcpProjectRow.hidden = !isGoogleVertex;
  els.gcpLocationRow.hidden = !isGoogleVertex;
  els.credentialLabel.textContent = isGoogleVertex ? "Service Account Key" : "API key";
  els.apiKey.hidden = isGoogleVertex;
  els.gcpCredentials.hidden = !isGoogleVertex;
  if (isGoogleVertex && !els.gcpLocation.value.trim()) {
    els.gcpLocation.value = defaultGCPLocations[els.provider.value] || "";
  }
}

function setStatus(message, isError = false) {
  els.status.textContent = message;
  els.status.style.color = isError ? "#b42318" : "";
}

function setBusy(busy) {
  for (const button of [els.provision, els.restart, els.delete]) {
    button.disabled = busy || (button === els.restart && !state.exists) || (button === els.delete && !state.exists);
  }
}

els.provision.addEventListener("click", async () => {
  const namespace = els.namespace.value.trim();
  const name = els.clawName.value.trim();
  const provider = els.provider.value;
  const model = els.model.value.trim();
  const isGoogleVertex = googleVertexProviders.has(provider);
  const apiKey = (isGoogleVertex ? els.gcpCredentials.value : els.apiKey.value).trim();
  const gcpProject = els.gcpProject.value.trim();
  const gcpLocation = els.gcpLocation.value.trim();

  if (!namespace || !name || !model || !apiKey) {
    setStatus(`Namespace, OpenClaw name, model, and ${isGoogleVertex ? "service account JSON" : "API key"} are required.`, true);
    return;
  }
  if (isGoogleVertex && (!gcpProject || !gcpLocation)) {
    setStatus("GCP project and region are required.", true);
    return;
  }
  if (isGoogleVertex && !isSupportedGCPKey(apiKey)) {
    setStatus('Valid JSON with type "service_account" or "authorized_user" is required.', true);
    return;
  }

  setBusy(true);
  setStatus(state.exists ? "Adding or updating provider..." : "Creating OpenClaw...");
  try {
    const current = await api("/api/provision", {
      method: "POST",
      body: JSON.stringify({ namespace, name, provider, model, apiKey, gcpProject, gcpLocation }),
    });
    els.apiKey.value = "";
    els.gcpCredentials.value = "";
    state.selectedName = current.name || name;
    els.clawName.value = state.selectedName;
    await refresh();
  } catch (error) {
    setStatus(error.message, true);
  } finally {
    setBusy(false);
  }
});

els.restart.addEventListener("click", async () => {
  if (!state.exists || !confirm("Restart this OpenClaw instance?")) {
    return;
  }
  setBusy(true);
  setStatus("Restarting OpenClaw...");
  try {
    await api(`/api/restart?namespace=${encodeURIComponent(els.namespace.value.trim())}&name=${encodeURIComponent(els.clawName.value.trim())}`, { method: "POST" });
    await refresh();
  } catch (error) {
    setStatus(error.message, true);
  } finally {
    setBusy(false);
  }
});

els.delete.addEventListener("click", async () => {
  if (!state.exists || !confirm("Delete this OpenClaw instance?")) {
    return;
  }
  setBusy(true);
  setStatus("Deleting OpenClaw...");
  try {
    await api(`/api/claw?namespace=${encodeURIComponent(els.namespace.value.trim())}&name=${encodeURIComponent(els.clawName.value.trim())}`, { method: "DELETE" });
    await refresh();
  } catch (error) {
    setStatus(error.message, true);
  } finally {
    setBusy(false);
  }
});

els.namespace.addEventListener("change", refresh);
els.clawName.addEventListener("change", refresh);
els.provider.addEventListener("change", () => {
  const previousDefault = modelDefaults[state.provider] || "";
  state.provider = els.provider.value;
  renderModelOptions();
  renderCredentialFields();
  if (!els.model.value.trim() || els.model.value.trim() === previousDefault) {
    els.model.value = modelDefaults[state.provider] || "";
  }
  localStorage.setItem("openclaw-deployer.provider", state.provider);
  localStorage.setItem("openclaw-deployer.model", els.model.value.trim());
});
els.gcpProject.addEventListener("change", () => {
  state.gcpProject = els.gcpProject.value.trim();
  localStorage.setItem("openclaw-deployer.gcpProject", state.gcpProject);
});
els.gcpLocation.addEventListener("change", () => {
  state.gcpLocation = els.gcpLocation.value.trim();
  localStorage.setItem("openclaw-deployer.gcpLocation", state.gcpLocation);
});
els.model.addEventListener("change", () => {
  state.model = els.model.value.trim();
  localStorage.setItem("openclaw-deployer.model", state.model);
});

function isSupportedGCPKey(value) {
  try {
    const parsed = JSON.parse(value);
    return parsed.type === "service_account" || parsed.type === "authorized_user";
  } catch {
    return false;
  }
}

init();
setInterval(() => {
  if (state.namespace && state.claws.some((claw) => !claw.ready)) {
    refresh();
  }
}, 10000);
