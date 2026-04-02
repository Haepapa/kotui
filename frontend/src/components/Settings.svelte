<script lang="ts">
  import { onMount } from 'svelte';
  import { getConfig, saveConfig, listOllamaModels, pullOllamaModel, deleteOllamaModel } from '../lib/warroom';
  import { switchToChat } from '../stores/warroom.svelte';
  import type { UIConfig } from '../lib/types';

  let cfg = $state<UIConfig>({
    ollama_endpoint: '',
    lead_model: '',
    worker_model: '',
    embedder_model: '',
    senior_model: '',
    senior_endpoint: '',
    senior_ssh_host: '',
    senior_ssh_cmd: '',
    timezone: '',
    telegram_bot_token: '',
    telegram_chat_id: '',
    slack_bot_token: '',
    slack_channel_id: '',
    slack_signing_secret: '',
    whatsapp_token: '',
    whatsapp_phone_number_id: '',
    whatsapp_verify_token: '',
    webhook_secret: '',
    webhook_port: 8080,
  });

  let saveStatus = $state<'idle' | 'saving' | 'saved' | 'error'>('idle');
  let errorMsg = $state('');

  // Ollama model management state
  let localModels = $state<string[]>([]);
  let modelsLoading = $state(false);
  let modelsError = $state('');
  let pullName = $state('');
  let pullStatus = $state<'idle' | 'pulling' | 'done' | 'error'>('idle');
  let pullError = $state('');
  let deletingModel = $state('');

  onMount(async () => {
    try {
      const loaded = await getConfig();
      if (loaded) Object.assign(cfg, loaded);
    } catch (e) {
      console.error('getConfig:', e);
    }
    refreshModels();
  });

  async function refreshModels() {
    modelsLoading = true;
    modelsError = '';
    try {
      localModels = (await listOllamaModels(cfg.ollama_endpoint)) ?? [];
    } catch (e) {
      modelsError = e instanceof Error ? e.message : String(e);
      localModels = [];
    } finally {
      modelsLoading = false;
    }
  }

  async function handlePull() {
    if (!pullName.trim()) return;
    pullStatus = 'pulling';
    pullError = '';
    try {
      await pullOllamaModel(pullName.trim());
      pullStatus = 'done';
      pullName = '';
      await refreshModels();
      setTimeout(() => pullStatus = 'idle', 3000);
    } catch (e) {
      pullStatus = 'error';
      pullError = e instanceof Error ? e.message : String(e);
    }
  }

  async function handleDelete(name: string) {
    if (!confirm(`Delete model "${name}"? This cannot be undone.`)) return;
    deletingModel = name;
    try {
      await deleteOllamaModel(name);
      await refreshModels();
    } catch (e) {
      modelsError = e instanceof Error ? e.message : String(e);
    } finally {
      deletingModel = '';
    }
  }

  async function handleSave() {
    saveStatus = 'saving';
    try {
      await saveConfig(cfg);
      saveStatus = 'saved';
      setTimeout(() => saveStatus = 'idle', 3000);
    } catch (e: unknown) {
      saveStatus = 'error';
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }
</script>

<div class="settings">
  <div class="settings-header">
    <button class="back-btn" onclick={switchToChat}>← Back</button>
    <h2>Settings</h2>
  </div>
  <div class="settings-body">

    <!-- ── Local Ollama ─────────────────────────────────────── -->
    <section class="settings-section">
      <h3>Ollama — Local</h3>
      <label>
        <span>Endpoint</span>
        <div class="input-row">
          <input bind:value={cfg.ollama_endpoint} placeholder="http://localhost:11434" />
          <button class="icon-btn" onclick={refreshModels} title="Refresh model list" disabled={modelsLoading}>
            {modelsLoading ? '…' : '↻'}
          </button>
        </div>
      </label>

      <label>
        <span>Lead Model</span>
        <div class="model-select-wrap">
          <select bind:value={cfg.lead_model} class="model-select">
            {#if cfg.lead_model && !localModels.includes(cfg.lead_model)}
              <option value={cfg.lead_model}>{cfg.lead_model}</option>
            {/if}
            {#each localModels as m}
              <option value={m}>{m}</option>
            {/each}
            {#if localModels.length === 0}
              <option value="" disabled>No models found</option>
            {/if}
          </select>
        </div>
      </label>

      <label>
        <span>Worker Model</span>
        <div class="model-select-wrap">
          <select bind:value={cfg.worker_model} class="model-select">
            {#if cfg.worker_model && !localModels.includes(cfg.worker_model)}
              <option value={cfg.worker_model}>{cfg.worker_model}</option>
            {/if}
            {#each localModels as m}
              <option value={m}>{m}</option>
            {/each}
            {#if localModels.length === 0}
              <option value="" disabled>No models found</option>
            {/if}
          </select>
        </div>
      </label>

      <label>
        <span>Embedder Model</span>
        <div class="model-select-wrap">
          <select bind:value={cfg.embedder_model} class="model-select">
            {#if cfg.embedder_model && !localModels.includes(cfg.embedder_model)}
              <option value={cfg.embedder_model}>{cfg.embedder_model}</option>
            {/if}
            {#each localModels as m}
              <option value={m}>{m}</option>
            {/each}
            {#if localModels.length === 0}
              <option value="" disabled>No models found</option>
            {/if}
          </select>
        </div>
      </label>

      <!-- Pull a new model -->
      <div class="subsection-label">Pull model</div>
      <label>
        <span>Model name</span>
        <div class="input-row">
          <input
            bind:value={pullName}
            placeholder="e.g. llama3.2:3b"
            onkeydown={(e) => { if (e.key === 'Enter') handlePull(); }}
            disabled={pullStatus === 'pulling'}
          />
          <button
            class="icon-btn accent"
            onclick={handlePull}
            disabled={pullStatus === 'pulling' || !pullName.trim()}
            title="Pull this model from Ollama registry"
          >
            {pullStatus === 'pulling' ? '…' : '↓'}
          </button>
        </div>
      </label>
      {#if pullStatus === 'pulling'}
        <p class="status-note">Pulling model — this may take several minutes…</p>
      {:else if pullStatus === 'done'}
        <p class="status-note success">Model pulled successfully.</p>
      {:else if pullStatus === 'error'}
        <p class="status-note danger">{pullError}</p>
      {/if}

      <!-- Installed models list -->
      <div class="subsection-label">
        Installed models
        {#if modelsError}
          <span class="inline-error">— {modelsError}</span>
        {/if}
      </div>
      {#if localModels.length === 0 && !modelsLoading}
        <p class="status-note">No models found. Is Ollama running at the configured endpoint?</p>
      {:else}
        <div class="model-list">
          {#each localModels as m}
            <div class="model-row">
              <span class="model-name">{m}</span>
              <button
                class="delete-btn"
                onclick={() => handleDelete(m)}
                disabled={deletingModel === m}
                title="Delete this model"
              >
                {deletingModel === m ? '…' : '✕'}
              </button>
            </div>
          {/each}
        </div>
      {/if}
    </section>

    <!-- ── Remote Ollama (optional) ─────────────────────────── -->
    <section class="settings-section">
      <h3>Ollama — Remote (optional)</h3>
      <p class="section-note">Override the local endpoint for the Senior Consultant agent. Leave blank to use local.</p>
      <label>
        <span>Endpoint</span>
        <input bind:value={cfg.senior_endpoint} placeholder="http://remote-host:11434" />
      </label>
      <label>
        <span>Model</span>
        <input bind:value={cfg.senior_model} placeholder="qwen2.5-coder:32b" />
      </label>
      <label>
        <span>SSH Host</span>
        <input bind:value={cfg.senior_ssh_host} placeholder="my-gpu-box" />
      </label>
      <label>
        <span>SSH Start Command</span>
        <input bind:value={cfg.senior_ssh_cmd} placeholder="ollama serve" />
      </label>
    </section>

    <!-- ── General ──────────────────────────────────────────── -->
    <section class="settings-section">
      <h3>General</h3>
      <label>
        <span>Timezone</span>
        <input bind:value={cfg.timezone} placeholder="Pacific/Auckland" />
      </label>
    </section>

    <!-- ── Remote Messaging ─────────────────────────────────── -->
    <section class="settings-section">
      <h3>Remote Messaging</h3>
      <p class="section-note">Changes apply on next restart. Tokens are stored in config.toml — keep this file secure.</p>

      <h4>Telegram</h4>
      <label>
        <span>Bot Token</span>
        <input type="password" bind:value={cfg.telegram_bot_token} placeholder="1234567890:ABC..." />
      </label>
      <label>
        <span>Chat ID</span>
        <input bind:value={cfg.telegram_chat_id} placeholder="Your Telegram chat_id (user or group)" />
      </label>

      <h4>Slack</h4>
      <label>
        <span>Bot Token</span>
        <input type="password" bind:value={cfg.slack_bot_token} placeholder="xoxb-..." />
      </label>
      <label>
        <span>Channel ID</span>
        <input bind:value={cfg.slack_channel_id} placeholder="C0123456789" />
      </label>
      <label>
        <span>Signing Secret</span>
        <input type="password" bind:value={cfg.slack_signing_secret} placeholder="Slack App signing secret" />
      </label>

      <h4>WhatsApp</h4>
      <label>
        <span>Access Token</span>
        <input type="password" bind:value={cfg.whatsapp_token} placeholder="WhatsApp Cloud API access token" />
      </label>
      <label>
        <span>Phone Number ID</span>
        <input bind:value={cfg.whatsapp_phone_number_id} placeholder="Meta phone number ID" />
      </label>
      <label>
        <span>Verify Token</span>
        <input bind:value={cfg.whatsapp_verify_token} placeholder="Webhook verify token" />
      </label>

      <h4>Webhook Server</h4>
      <label>
        <span>Port</span>
        <input type="number" bind:value={cfg.webhook_port} placeholder="8080" min="1" max="65535" />
      </label>
      <label>
        <span>Shared Secret</span>
        <input type="password" bind:value={cfg.webhook_secret} placeholder="Optional HMAC secret" />
      </label>
    </section>

    <div class="settings-footer">
      {#if saveStatus === 'error'}
        <span class="status-error">{errorMsg}</span>
      {:else if saveStatus === 'saved'}
        <span class="status-saved">✓ Saved — some changes apply on next restart</span>
      {/if}
      <button class="save-btn" onclick={handleSave} disabled={saveStatus === 'saving'}>
        {saveStatus === 'saving' ? 'Saving…' : 'Save Settings'}
      </button>
    </div>
  </div>
</div>

<style>
  .settings {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    height: 100%;
  }
  .settings-header {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 0 1.25rem;
    height: 48px;
    border-bottom: 1px solid var(--border-subtle);
    flex-shrink: 0;
  }
  .back-btn {
    background: none;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 0.875rem;
    padding: 0.25rem 0.5rem;
    border-radius: 6px;
    line-height: 1;
    transition: background 0.12s, color 0.12s;
  }
  .back-btn:hover { background: var(--bg-hover); color: var(--text-heading); }
  h2 {
    font-size: 1rem;
    font-weight: 600;
    color: var(--text-heading);
    margin: 0;
  }
  .settings-body {
    flex: 1;
    overflow-y: auto;
    padding: 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
  }
  .settings-body::-webkit-scrollbar { width: 4px; }
  .settings-body::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 4px; }
  .settings-section {
    display: flex;
    flex-direction: column;
    gap: 0.625rem;
  }
  h3 {
    font-size: 0.6875rem;
    font-weight: 700;
    letter-spacing: 0.09em;
    text-transform: uppercase;
    color: var(--nav-label-color);
    margin: 0 0 0.25rem;
  }
  h4 {
    font-size: 0.8125rem;
    font-weight: 600;
    color: var(--text-secondary);
    margin: 0.75rem 0 0.25rem;
  }
  .subsection-label {
    font-size: 0.8125rem;
    font-weight: 600;
    color: var(--text-secondary);
    margin-top: 0.625rem;
  }
  .inline-error { color: #f87171; font-weight: 400; }
  .section-note {
    font-size: 0.8125rem;
    color: var(--text-muted);
    margin: 0 0 0.5rem;
  }
  label {
    display: flex;
    align-items: center;
    gap: 1rem;
  }
  label span {
    font-size: 0.875rem;
    color: var(--text-secondary);
    width: 180px;
    flex-shrink: 0;
  }
  input, select {
    flex: 1;
    background: var(--bg-surface);
    border: 1px solid var(--border-input);
    border-radius: 7px;
    color: var(--text-heading);
    font-size: 0.875rem;
    padding: 0.4rem 0.75rem;
    outline: none;
    transition: border-color 0.15s;
    font-family: inherit;
    min-width: 0;
  }
  input:focus, select:focus { border-color: var(--accent); }
  select { cursor: pointer; appearance: auto; }

  /* Input + icon button side by side */
  .input-row {
    flex: 1;
    display: flex;
    gap: 0.375rem;
    min-width: 0;
  }
  .input-row input { flex: 1; }
  .model-select-wrap {
    flex: 1;
    min-width: 0;
  }
  .model-select { width: 100%; }

  .icon-btn {
    flex-shrink: 0;
    background: var(--bg-surface);
    border: 1px solid var(--border-input);
    border-radius: 7px;
    color: var(--text-secondary);
    cursor: pointer;
    font-size: 1rem;
    width: 34px;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background 0.12s, color 0.12s;
  }
  .icon-btn:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-heading); }
  .icon-btn:disabled { opacity: 0.5; cursor: default; }
  .icon-btn.accent { background: var(--accent-btn); color: #e0e9ff; border-color: var(--accent-btn-hover); }
  .icon-btn.accent:hover:not(:disabled) { background: var(--accent-btn-hover); }

  /* Installed model list */
  .model-list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    overflow: hidden;
  }
  .model-row {
    display: flex;
    align-items: center;
    padding: 0.375rem 0.75rem;
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border-subtle);
  }
  .model-row:last-child { border-bottom: none; }
  .model-name {
    flex: 1;
    font-size: 0.875rem;
    color: var(--text-primary);
    font-family: monospace;
  }
  .delete-btn {
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-muted);
    font-size: 0.75rem;
    padding: 2px 6px;
    border-radius: 4px;
    transition: background 0.1s, color 0.1s;
    flex-shrink: 0;
  }
  .delete-btn:hover:not(:disabled) { background: rgba(248,113,113,0.12); color: #f87171; }
  .delete-btn:disabled { opacity: 0.4; cursor: default; }

  .status-note {
    font-size: 0.8125rem;
    color: var(--text-muted);
    margin: 0;
    padding-left: 196px;
  }
  .status-note.success { color: #4ade80; }
  .status-note.danger { color: #f87171; }

  .settings-footer {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding-top: 0.5rem;
  }
  .save-btn {
    padding: 0.5rem 1.25rem;
    background: var(--accent-btn);
    color: #e0e9ff;
    border: none;
    border-radius: 8px;
    font-size: 0.9375rem;
    cursor: pointer;
    transition: background 0.15s;
  }
  .save-btn:hover:not(:disabled) { background: var(--accent-btn-hover); }
  .save-btn:disabled { opacity: 0.5; cursor: default; }
  .status-saved { font-size: 0.875rem; color: #4ade80; }
  .status-error { font-size: 0.875rem; color: #f87171; }
</style>
