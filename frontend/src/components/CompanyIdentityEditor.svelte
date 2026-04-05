<script lang="ts">
  import { onMount } from 'svelte';
  import { getCompanyIdentity, saveCompanyIdentity, getHandbook, saveHandbook } from '../lib/warroom';
  import { switchToChat } from '../stores/warroom.svelte';

  type Tab = 'company' | 'handbook';
  let activeTab = $state<Tab>('company');

  // Company Identity state
  let companyContent = $state('');
  let companySaveStatus = $state<'idle' | 'confirming' | 'saving' | 'saved' | 'error'>('idle');
  let companyErrorMsg = $state('');

  // Handbook state
  let handbookContent = $state('');
  let handbookSaveStatus = $state<'idle' | 'saving' | 'saved' | 'error'>('idle');
  let handbookErrorMsg = $state('');

  onMount(async () => {
    try {
      companyContent = (await getCompanyIdentity()) ?? '';
    } catch (e) {
      console.error('getCompanyIdentity:', e);
    }
    try {
      handbookContent = (await getHandbook()) ?? '';
    } catch (e) {
      console.error('getHandbook:', e);
    }
  });

  async function handleCompanySave() {
    if (companySaveStatus !== 'confirming') {
      companySaveStatus = 'confirming';
      return;
    }
    companySaveStatus = 'saving';
    try {
      await saveCompanyIdentity(companyContent);
      companySaveStatus = 'saved';
      setTimeout(() => (companySaveStatus = 'idle'), 3000);
    } catch (e: unknown) {
      companySaveStatus = 'error';
      companyErrorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  function cancelCompanyConfirm() {
    companySaveStatus = 'idle';
  }

  async function handleHandbookSave() {
    handbookSaveStatus = 'saving';
    try {
      await saveHandbook(handbookContent);
      handbookSaveStatus = 'saved';
      setTimeout(() => (handbookSaveStatus = 'idle'), 3000);
    } catch (e: unknown) {
      handbookSaveStatus = 'error';
      handbookErrorMsg = e instanceof Error ? e.message : String(e);
    }
  }
</script>

<div class="company-editor">
  <div class="editor-header">
    <button class="back-btn" onclick={switchToChat}>← Back</button>
    <h2>Company Identity</h2>
  </div>

  <div class="tabs">
    <button class="tab-btn" class:active={activeTab === 'company'} onclick={() => (activeTab = 'company')}>
      🏢 Company Identity
      {#if companySaveStatus === 'saved'}<span class="tab-saved">✓</span>{/if}
    </button>
    <button class="tab-btn" class:active={activeTab === 'handbook'} onclick={() => (activeTab = 'handbook')}>
      📋 Handbook
      {#if handbookSaveStatus === 'saved'}<span class="tab-saved">✓</span>{/if}
    </button>
  </div>

  {#if activeTab === 'company'}
    <div class="tab-body">
      <p class="tab-hint">Edit the company identity document. Saving will reset all agent context (Culture Broadcast).</p>
      <textarea
        class="editor-textarea"
        bind:value={companyContent}
        placeholder="# Company Identity&#10;&#10;Write your company identity here in Markdown..."
        spellcheck={false}
      ></textarea>
      <div class="tab-footer">
        {#if companySaveStatus === 'error'}
          <span class="status-error">{companyErrorMsg}</span>
        {:else if companySaveStatus === 'saved'}
          <span class="status-saved">✓ Saved and context reset</span>
        {:else if companySaveStatus === 'confirming'}
          <span class="status-warn">⚠ This will reset all agent context. Click Save again to confirm.</span>
        {/if}
        <div class="footer-actions">
          {#if companySaveStatus === 'confirming'}
            <button class="cancel-btn" onclick={cancelCompanyConfirm}>Cancel</button>
          {/if}
          <button class="save-btn" onclick={handleCompanySave} disabled={companySaveStatus === 'saving'}>
            {companySaveStatus === 'saving' ? 'Saving…' : companySaveStatus === 'confirming' ? 'Confirm Save' : 'Save Identity'}
          </button>
        </div>
      </div>
    </div>
  {:else}
    <div class="tab-body">
      <p class="tab-hint">The agent handbook defines operating rules, escalation protocols, and hard constraints. All agents receive this as part of their system prompt.</p>
      <textarea
        class="editor-textarea"
        bind:value={handbookContent}
        placeholder="# Handbook&#10;&#10;Write your handbook here in Markdown..."
        spellcheck={false}
      ></textarea>
      <div class="tab-footer">
        {#if handbookSaveStatus === 'error'}
          <span class="status-error">{handbookErrorMsg}</span>
        {:else if handbookSaveStatus === 'saved'}
          <span class="status-saved">✓ Handbook saved</span>
        {/if}
        <div class="footer-actions">
          <button class="save-btn" onclick={handleHandbookSave} disabled={handbookSaveStatus === 'saving'}>
            {handbookSaveStatus === 'saving' ? 'Saving…' : 'Save Handbook'}
          </button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .company-editor { flex: 1; display: flex; flex-direction: column; overflow: hidden; height: 100%; }
  .editor-header { display: flex; align-items: center; gap: 1rem; padding: 0 1.25rem; height: 48px; border-bottom: 1px solid var(--border-subtle); flex-shrink: 0; }
  .back-btn { background: none; border: none; color: var(--text-muted); cursor: pointer; font-size: 0.875rem; padding: 0.25rem 0.5rem; border-radius: 6px; line-height: 1; transition: background 0.12s, color 0.12s; }
  .back-btn:hover { background: var(--bg-hover); color: var(--text-heading); }
  h2 { font-size: 1rem; font-weight: 600; color: var(--text-heading); margin: 0; }
  .tabs { display: flex; gap: 0.25rem; padding: 0.75rem 1.25rem 0; border-bottom: 1px solid var(--border-subtle); flex-shrink: 0; }
  .tab-btn { background: none; border: none; border-bottom: 2px solid transparent; color: var(--text-muted); cursor: pointer; font-size: 0.875rem; padding: 0.375rem 0.875rem 0.5rem; margin-bottom: -1px; transition: color 0.12s, border-color 0.12s; display: flex; align-items: center; gap: 0.35rem; }
  .tab-btn:hover { color: var(--text-secondary); }
  .tab-btn.active { color: var(--text-heading); border-bottom-color: var(--accent); }
  .tab-saved { font-size: 0.7rem; color: #4ade80; }
  .tab-body { flex: 1; display: flex; flex-direction: column; padding: 1rem 1.25rem 1.25rem; gap: 0.75rem; overflow: hidden; min-height: 0; }
  .tab-hint { font-size: 0.8125rem; color: var(--text-muted); margin: 0; flex-shrink: 0; }
  .editor-textarea { flex: 1; background: var(--bg-surface); border: 1px solid var(--border-input); border-radius: 9px; color: var(--text-heading); font-size: 0.9rem; font-family: 'Menlo', 'Monaco', 'Courier New', monospace; line-height: 1.6; padding: 0.875rem 1rem; outline: none; resize: none; transition: border-color 0.15s; min-height: 0; user-select: text; }
  .editor-textarea:focus { border-color: var(--accent); }
  .tab-footer { display: flex; align-items: center; gap: 1rem; flex-shrink: 0; }
  .footer-actions { display: flex; gap: 0.5rem; margin-left: auto; }
  .save-btn { padding: 0.5rem 1.25rem; background: var(--accent-btn); color: #e0e9ff; border: none; border-radius: 8px; font-size: 0.9375rem; cursor: pointer; transition: background 0.15s; }
  .save-btn:hover:not(:disabled) { background: var(--accent-btn-hover); }
  .save-btn:disabled { opacity: 0.5; cursor: default; }
  .cancel-btn { padding: 0.5rem 1rem; background: none; border: 1px solid var(--border-input); border-radius: 8px; color: var(--text-secondary); font-size: 0.9375rem; cursor: pointer; transition: background 0.12s; }
  .cancel-btn:hover { background: var(--bg-hover); }
  .status-saved { font-size: 0.875rem; color: #4ade80; }
  .status-error { font-size: 0.875rem; color: #f87171; }
  .status-warn { font-size: 0.875rem; color: #facc15; }
</style>
