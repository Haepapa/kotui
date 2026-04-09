<script lang="ts">
  import { onMount } from 'svelte';
  import { getCompanyIdentity, saveCompanyIdentity, getHandbook, saveHandbook, getTools, sendDirectMessage, type ToolInfo } from '../lib/warroom';
  import { switchToChat, openDM } from '../stores/warroom.svelte';

  type Tab = 'company' | 'handbook' | 'tools';
  let activeTab = $state<Tab>('company');

  // Company Identity state
  let companyContent = $state('');
  let companySaveStatus = $state<'idle' | 'confirming' | 'saving' | 'saved' | 'error'>('idle');
  let companyErrorMsg = $state('');

  // Handbook state
  let handbookContent = $state('');
  let handbookSaveStatus = $state<'idle' | 'saving' | 'saved' | 'error'>('idle');
  let handbookErrorMsg = $state('');

  // Tools state
  let tools: ToolInfo[] = $state([]);
  let toolsLoading = $state(false);
  let toolsError = $state('');
  let expandedTool: string | null = $state(null);

  // New tool panel state
  let showNewTool = $state(false);
  let newToolDescription = $state('');
  let startingNewTool = $state(false);

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

  $effect(() => {
    if (activeTab === 'tools' && tools.length === 0 && !toolsLoading) {
      loadTools();
    }
  });

  async function loadTools() {
    toolsLoading = true;
    toolsError = '';
    try {
      tools = await getTools();
    } catch (e) {
      toolsError = String(e);
    } finally {
      toolsLoading = false;
    }
  }

  function toggleTool(name: string) {
    expandedTool = expandedTool === name ? null : name;
  }

  function parseOperations(schema: any): string[] {
    try {
      const s = typeof schema === 'string' ? JSON.parse(schema) : schema;
      const opDesc = s?.properties?.operation?.description ?? '';
      if (opDesc) return opDesc.split('|').map((o: string) => o.trim());
      return Object.keys(s?.properties ?? {});
    } catch {
      return [];
    }
  }

  async function startToolCreation() {
    if (!newToolDescription.trim()) return;
    startingNewTool = true;
    try {
      const message = `I want to create a new MCP tool for Kōtui. Here's what I want it to do:\n\n${newToolDescription.trim()}\n\nPlease guide me through: naming the tool, defining its operations and parameters, and generating the Go implementation code. Start by asking any clarifying questions you need.`;
      await openDM('lead');
      await sendDirectMessage('lead', message);
      showNewTool = false;
      newToolDescription = '';
    } catch (e) {
      console.error('Failed to start tool creation:', e);
    } finally {
      startingNewTool = false;
    }
  }

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
    <h2>My Company</h2>
  </div>

  <div class="tabs">
    <button class="tab-btn" class:active={activeTab === 'company'} onclick={() => (activeTab = 'company')}>
      🏢 Identity
      {#if companySaveStatus === 'saved'}<span class="tab-saved">✓</span>{/if}
    </button>
    <button class="tab-btn" class:active={activeTab === 'handbook'} onclick={() => (activeTab = 'handbook')}>
      📋 Handbook
      {#if handbookSaveStatus === 'saved'}<span class="tab-saved">✓</span>{/if}
    </button>
    <button class="tab-btn" class:active={activeTab === 'tools'} onclick={() => (activeTab = 'tools')}>
      🛠️ Tools
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
  {:else if activeTab === 'handbook'}
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
  {:else}
    <div class="tab-body tools-tab">
      <div class="tools-header">
        <span class="tools-title">Available Tools</span>
        <button class="add-tool-btn" onclick={() => (showNewTool = !showNewTool)} title="Create a new tool">+</button>
      </div>

      {#if showNewTool}
        <div class="new-tool-panel">
          <div class="new-tool-header">
            <span class="new-tool-title">New Tool</span>
            <button class="close-btn" onclick={() => { showNewTool = false; newToolDescription = ''; }}>✕</button>
          </div>
          <p class="new-tool-hint">Describe what you want this tool to do:</p>
          <textarea
            class="new-tool-textarea"
            bind:value={newToolDescription}
            placeholder='e.g. "A tool that sends emails via SMTP"'
            rows={3}
          ></textarea>
          <p class="new-tool-subhint">The Lead agent will guide you through the design and implementation.</p>
          <div class="new-tool-actions">
            <button class="cancel-btn" onclick={() => { showNewTool = false; newToolDescription = ''; }}>Cancel</button>
            <button class="save-btn" onclick={startToolCreation} disabled={startingNewTool || !newToolDescription.trim()}>
              {startingNewTool ? 'Starting…' : 'Start →'}
            </button>
          </div>
        </div>
      {/if}

      {#if toolsLoading}
        <div class="tools-loading">
          <span class="spinner"></span>
          <span>Loading tools…</span>
        </div>
      {:else if toolsError}
        <div class="tools-error">{toolsError}</div>
      {:else if tools.length === 0}
        <div class="tools-empty">No tools registered.</div>
      {:else}
        <div class="tools-list">
          {#each tools as tool (tool.name)}
            {@const expanded = expandedTool === tool.name}
            {@const ops = parseOperations(tool.schema)}
            <div class="tool-card" class:expanded>
              <button class="tool-card-header" onclick={() => toggleTool(tool.name)}>
                <div class="tool-card-left">
                  <span class="tool-name">{tool.name}</span>
                  <span class="clearance-badge clearance-{tool.clearance}">{tool.clearance}</span>
                </div>
                <span class="tool-chevron">{expanded ? '▼' : '▶'}</span>
              </button>
              {#if !expanded}
                <p class="tool-desc-short">{tool.description}</p>
              {:else}
                <div class="tool-card-body">
                  <p class="tool-desc-full">{tool.description}</p>
                  {#if ops.length > 0}
                    <div class="tool-ops">
                      <span class="tool-ops-label">Operations / Parameters</span>
                      <ul class="tool-ops-list">
                        {#each ops as op}
                          <li>{op}</li>
                        {/each}
                      </ul>
                    </div>
                  {/if}
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
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

  /* Tools tab */
  .tools-tab { gap: 0.5rem; overflow-y: auto; }
  .tools-header { display: flex; align-items: center; justify-content: space-between; flex-shrink: 0; }
  .tools-title { font-size: 0.875rem; font-weight: 600; color: var(--text-heading); }
  .add-tool-btn { width: 28px; height: 28px; border-radius: 7px; background: var(--accent-btn); color: #e0e9ff; border: none; cursor: pointer; font-size: 1.1rem; line-height: 1; display: flex; align-items: center; justify-content: center; transition: background 0.15s; }
  .add-tool-btn:hover { background: var(--accent-btn-hover); }

  /* New tool panel */
  .new-tool-panel { background: var(--bg-surface); border: 1px solid var(--border-input); border-radius: 10px; padding: 1rem; display: flex; flex-direction: column; gap: 0.625rem; flex-shrink: 0; }
  .new-tool-header { display: flex; align-items: center; justify-content: space-between; }
  .new-tool-title { font-size: 0.875rem; font-weight: 600; color: var(--text-heading); }
  .close-btn { background: none; border: none; color: var(--text-muted); cursor: pointer; font-size: 0.875rem; padding: 2px 6px; border-radius: 5px; transition: background 0.12s, color 0.12s; }
  .close-btn:hover { background: var(--bg-hover); color: var(--text-heading); }
  .new-tool-hint { font-size: 0.8125rem; color: var(--text-muted); margin: 0; }
  .new-tool-textarea { background: var(--bg-app); border: 1px solid var(--border-input); border-radius: 7px; color: var(--text-heading); font-size: 0.875rem; font-family: inherit; line-height: 1.5; padding: 0.625rem 0.75rem; outline: none; resize: none; transition: border-color 0.15s; user-select: text; }
  .new-tool-textarea:focus { border-color: var(--accent); }
  .new-tool-subhint { font-size: 0.75rem; color: var(--text-muted); margin: 0; }
  .new-tool-actions { display: flex; gap: 0.5rem; justify-content: flex-end; }

  /* Loading / empty / error */
  .tools-loading { display: flex; align-items: center; gap: 0.5rem; color: var(--text-muted); font-size: 0.875rem; padding: 1rem 0; }
  .spinner { width: 14px; height: 14px; border: 2px solid var(--border-input); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.7s linear infinite; flex-shrink: 0; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .tools-error { font-size: 0.875rem; color: #f87171; padding: 0.5rem 0; }
  .tools-empty { font-size: 0.875rem; color: var(--text-muted); padding: 0.5rem 0; }

  /* Tool list */
  .tools-list { display: flex; flex-direction: column; gap: 0.375rem; overflow-y: auto; flex: 1; min-height: 0; }
  .tool-card { background: var(--bg-surface); border: 1px solid var(--border-subtle); border-radius: 9px; overflow: hidden; transition: border-color 0.12s; }
  .tool-card.expanded { border-color: var(--accent); }
  .tool-card-header { width: 100%; background: none; border: none; cursor: pointer; display: flex; align-items: center; justify-content: space-between; padding: 0.5rem 0.75rem; gap: 0.5rem; }
  .tool-card-left { display: flex; align-items: center; gap: 0.5rem; min-width: 0; flex: 1; }
  .tool-name { font-size: 0.8125rem; font-weight: 600; color: var(--text-heading); font-family: 'Menlo', 'Monaco', 'Courier New', monospace; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .tool-chevron { font-size: 0.6rem; color: var(--text-muted); flex-shrink: 0; }
  .tool-desc-short { font-size: 0.75rem; color: var(--text-muted); margin: 0; padding: 0 0.75rem 0.5rem; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .tool-card-body { padding: 0 0.75rem 0.75rem; display: flex; flex-direction: column; gap: 0.5rem; }
  .tool-desc-full { font-size: 0.8125rem; color: var(--text-secondary); margin: 0; line-height: 1.5; }
  .tool-ops { display: flex; flex-direction: column; gap: 0.25rem; }
  .tool-ops-label { font-size: 0.6875rem; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.04em; }
  .tool-ops-list { margin: 0; padding-left: 1.25rem; display: flex; flex-direction: column; gap: 0.125rem; }
  .tool-ops-list li { font-size: 0.75rem; color: var(--text-secondary); font-family: 'Menlo', 'Monaco', 'Courier New', monospace; }

  /* Clearance badges */
  .clearance-badge { font-size: 0.6rem; font-weight: 700; padding: 1px 5px; border-radius: 4px; text-transform: uppercase; letter-spacing: 0.05em; flex-shrink: 0; }
  .clearance-lead { background: rgba(251, 191, 36, 0.18); color: #fbbf24; border: 1px solid rgba(251, 191, 36, 0.35); }
  .clearance-specialist { background: rgba(96, 165, 250, 0.18); color: #60a5fa; border: 1px solid rgba(96, 165, 250, 0.35); }
  .clearance-badge:not(.clearance-lead):not(.clearance-specialist) { background: rgba(148, 163, 184, 0.18); color: #94a3b8; border: 1px solid rgba(148, 163, 184, 0.35); }
</style>
