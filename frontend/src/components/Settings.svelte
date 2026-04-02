<script lang="ts">
  import { onMount } from 'svelte';
  import { getConfig, saveConfig } from '../lib/warroom';
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

  onMount(async () => {
    try {
      const loaded = await getConfig();
      if (loaded) Object.assign(cfg, loaded);
    } catch (e) {
      console.error('getConfig:', e);
    }
  });

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
    <h2>Infrastructure Office</h2>
  </div>
  <div class="settings-body">

    <section class="settings-section">
      <h3>Ollama</h3>
      <label>
        <span>Endpoint</span>
        <input bind:value={cfg.ollama_endpoint} placeholder="http://localhost:11434" />
      </label>
      <label>
        <span>Lead Model</span>
        <input bind:value={cfg.lead_model} placeholder="qwen2.5-coder:32b" />
      </label>
      <label>
        <span>Worker Model</span>
        <input bind:value={cfg.worker_model} placeholder="llama3.1:8b" />
      </label>
      <label>
        <span>Embedder Model</span>
        <input bind:value={cfg.embedder_model} placeholder="nomic-embed-text" />
      </label>
    </section>

    <section class="settings-section">
      <h3>Senior Consultant</h3>
      <label>
        <span>Model</span>
        <input bind:value={cfg.senior_model} placeholder="qwen2.5-coder:32b" />
      </label>
      <label>
        <span>Endpoint</span>
        <input bind:value={cfg.senior_endpoint} placeholder="http://remote:11434" />
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

    <section class="settings-section">
      <h3>General</h3>
      <label>
        <span>Timezone</span>
        <input bind:value={cfg.timezone} placeholder="Pacific/Auckland" />
      </label>
    </section>

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
        <span class="status-saved">✓ Saved — changes apply on next restart</span>
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
    padding: 0 1.25rem 0.75rem;
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
  input {
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
  }
  input:focus { border-color: var(--accent); }
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
