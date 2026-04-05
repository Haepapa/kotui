<script lang="ts">
  import { onMount } from 'svelte';
  import { getAgentBrainFiles, saveAgentBrainFile } from '../lib/warroom';
  import { wr, switchFromBrain } from '../stores/warroom.svelte';

  let { agentID }: { agentID: string } = $props();

  type TabKey = 'soul' | 'persona' | 'skills';
  type SaveStatus = 'idle' | 'saving' | 'saved' | 'error';

  let activeTab = $state<TabKey>('soul');
  let soul    = $state('');
  let persona = $state('');
  let skills  = $state('');
  let loadError = $state('');
  let loadErrorCopied = $state(false);
  let saveStatus = $state<Record<TabKey, SaveStatus>>({ soul: 'idle', persona: 'idle', skills: 'idle' });
  let saveError  = $state<Record<TabKey, string>>({ soul: '', persona: '', skills: '' });
  let saveErrorCopied = $state<Record<TabKey, boolean>>({ soul: false, persona: false, skills: false });

  const tabLabels: Record<TabKey, string> = {
    soul:    '✦ Soul',
    persona: '◎ Persona',
    skills:  '⚡ Skills',
  };

  const tabHints: Record<TabKey, string> = {
    soul:    'Core values and purpose. These are the agent\'s deepest commitments.',
    persona: 'Communication style, personality, and name. How the agent expresses itself.',
    skills:  'Known capabilities, limitations, and capability ceiling. What the agent can and cannot do.',
  };

  function currentContent() {
    if (activeTab === 'soul')    return soul;
    if (activeTab === 'persona') return persona;
    return skills;
  }

  function setContent(val: string) {
    if (activeTab === 'soul')         soul    = val;
    else if (activeTab === 'persona') persona = val;
    else                              skills  = val;
  }

  function copyError(text: string, which: 'load' | TabKey) {
    navigator.clipboard.writeText(text).then(() => {
      if (which === 'load') {
        loadErrorCopied = true;
        setTimeout(() => (loadErrorCopied = false), 2000);
      } else {
        saveErrorCopied[which as TabKey] = true;
        setTimeout(() => (saveErrorCopied[which as TabKey] = false), 2000);
      }
    });
  }

  onMount(async () => {
    try {
      const files = await getAgentBrainFiles(agentID);
      soul    = files.soul    ?? '';
      persona = files.persona ?? '';
      skills  = files.skills  ?? '';
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
    }
  });

  async function handleSave() {
    const tab = activeTab;
    const content = currentContent();
    const summary = `Manually edited via Brain Panel`;
    saveStatus[tab] = 'saving';
    saveError[tab]  = '';
    try {
      await saveAgentBrainFile(agentID, tab, content, summary);
      saveStatus[tab] = 'saved';
      setTimeout(() => { saveStatus[tab] = 'idle'; }, 2500);
    } catch (e: unknown) {
      saveStatus[tab] = 'error';
      saveError[tab]  = e instanceof Error ? e.message : String(e);
    }
  }
</script>

<div class="brain-panel">
  <div class="brain-header">
    <button class="back-btn" onclick={switchFromBrain}>← Back</button>
    <h2>Brain Files</h2>
  </div>

  {#if loadError}
    <div class="load-error">
      <span>⚠ Could not load brain files: {loadError}</span>
      <button class="err-copy-btn" onclick={() => copyError(loadError, 'load')} title="Copy error">
        {#if loadErrorCopied}
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>
        {:else}
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
        {/if}
      </button>
    </div>
  {:else}
    <div class="tabs">
      {#each (Object.keys(tabLabels) as TabKey[]) as tab}
        <button
          class="tab-btn"
          class:active={activeTab === tab}
          onclick={() => (activeTab = tab)}
        >
          {tabLabels[tab]}
          {#if saveStatus[tab] === 'saved'}
            <span class="tab-saved">✓</span>
          {/if}
        </button>
      {/each}
    </div>

    <div class="tab-body">
      <p class="tab-hint">{tabHints[activeTab]}</p>
      <textarea
        class="brain-textarea"
        value={currentContent()}
        oninput={(e) => setContent((e.target as HTMLTextAreaElement).value)}
        placeholder="# {activeTab}.md&#10;&#10;Write in Markdown..."
        spellcheck={false}
      ></textarea>
      <div class="tab-footer">
        {#if saveStatus[activeTab] === 'error'}
          <span class="status-error">
            {saveError[activeTab]}
            <button class="err-copy-btn" onclick={() => copyError(saveError[activeTab], activeTab)} title="Copy error">
              {#if saveErrorCopied[activeTab]}
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>
              {:else}
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
              {/if}
            </button>
          </span>
        {:else if saveStatus[activeTab] === 'saved'}
          <span class="status-saved">✓ Saved — agent context refreshed</span>
        {/if}
        <div class="footer-actions">
          <button
            class="save-btn"
            onclick={handleSave}
            disabled={saveStatus[activeTab] === 'saving'}
          >
            {saveStatus[activeTab] === 'saving' ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .brain-panel {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    height: 100%;
  }

  .brain-header {
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

  .load-error {
    display: flex;
    align-items: flex-start;
    gap: 0.5rem;
    padding: 1.25rem;
    color: #f87171;
    font-size: 0.9rem;
  }
  .load-error span { flex: 1; user-select: text; -webkit-user-select: text; cursor: text; }

  .tabs {
    display: flex;
    gap: 0.25rem;
    padding: 0.75rem 1.25rem 0;
    border-bottom: 1px solid var(--border-subtle);
    flex-shrink: 0;
  }

  .tab-btn {
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 0.875rem;
    padding: 0.375rem 0.875rem 0.5rem;
    margin-bottom: -1px;
    transition: color 0.12s, border-color 0.12s;
    display: flex;
    align-items: center;
    gap: 0.35rem;
  }
  .tab-btn:hover { color: var(--text-secondary); }
  .tab-btn.active {
    color: var(--text-heading);
    border-bottom-color: var(--accent);
  }

  .tab-saved {
    font-size: 0.7rem;
    color: #4ade80;
  }

  .tab-body {
    flex: 1;
    display: flex;
    flex-direction: column;
    padding: 1rem 1.25rem 1.25rem;
    gap: 0.75rem;
    overflow: hidden;
    min-height: 0;
  }

  .tab-hint {
    font-size: 0.8125rem;
    color: var(--text-muted);
    margin: 0;
    flex-shrink: 0;
  }

  .brain-textarea {
    flex: 1;
    background: var(--bg-surface);
    border: 1px solid var(--border-input);
    border-radius: 9px;
    color: var(--text-heading);
    font-size: 0.9rem;
    font-family: 'Menlo', 'Monaco', 'Courier New', monospace;
    line-height: 1.6;
    padding: 0.875rem 1rem;
    outline: none;
    resize: none;
    transition: border-color 0.15s;
    min-height: 0;
    user-select: text;
  }
  .brain-textarea:focus { border-color: var(--accent); }

  .tab-footer {
    display: flex;
    align-items: center;
    gap: 1rem;
    flex-shrink: 0;
  }

  .footer-actions {
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

  .status-saved { font-size: 0.875rem; color: #4ade80; }
  .status-error {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    font-size: 0.875rem;
    color: #f87171;
    user-select: text;
    -webkit-user-select: text;
  }

  .err-copy-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    color: #f87171;
    cursor: pointer;
    padding: 2px 4px;
    border-radius: 4px;
    flex-shrink: 0;
    transition: color 0.12s, background 0.12s;
  }
  .err-copy-btn:hover { color: #fca5a5; background: rgba(248,113,113,0.12); }
</style>
