<script lang="ts">
  import { onMount } from 'svelte';
  import { wr, refreshFiles } from '../stores/warroom.svelte';
  import { readSandboxFile } from '../lib/warroom';
  import type { FileEntry } from '../lib/types';

  let selectedFile = $state<FileEntry | null>(null);
  let fileContent = $state('');
  let isLoading = $state(false);
  let loadError = $state('');
  let isBinary = $state(false);

  onMount(() => {
    refreshFiles();
  });

  async function handleSelect(entry: FileEntry) {
    if (entry.is_dir) return;
    selectedFile = entry;
    fileContent = '';
    loadError = '';
    isBinary = false;
    isLoading = true;
    try {
      fileContent = await readSandboxFile(entry.path);
    } catch (e: any) {
      const msg = e?.message ?? String(e);
      if (msg.includes('too large')) {
        loadError = msg;
      } else {
        // Try to detect binary by checking for null bytes in partial content.
        isBinary = true;
        loadError = 'Binary file — cannot preview';
      }
    } finally {
      isLoading = false;
    }
  }

  function formatSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  }

  function formatDate(iso: string): string {
    try {
      return new Date(iso).toLocaleString(undefined, {
        month: 'short', day: 'numeric',
        hour: '2-digit', minute: '2-digit',
      });
    } catch { return iso; }
  }

  function depth(entry: FileEntry): number {
    return entry.path.split('/').length - 1;
  }

  /** Group entries so directory headers appear before their children. */
  const treeEntries = $derived(wr.files);
</script>

<div class="file-browser">
  <!-- Left panel: file tree -->
  <aside class="tree-panel">
    <div class="panel-header">
      <span class="panel-title">Workspace Files</span>
      <button class="refresh-btn" onclick={refreshFiles} title="Refresh file list">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="23 4 23 10 17 10"/>
          <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/>
        </svg>
        Refresh
      </button>
    </div>

    <div class="tree-list">
      {#if treeEntries.length === 0}
        <div class="tree-empty">No files yet. Agents will create files here when they write to the workspace.</div>
      {:else}
        {#each treeEntries as entry (entry.path)}
          {#if entry.is_dir}
            <div class="tree-dir" style="padding-left: {depth(entry) * 14 + 8}px">
              <span class="dir-icon">📁</span>
              <span class="dir-name">{entry.name}/</span>
            </div>
          {:else}
            <button
              class="tree-file"
              class:selected={selectedFile?.path === entry.path}
              style="padding-left: {depth(entry) * 14 + 8}px"
              onclick={() => handleSelect(entry)}
              title={entry.path}
            >
              <span class="file-icon-sm">📄</span>
              <span class="file-name-tree">{entry.name}</span>
              <span class="file-meta">{formatSize(entry.size)}</span>
            </button>
          {/if}
        {/each}
      {/if}
    </div>
  </aside>

  <!-- Right panel: file content viewer -->
  <div class="viewer-panel">
    {#if !selectedFile}
      <div class="viewer-placeholder">
        <span class="placeholder-icon">📂</span>
        <span class="placeholder-text">Select a file to view its contents</span>
      </div>
    {:else}
      <div class="viewer-header">
        <span class="viewer-path">{selectedFile.path}</span>
        <span class="viewer-info">{formatSize(selectedFile.size)} · {formatDate(selectedFile.mod_time)}</span>
      </div>
      <div class="viewer-body">
        {#if isLoading}
          <div class="viewer-state">Loading…</div>
        {:else if loadError}
          <div class="viewer-state viewer-error">{loadError}</div>
        {:else}
          <pre class="code-view">{fileContent}</pre>
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .file-browser {
    display: flex;
    flex: 1;
    overflow: hidden;
    min-height: 0;
    height: 100%;
  }

  /* ── Tree panel ── */
  .tree-panel {
    width: 260px;
    flex-shrink: 0;
    border-right: 1px solid var(--border-subtle);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    background: var(--bg-sidebar);
  }

  .panel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.75rem 0.875rem 0.5rem;
    border-bottom: 1px solid var(--border-subtle);
    flex-shrink: 0;
  }

  .panel-title {
    font-size: 0.6875rem;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--nav-label-color);
  }

  .refresh-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    font-size: 0.75rem;
    color: var(--text-muted);
    background: none;
    border: none;
    cursor: pointer;
    padding: 2px 6px;
    border-radius: 5px;
    transition: background 0.12s, color 0.12s;
  }
  .refresh-btn:hover { background: var(--bg-hover); color: var(--text-secondary); }

  .tree-list {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 0.375rem 0;
  }
  .tree-list::-webkit-scrollbar { width: 3px; }
  .tree-list::-webkit-scrollbar-track { background: transparent; }
  .tree-list::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 4px; }

  .tree-empty {
    font-size: 0.8125rem;
    color: var(--text-muted);
    padding: 1rem 0.875rem;
    line-height: 1.5;
  }

  .tree-dir {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    padding-top: 0.35rem;
    padding-bottom: 0.2rem;
    padding-right: 0.5rem;
    user-select: none;
  }
  .dir-icon { font-size: 0.875rem; flex-shrink: 0; }
  .dir-name {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .tree-file {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    cursor: pointer;
    padding-top: 0.25rem;
    padding-bottom: 0.25rem;
    padding-right: 0.5rem;
    color: var(--nav-item-color);
    min-height: 28px;
    transition: background 0.1s;
  }
  .tree-file:hover { background: var(--bg-hover); }
  .tree-file.selected { background: var(--bg-active); color: var(--nav-active-color); }

  .file-icon-sm { font-size: 0.875rem; flex-shrink: 0; }
  .file-name-tree {
    flex: 1;
    font-size: 0.8125rem;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .file-meta {
    font-size: 0.6875rem;
    color: var(--text-muted);
    flex-shrink: 0;
    margin-left: auto;
    padding-left: 0.375rem;
  }

  /* ── Viewer panel ── */
  .viewer-panel {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-width: 0;
    background: var(--bg-content);
  }

  .viewer-placeholder {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.625rem;
    color: var(--text-muted);
  }
  .placeholder-icon { font-size: 2.5rem; }
  .placeholder-text { font-size: 0.875rem; }

  .viewer-header {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.625rem 1rem;
    border-bottom: 1px solid var(--border-subtle);
    flex-shrink: 0;
    background: var(--bg-surface);
  }
  .viewer-path {
    font-size: 0.8125rem;
    font-weight: 600;
    color: var(--text-heading);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }
  .viewer-info {
    font-size: 0.75rem;
    color: var(--text-muted);
    white-space: nowrap;
    flex-shrink: 0;
  }

  .viewer-body {
    flex: 1;
    overflow: auto;
    min-height: 0;
  }
  .viewer-body::-webkit-scrollbar { width: 5px; height: 5px; }
  .viewer-body::-webkit-scrollbar-track { background: transparent; }
  .viewer-body::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 4px; }

  .viewer-state {
    padding: 1.5rem;
    font-size: 0.875rem;
    color: var(--text-muted);
  }
  .viewer-error { color: #f87171; }

  .code-view {
    margin: 0;
    padding: 1rem 1.25rem;
    font-family: 'Menlo', 'Monaco', 'Consolas', 'Liberation Mono', monospace;
    font-size: 0.8125rem;
    line-height: 1.6;
    color: var(--text-primary);
    white-space: pre;
    tab-size: 2;
  }
</style>
