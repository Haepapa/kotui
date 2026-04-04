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
  let saveStatus = $state<Record<TabKey, SaveStatus>>({ soul: 'idle', persona: 'idle', skills: 'idle' });
  let saveError  = $state<Record<TabKey, string>>({ soul: '', persona: '', skills: '' });

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
    <div class="load-error">⚠ Could not load brain files: {loadError}</div>
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
          <span class="status-error">{saveError[activeTab]}</span>
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
    padding: 1.25rem;
    color: #f87171;
    font-size: 0.9rem;
  }

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
  .status-error { font-size: 0.875rem; color: #f87171; }
</style>
