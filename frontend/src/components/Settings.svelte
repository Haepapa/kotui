<script lang="ts">
  import { onMount } from 'svelte';
  import { getConfig, saveConfig, listOllamaModels, pullOllamaModel, deleteOllamaModel } from '../lib/warroom';
  import { saveAccentColor, currentAccentColor, DEFAULT_ACCENT } from '../lib/theme';
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

  // ── Appearance ────────────────────────────────────────────────────────────
  let accentColor = $state(currentAccentColor());

  const ACCENT_PRESETS = [
    { label: 'Gold',    hex: '#d4a017' },
    { label: 'Blue',    hex: '#4f7cf7' },
    { label: 'Purple',  hex: '#8b5cf6' },
    { label: 'Green',   hex: '#22c55e' },
    { label: 'Red',     hex: '#ef4444' },
    { label: 'Teal',    hex: '#14b8a6' },
  ];

  function handleAccentChange(hex: string) {
    accentColor = hex;
    saveAccentColor(hex);
  }

  // Per-endpoint model state
  type OllamaState = {
    models: string[];
    loading: boolean;
    opError: string;       // errors from pull/delete operations only
    connected: boolean | null; // null = unchecked / not configured
    configured: boolean;   // false = no endpoint entered yet
    pullName: string;
    pullStatus: 'idle' | 'pulling' | 'done' | 'error';
    pullError: string;
    deleting: string;
  };

  function freshState(): OllamaState {
    return { models: [], loading: false, opError: '', connected: null, configured: true,
             pullName: '', pullStatus: 'idle', pullError: '', deleting: '' };
  }

  let local = $state<OllamaState>(freshState());
  let remote = $state<OllamaState>(freshState());

  onMount(async () => {
    try {
      const loaded = await getConfig();
      if (loaded) Object.assign(cfg, loaded);
    } catch (e) {
      console.error('getConfig:', e);
    }
    // Local always checks (falls back to localhost:11434 if blank).
    // Remote only checks if an endpoint has been configured.
    await Promise.all([
      refreshModels(local, cfg.ollama_endpoint || ''),
      cfg.senior_endpoint ? refreshModels(remote, cfg.senior_endpoint) : markUnconfigured(remote),
    ]);
  });

  function markUnconfigured(state: OllamaState) {
    state.configured = false;
    state.connected = null;
    state.models = [];
  }

  async function refreshModels(state: OllamaState, endpoint: string) {
    // If no endpoint is given for remote, mark as not configured rather than
    // silently falling back to local — that would give a false positive.
    if (!endpoint && state === remote) {
      markUnconfigured(state);
      return;
    }
    state.loading = true;
    state.configured = true;
    state.opError = '';
    try {
      state.models = (await listOllamaModels(endpoint)) ?? [];
      state.connected = true;
    } catch (_e) {
      // Don't surface the raw error here; the dot indicator already communicates
      // connection failure. Raw errors are reserved for pull/delete operations.
      state.models = [];
      state.connected = false;
    } finally {
      state.loading = false;
    }
  }

  async function handlePull(state: OllamaState, endpoint: string) {
    if (!state.pullName.trim() || !state.connected) return;
    state.pullStatus = 'pulling';
    state.pullError = '';
    try {
      await pullOllamaModel(endpoint, state.pullName.trim());
      state.pullStatus = 'done';
      state.pullName = '';
      await refreshModels(state, endpoint);
      setTimeout(() => { state.pullStatus = 'idle'; }, 3000);
    } catch (e) {
      state.pullStatus = 'error';
      state.pullError = e instanceof Error ? e.message : String(e);
    }
  }

  async function handleDelete(state: OllamaState, endpoint: string, name: string) {
    if (!confirm(`Delete model "${name}"? This cannot be undone.`)) return;
    state.deleting = name;
    state.opError = '';
    try {
      await deleteOllamaModel(endpoint, name);
      await refreshModels(state, endpoint);
    } catch (e) {
      state.opError = e instanceof Error ? e.message : String(e);
    } finally {
      state.deleting = '';
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

    <!-- ── Appearance ────────────────────────────────────────────── -->
    <section class="settings-section">
      <div class="section-heading-row">
        <h3>Appearance</h3>
      </div>
      <div class="field-group">
        <label class="field-label">Accent colour</label>
        <div class="accent-row">
          {#each ACCENT_PRESETS as p}
            <button
              class="swatch"
              class:swatch-active={accentColor.toLowerCase() === p.hex.toLowerCase()}
              style="background:{p.hex}"
              title={p.label}
              onclick={() => handleAccentChange(p.hex)}
            ></button>
          {/each}
          <input
            type="color"
            class="colour-picker"
            value={accentColor}
            title="Custom colour"
            oninput={(e) => handleAccentChange((e.target as HTMLInputElement).value)}
          />
        </div>
      </div>
    </section>

    <!-- ── Local Ollama ───────────────────────────────────────── -->
    <section class="settings-section">
      <div class="section-heading-row">
        <h3>Ollama — Local</h3>
        <span
          class="conn-dot"
          class:conn-ok={local.connected === true}
          class:conn-err={local.connected === false}
          class:conn-checking={local.connected === null}
          title={local.connected === true ? 'Connected' : local.connected === false ? 'Unreachable' : 'Checking…'}
        ></span>
        <span class="conn-label">
          {local.connected === true ? 'Connected' : local.connected === false ? 'Unreachable' : ''}
        </span>
      </div>

      <label>
        <span>Endpoint</span>
        <div class="input-row">
          <input bind:value={cfg.ollama_endpoint} placeholder="http://localhost:11434" />
          <button class="icon-btn" onclick={() => refreshModels(local, cfg.ollama_endpoint)} title="Refresh" disabled={local.loading}>
            {local.loading ? '…' : '↻'}
          </button>
        </div>
      </label>

      <label>
        <span>Lead Model</span>
        <select bind:value={cfg.lead_model} class="model-select" disabled={local.connected !== true}>
          {#if cfg.lead_model && !local.models.includes(cfg.lead_model)}
            <option value={cfg.lead_model}>{cfg.lead_model}</option>
          {/if}
          {#each local.models as m}
            <option value={m}>{m}</option>
          {/each}
          {#if local.models.length === 0}<option value="" disabled>No models</option>{/if}
        </select>
      </label>

      <label>
        <span>Worker Model</span>
        <select bind:value={cfg.worker_model} class="model-select" disabled={local.connected !== true}>
          {#if cfg.worker_model && !local.models.includes(cfg.worker_model)}
            <option value={cfg.worker_model}>{cfg.worker_model}</option>
          {/if}
          {#each local.models as m}
            <option value={m}>{m}</option>
          {/each}
          {#if local.models.length === 0}<option value="" disabled>No models</option>{/if}
        </select>
      </label>

      <label>
        <span>Embedder Model</span>
        <select bind:value={cfg.embedder_model} class="model-select" disabled={local.connected !== true}>
          {#if cfg.embedder_model && !local.models.includes(cfg.embedder_model)}
            <option value={cfg.embedder_model}>{cfg.embedder_model}</option>
          {/if}
          {#each local.models as m}
            <option value={m}>{m}</option>
          {/each}
          {#if local.models.length === 0}<option value="" disabled>No models</option>{/if}
        </select>
      </label>

      {#if local.connected === false}
        <p class="status-note service-down">Ollama service is unreachable. Start Ollama and click ↻ to reconnect.</p>
      {:else}
        <div class="subsection-label">Pull model</div>
        <label>
          <span>Model name</span>
          <div class="input-row">
            <input
              bind:value={local.pullName}
              placeholder="e.g. llama3.2:3b"
              onkeydown={(e) => { if (e.key === 'Enter') handlePull(local, cfg.ollama_endpoint); }}
              disabled={local.pullStatus === 'pulling'}
            />
            <button class="icon-btn accent" onclick={() => handlePull(local, cfg.ollama_endpoint)}
              disabled={local.pullStatus === 'pulling' || !local.pullName.trim()} title="Pull model">
              {local.pullStatus === 'pulling' ? '…' : '↓'}
            </button>
          </div>
        </label>
        {#if local.pullStatus === 'pulling'}
          <p class="status-note">Pulling — this may take several minutes…</p>
        {:else if local.pullStatus === 'done'}
          <p class="status-note success">Pulled successfully.</p>
        {:else if local.pullStatus === 'error'}
          <p class="status-note danger">{local.pullError}</p>
        {/if}

        <div class="subsection-label">
          Installed models
          {#if local.opError}<span class="inline-error">— {local.opError}</span>{/if}
        </div>
        {#if local.models.length === 0 && !local.loading}
          <p class="status-note">No models installed.</p>
        {:else}
          <div class="model-list">
            {#each local.models as m}
              <div class="model-row">
                <span class="model-name">{m}</span>
                <button class="delete-btn" onclick={() => handleDelete(local, cfg.ollama_endpoint, m)}
                  disabled={local.deleting === m} title="Delete model">
                  {local.deleting === m ? '…' : '✕'}
                </button>
              </div>
            {/each}
          </div>
        {/if}
      {/if}
    </section>

    <!-- ── Remote Ollama ─────────────────────────────────────── -->
    <section class="settings-section">
      <div class="section-heading-row">
        <h3>Ollama — Remote</h3>
        <span
          class="conn-dot"
          class:conn-ok={remote.connected === true}
          class:conn-err={remote.connected === false}
          class:conn-checking={remote.connected === null}
          title={remote.connected === true ? 'Connected' : remote.connected === false ? 'Unreachable' : 'Not configured'}
        ></span>
        <span class="conn-label">
          {remote.connected === true ? 'Connected' : remote.connected === false ? 'Unreachable' : ''}
        </span>
      </div>
      <p class="section-note">Optional. Used by the Senior Consultant agent; overrides local settings for that agent.</p>

      <label>
        <span>Endpoint</span>
        <div class="input-row">
          <input bind:value={cfg.senior_endpoint} placeholder="http://remote-host:11434" />
          <button class="icon-btn" onclick={() => refreshModels(remote, cfg.senior_endpoint)} title="Refresh" disabled={remote.loading}>
            {remote.loading ? '…' : '↻'}
          </button>
        </div>
      </label>

      <label>
        <span>Lead Model</span>
        <select bind:value={cfg.senior_model} class="model-select" disabled={remote.connected !== true}>
          {#if cfg.senior_model && !remote.models.includes(cfg.senior_model)}
            <option value={cfg.senior_model}>{cfg.senior_model}</option>
          {/if}
          {#each remote.models as m}
            <option value={m}>{m}</option>
          {/each}
          {#if remote.models.length === 0}<option value="" disabled>No models</option>{/if}
        </select>
      </label>

      <label>
        <span>SSH Host</span>
        <input bind:value={cfg.senior_ssh_host} placeholder="my-gpu-box" />
      </label>
      <label>
        <span>SSH Start Command</span>
        <input bind:value={cfg.senior_ssh_cmd} placeholder="ollama serve" />
      </label>

      {#if !remote.configured}
        <p class="status-note">Enter an endpoint above to manage remote models.</p>
      {:else if remote.connected === false}
        <p class="status-note service-down">Remote Ollama service is unreachable. Check the endpoint and click ↻.</p>
      {:else if remote.connected === true}
        <div class="subsection-label">Pull model</div>
        <label>
          <span>Model name</span>
          <div class="input-row">
            <input
              bind:value={remote.pullName}
              placeholder="e.g. qwen2.5-coder:32b"
              onkeydown={(e) => { if (e.key === 'Enter') handlePull(remote, cfg.senior_endpoint); }}
              disabled={remote.pullStatus === 'pulling'}
            />
            <button class="icon-btn accent" onclick={() => handlePull(remote, cfg.senior_endpoint)}
              disabled={remote.pullStatus === 'pulling' || !remote.pullName.trim()} title="Pull model">
              {remote.pullStatus === 'pulling' ? '…' : '↓'}
            </button>
          </div>
        </label>
        {#if remote.pullStatus === 'pulling'}
          <p class="status-note">Pulling — this may take several minutes…</p>
        {:else if remote.pullStatus === 'done'}
          <p class="status-note success">Pulled successfully.</p>
        {:else if remote.pullStatus === 'error'}
          <p class="status-note danger">{remote.pullError}</p>
        {/if}

        <div class="subsection-label">
          Installed models
          {#if remote.opError}<span class="inline-error">— {remote.opError}</span>{/if}
        </div>
        {#if remote.models.length === 0 && !remote.loading}
          <p class="status-note">No models installed on remote.</p>
        {:else}
          <div class="model-list">
            {#each remote.models as m}
              <div class="model-row">
                <span class="model-name">{m}</span>
                <button class="delete-btn" onclick={() => handleDelete(remote, cfg.senior_endpoint, m)}
                  disabled={remote.deleting === m} title="Delete model">
                  {remote.deleting === m ? '…' : '✕'}
                </button>
              </div>
            {/each}
          </div>
        {/if}
      {/if}
    </section>

    <!-- ── General ────────────────────────────────────────────── -->
    <section class="settings-section">
      <h3>General</h3>
      <label>
        <span>Timezone</span>
        <input bind:value={cfg.timezone} placeholder="Pacific/Auckland" />
      </label>
    </section>

    <!-- ── Remote Messaging ──────────────────────────────────── -->
    <section class="settings-section">
      <h3>Remote Messaging</h3>
      <p class="section-note">Changes apply on next restart. Tokens are stored in config.toml — keep this file secure.</p>

      <h4>Telegram</h4>
      <label><span>Bot Token</span><input type="password" bind:value={cfg.telegram_bot_token} placeholder="1234567890:ABC..." /></label>
      <label><span>Chat ID</span><input bind:value={cfg.telegram_chat_id} placeholder="Your Telegram chat_id" /></label>

      <h4>Slack</h4>
      <label><span>Bot Token</span><input type="password" bind:value={cfg.slack_bot_token} placeholder="xoxb-..." /></label>
      <label><span>Channel ID</span><input bind:value={cfg.slack_channel_id} placeholder="C0123456789" /></label>
      <label><span>Signing Secret</span><input type="password" bind:value={cfg.slack_signing_secret} placeholder="Slack App signing secret" /></label>

      <h4>WhatsApp</h4>
      <label><span>Access Token</span><input type="password" bind:value={cfg.whatsapp_token} placeholder="WhatsApp Cloud API access token" /></label>
      <label><span>Phone Number ID</span><input bind:value={cfg.whatsapp_phone_number_id} placeholder="Meta phone number ID" /></label>
      <label><span>Verify Token</span><input bind:value={cfg.whatsapp_verify_token} placeholder="Webhook verify token" /></label>

      <h4>Webhook Server</h4>
      <label><span>Port</span><input type="number" bind:value={cfg.webhook_port} placeholder="8080" min="1" max="65535" /></label>
      <label><span>Shared Secret</span><input type="password" bind:value={cfg.webhook_secret} placeholder="Optional HMAC secret" /></label>
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

  /* Section heading with connection indicator */
  .section-heading-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-bottom: 0.25rem;
  }
  h3 {
    font-size: 0.6875rem;
    font-weight: 700;
    letter-spacing: 0.09em;
    text-transform: uppercase;
    color: var(--nav-label-color);
    margin: 0;
  }
  .conn-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex-shrink: 0;
    background: var(--border-subtle);
    transition: background 0.3s;
  }
  .conn-dot.conn-ok    { background: #4ade80; box-shadow: 0 0 4px #4ade8066; }
  .conn-dot.conn-err   { background: #f87171; }
  .conn-dot.conn-checking { background: var(--text-muted); opacity: 0.5; }
  .conn-label {
    font-size: 0.7rem;
    color: var(--text-muted);
    letter-spacing: 0.03em;
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
    margin-top: 0.375rem;
  }
  .inline-error { color: #f87171; font-weight: 400; }
  .section-note {
    font-size: 0.8125rem;
    color: var(--text-muted);
    margin: 0 0 0.25rem;
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
  select { cursor: pointer; }

  .input-row {
    flex: 1;
    display: flex;
    gap: 0.375rem;
    min-width: 0;
  }
  .input-row input { flex: 1; }
  .model-select { width: 100%; flex: 1; }

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

  /* ── Appearance ───────────────────────────────────────────── */
  .field-group { display: flex; flex-direction: column; gap: 0.375rem; }
  .field-label { font-size: 0.8125rem; color: var(--text-secondary); font-weight: 500; }
  .accent-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .swatch {
    width: 24px;
    height: 24px;
    border-radius: 50%;
    border: 2px solid transparent;
    cursor: pointer;
    transition: transform 0.12s, border-color 0.12s;
    flex-shrink: 0;
  }
  .swatch:hover { transform: scale(1.18); }
  .swatch-active { border-color: var(--text-heading) !important; transform: scale(1.15); }
  .colour-picker {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    border: 2px solid var(--border-input);
    padding: 0;
    cursor: pointer;
    background: none;
    overflow: hidden;
  }
  .colour-picker::-webkit-color-swatch-wrapper { padding: 0; }
  .colour-picker::-webkit-color-swatch { border: none; border-radius: 50%; }

  .model-list {
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
  .status-note.danger  { color: #f87171; }
  .status-note.service-down { color: var(--text-muted); padding-left: 0; font-style: italic; }

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
