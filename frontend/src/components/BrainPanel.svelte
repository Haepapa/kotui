<script lang="ts">
  import { onMount } from 'svelte';
  import { getAgentBrainFiles, saveAgentBrainFile, getAgentJournalFiles, getAgentJournalFile } from '../lib/warroom';
  import { wr, switchFromBrain } from '../stores/warroom.svelte';

  let { agentID }: { agentID: string } = $props();

  type TabKey = 'soul' | 'persona' | 'skills' | 'journal';
  type SaveStatus = 'idle' | 'saving' | 'saved' | 'error';

  let activeTab = $state<TabKey>('soul');
  let soul    = $state('');
  let persona = $state('');
  let skills  = $state('');
  let loadError = $state('');
  let loadErrorCopied = $state(false);
  let saveStatus = $state<Record<TabKey, SaveStatus>>({ soul: 'idle', persona: 'idle', skills: 'idle', journal: 'idle' });
  let saveError  = $state<Record<TabKey, string>>({ soul: '', persona: '', skills: '', journal: '' });
  let saveErrorCopied = $state<Record<TabKey, boolean>>({ soul: false, persona: false, skills: false, journal: false });

  // Journal state
  let journalFiles  = $state<string[]>([]);
  let selectedJournalFile = $state('');
  let journalContent = $state('');
  let journalSearch  = $state('');
  let journalLoading = $state(false);
  let journalError   = $state('');
  let journalDropdownOpen = $state(false);

  const filteredJournalFiles = $derived(
    journalSearch.trim()
      ? journalFiles.filter(f => f.toLowerCase().includes(journalSearch.toLowerCase()))
      : journalFiles
  );

  const tabLabels: Record<TabKey, string> = {
    soul:    '✦ Soul',
    persona: '◎ Persona',
    skills:  '⚡ Skills',
    journal: '📓 Journal',
  };

  const tabHints: Record<TabKey, string> = {
    soul:    'Core values and purpose. These are the agent\'s deepest commitments.',
    persona: 'Communication style, personality, and name. How the agent expresses itself.',
    skills:  'Known capabilities, limitations, and capability ceiling. What the agent can and cannot do.',
    journal: 'Read-only experience log. Entries are written by the agent after completing tasks.',
  };

  function currentContent() {
    if (activeTab === 'soul')    return soul;
    if (activeTab === 'persona') return persona;
    if (activeTab === 'skills')  return skills;
    return journalContent;
  }

  function setContent(val: string) {
    if (activeTab === 'soul')         soul    = val;
    else if (activeTab === 'persona') persona = val;
    else if (activeTab === 'skills')  skills  = val;
    // journal is read-only
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

  async function loadJournalFiles() {
    journalError = '';
    try {
      journalFiles = (await getAgentJournalFiles(agentID)).sort().reverse(); // newest first
      if (journalFiles.length > 0 && !selectedJournalFile) {
        await selectJournalFile(journalFiles[0]);
      }
    } catch (e) {
      journalError = e instanceof Error ? e.message : String(e);
    }
  }

  async function selectJournalFile(filename: string) {
    selectedJournalFile = filename;
    journalDropdownOpen = false;
    journalSearch = '';
    journalLoading = true;
    journalContent = '';
    journalError = '';
    try {
      journalContent = await getAgentJournalFile(agentID, filename);
    } catch (e) {
      journalError = e instanceof Error ? e.message : String(e);
    } finally {
      journalLoading = false;
    }
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

  async function handleTabChange(tab: TabKey) {
    activeTab = tab;
    if (tab === 'journal' && journalFiles.length === 0) {
      await loadJournalFiles();
    }
  }

  async function handleSave() {
    if (activeTab === 'journal') return; // read-only
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
          onclick={() => handleTabChange(tab)}
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

      {#if activeTab === 'journal'}
        <!-- Journal file navigator -->
        {#if journalFiles.length === 0 && !journalLoading}
          <p class="journal-empty">No journal entries yet. Entries are created automatically after the agent completes tasks.</p>
        {:else}
          <!-- Searchable file picker -->
          <div class="journal-picker">
            <div class="journal-dropdown-wrap">
              <button
                class="journal-dropdown-btn"
                onclick={() => { journalDropdownOpen = !journalDropdownOpen; }}
              >
                <span class="journal-selected-label">{selectedJournalFile || 'Select entry…'}</span>
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                  <polyline points="6 9 12 15 18 9"/>
                </svg>
              </button>
              {#if journalDropdownOpen}
                <div class="journal-dropdown-panel">
                  <input
                    class="journal-search"
                    type="text"
                    placeholder="Search entries…"
                    bind:value={journalSearch}
                    autofocus
                  />
                  <div class="journal-file-list">
                    {#each filteredJournalFiles as f}
                      <button
                        class="journal-file-item"
                        class:active={f === selectedJournalFile}
                        onclick={() => selectJournalFile(f)}
                      >{f}</button>
                    {/each}
                    {#if filteredJournalFiles.length === 0}
                      <span class="journal-no-results">No matches</span>
                    {/if}
                  </div>
                </div>
              {/if}
            </div>
          </div>

          {#if journalLoading}
            <p class="journal-loading">Loading…</p>
          {:else if journalError}
            <p class="journal-error">⚠ {journalError}</p>
          {:else}
            <textarea
              class="brain-textarea"
              value={journalContent}
              readonly
              spellcheck={false}
            ></textarea>
          {/if}
        {/if}
      {:else}
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
      {/if}
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

  /* ── Journal ── */
  .journal-picker {
    flex-shrink: 0;
    position: relative;
  }

  .journal-dropdown-wrap {
    position: relative;
    display: inline-block;
    width: 100%;
  }

  .journal-dropdown-btn {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    background: var(--bg-surface);
    border: 1px solid var(--border-input);
    border-radius: 8px;
    color: var(--text-heading);
    cursor: pointer;
    font-size: 0.875rem;
    padding: 0.45rem 0.75rem;
    text-align: left;
    transition: border-color 0.12s;
  }
  .journal-dropdown-btn:hover { border-color: var(--accent); }
  .journal-selected-label { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .journal-dropdown-panel {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    right: 0;
    background: var(--bg-sidebar);
    border: 1px solid var(--border-input);
    border-radius: 8px;
    box-shadow: 0 8px 24px rgba(0,0,0,0.35);
    z-index: 50;
    overflow: hidden;
  }

  .journal-search {
    display: block;
    width: 100%;
    background: var(--bg-surface);
    border: none;
    border-bottom: 1px solid var(--border-subtle);
    color: var(--text-primary);
    font-size: 0.8125rem;
    outline: none;
    padding: 0.5rem 0.75rem;
    user-select: text;
    -webkit-user-select: text;
  }

  .journal-file-list {
    max-height: 220px;
    overflow-y: auto;
    padding: 0.25rem 0;
  }
  .journal-file-list::-webkit-scrollbar { width: 4px; }
  .journal-file-list::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 4px; }

  .journal-file-item {
    display: block;
    width: 100%;
    background: none;
    border: none;
    color: var(--text-secondary);
    cursor: pointer;
    font-size: 0.8125rem;
    font-family: 'Menlo', 'Monaco', monospace;
    padding: 0.375rem 0.75rem;
    text-align: left;
    transition: background 0.1s, color 0.1s;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .journal-file-item:hover { background: var(--bg-hover); color: var(--text-heading); }
  .journal-file-item.active { background: var(--bg-active); color: var(--accent); }

  .journal-no-results {
    display: block;
    color: var(--text-muted);
    font-size: 0.8125rem;
    padding: 0.5rem 0.75rem;
  }

  .journal-empty {
    color: var(--text-muted);
    font-size: 0.875rem;
    margin: 0;
  }
  .journal-loading { color: var(--text-muted); font-size: 0.875rem; margin: 0; }
  .journal-error { color: #f87171; font-size: 0.875rem; margin: 0; user-select: text; }

  /* ── Shared textarea ── */
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
  .brain-textarea:focus:not([readonly]) { border-color: var(--accent); }
  .brain-textarea[readonly] { cursor: default; opacity: 0.85; }

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
