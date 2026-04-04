<script lang="ts">
  import { onMount } from 'svelte';
  import { getCompanyIdentity, saveCompanyIdentity } from '../lib/warroom';
  import { switchToChat } from '../stores/warroom.svelte';

  let content = $state('');
  let saveStatus = $state<'idle' | 'confirming' | 'saving' | 'saved' | 'error'>('idle');
  let errorMsg = $state('');

  onMount(async () => {
    try {
      content = (await getCompanyIdentity()) ?? '';
    } catch (e) {
      console.error('getCompanyIdentity:', e);
    }
  });

  async function handleSave() {
    if (saveStatus !== 'confirming') {
      saveStatus = 'confirming';
      return;
    }
    saveStatus = 'saving';
    try {
      await saveCompanyIdentity(content);
      saveStatus = 'saved';
      setTimeout(() => saveStatus = 'idle', 3000);
    } catch (e: unknown) {
      saveStatus = 'error';
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  function cancelConfirm() {
    saveStatus = 'idle';
  }
</script>

<div class="editor">
  <div class="editor-header">
    <button class="back-btn" onclick={switchToChat}>← Back</button>
    <h2>Company Identity</h2>
  </div>
  <div class="editor-body">
    <p class="editor-hint">Edit the company identity document. Saving will reset all agent context (Culture Broadcast).</p>
    <textarea
      class="identity-textarea"
      bind:value={content}
      placeholder="# Company Identity&#10;&#10;Write your company identity here in Markdown..."
      spellcheck={false}
    ></textarea>
    <div class="editor-footer">
      {#if saveStatus === 'error'}
        <span class="status-error">{errorMsg}</span>
      {:else if saveStatus === 'saved'}
        <span class="status-saved">✓ Saved and context reset</span>
      {:else if saveStatus === 'confirming'}
        <span class="status-warn">⚠ This will reset all agent context. Click Save again to confirm.</span>
      {/if}
      <div class="footer-actions">
        {#if saveStatus === 'confirming'}
          <button class="cancel-btn" onclick={cancelConfirm}>Cancel</button>
        {/if}
        <button class="save-btn" onclick={handleSave} disabled={saveStatus === 'saving'}>
          {saveStatus === 'saving' ? 'Saving…' : saveStatus === 'confirming' ? 'Confirm Save' : 'Save Identity'}
        </button>
      </div>
    </div>
  </div>
</div>

<style>
  .editor {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    height: 100%;
  }
  .editor-header {
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
  .editor-body {
    flex: 1;
    display: flex;
    flex-direction: column;
    padding: 1.25rem;
    gap: 0.75rem;
    overflow: hidden;
    min-height: 0;
  }
  .editor-hint {
    font-size: 0.875rem;
    color: var(--text-muted);
    margin: 0;
  }
  .identity-textarea {
    flex: 1;
    background: var(--bg-surface);
    border: 1px solid var(--border-input);
    border-radius: 9px;
    color: var(--text-heading);
    font-size: 0.9375rem;
    font-family: 'Menlo', 'Monaco', 'Courier New', monospace;
    line-height: 1.6;
    padding: 0.875rem 1rem;
    outline: none;
    resize: none;
    transition: border-color 0.15s;
    min-height: 0;
  }
  .identity-textarea:focus { border-color: var(--accent); }
  .editor-footer {
    display: flex;
    align-items: center;
    gap: 1rem;
    flex-shrink: 0;
  }
  .footer-actions {
    display: flex;
    gap: 0.5rem;
    margin-left: auto;
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
  .cancel-btn {
    padding: 0.5rem 1rem;
    background: none;
    border: 1px solid var(--border-input);
    border-radius: 8px;
    color: var(--text-secondary);
    font-size: 0.9375rem;
    cursor: pointer;
    transition: background 0.12s;
  }
  .cancel-btn:hover { background: var(--bg-hover); }
  .status-saved { font-size: 0.875rem; color: #4ade80; }
  .status-error { font-size: 0.875rem; color: #f87171; }
  .status-warn { font-size: 0.875rem; color: #facc15; }
</style>
