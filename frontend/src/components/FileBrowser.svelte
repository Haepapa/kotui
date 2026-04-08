<script lang="ts">
  import { onMount } from 'svelte';
  import { wr, refreshFiles } from '../stores/warroom.svelte';
  import { readSandboxFile, deleteSandboxFile, renameSandboxFile, revealSandboxFile } from '../lib/warroom';
  import type { FileEntry } from '../lib/types';

  let selectedFile = $state<FileEntry | null>(null);
  let fileContent = $state('');
  let isLoading = $state(false);
  let loadError = $state('');
  let isBinary = $state(false);

  // Rename state
  let renamingPath = $state<string | null>(null);
  let renameValue = $state('');

  // Delete confirmation
  let deletingEntry = $state<FileEntry | null>(null);

  // Hover tracking
  let hoveredPath = $state<string | null>(null);

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
        isBinary = true;
        loadError = 'Binary file — cannot preview';
      }
    } finally {
      isLoading = false;
    }
  }

  function startRename(entry: FileEntry) {
    renamingPath = entry.path;
    renameValue = entry.name;
  }

  async function commitRename(entry: FileEntry) {
    const newName = renameValue.trim();
    if (!newName || newName === entry.name) {
      renamingPath = null;
      return;
    }
    try {
      await renameSandboxFile(entry.path, newName);
      if (selectedFile?.path === entry.path) {
        const dir = entry.path.includes('/') ? entry.path.slice(0, entry.path.lastIndexOf('/') + 1) : '';
        selectedFile = { ...selectedFile, path: dir + newName, name: newName };
      }
      await refreshFiles();
    } catch (e: any) {
      alert(`Rename failed: ${e?.message ?? e}`);
    } finally {
      renamingPath = null;
    }
  }

  async function confirmDelete() {
    if (!deletingEntry) return;
    const entry = deletingEntry;
    deletingEntry = null;
    try {
      await deleteSandboxFile(entry.path);
      if (selectedFile?.path === entry.path) {
        selectedFile = null;
        fileContent = '';
      }
      await refreshFiles();
    } catch (e: any) {
      alert(`Delete failed: ${e?.message ?? e}`);
    }
  }

  async function handleReveal(entry: FileEntry) {
    try {
      await revealSandboxFile(entry.path);
    } catch (e: any) {
      alert(`Could not open location: ${e?.message ?? e}`);
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
            <!-- Directory row -->
            <div
              class="tree-dir"
              style="padding-left: {depth(entry) * 14 + 8}px"
              onmouseenter={() => hoveredPath = entry.path}
              onmouseleave={() => hoveredPath = null}
              role="none"
            >
              <svg class="dir-icon-svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
              </svg>
              <span class="dir-name">{entry.name}/</span>
              {#if hoveredPath === entry.path}
                <div class="row-actions">
                  <button class="row-action-btn" title="Open location" onclick={() => handleReveal(entry)}>
                    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/>
                    </svg>
                  </button>
                  <button class="row-action-btn danger" title="Delete folder" onclick={() => deletingEntry = entry}>
                    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                      <polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M9 6V4h6v2"/>
                    </svg>
                  </button>
                </div>
              {/if}
            </div>
          {:else}
            <!-- File row -->
            <div
              class="tree-file-wrap"
              class:selected={selectedFile?.path === entry.path}
              style="padding-left: {depth(entry) * 14 + 8}px"
              onmouseenter={() => hoveredPath = entry.path}
              onmouseleave={() => hoveredPath = null}
              role="none"
            >
              {#if renamingPath === entry.path}
                <!-- Inline rename input -->
                <svg class="file-icon-svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/>
                </svg>
                <input
                  class="rename-input"
                  bind:value={renameValue}
                  onkeydown={(e) => { if (e.key === 'Enter') commitRename(entry); if (e.key === 'Escape') renamingPath = null; }}
                  onblur={() => commitRename(entry)}
                  autofocus
                />
              {:else}
                <button
                  class="tree-file"
                  onclick={() => handleSelect(entry)}
                  title={entry.path}
                >
                  <svg class="file-icon-svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/>
                  </svg>
                  <span class="file-name-tree">{entry.name}</span>
                  <span class="file-meta">{formatSize(entry.size)}</span>
                </button>
                {#if hoveredPath === entry.path}
                  <div class="row-actions">
                    <button class="row-action-btn" title="Open location" onclick={() => handleReveal(entry)}>
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/>
                      </svg>
                    </button>
                    <button class="row-action-btn" title="Rename" onclick={() => startRename(entry)}>
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
                      </svg>
                    </button>
                    <button class="row-action-btn danger" title="Delete file" onclick={() => deletingEntry = entry}>
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                        <polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M9 6V4h6v2"/>
                      </svg>
                    </button>
                  </div>
                {/if}
              {/if}
            </div>
          {/if}
        {/each}
      {/if}
    </div>
  </aside>

  <!-- Right panel: file content viewer -->
  <div class="viewer-panel">
    {#if !selectedFile}
      <div class="viewer-placeholder">
        <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round" style="color: var(--text-muted)">
          <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
        </svg>
        <span class="placeholder-text">Select a file to view its contents</span>
      </div>
    {:else}
      <div class="viewer-header">
        <div class="viewer-header-left">
          <span class="viewer-path">{selectedFile.path}</span>
          <span class="viewer-info">{formatSize(selectedFile.size)} · {formatDate(selectedFile.mod_time)}</span>
        </div>
        <div class="viewer-header-actions">
          <button class="viewer-action-btn" title="Open location" onclick={() => handleReveal(selectedFile!)}>
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/>
            </svg>
            Open Location
          </button>
          <button class="viewer-action-btn" title="Rename" onclick={() => startRename(selectedFile!)}>
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
            </svg>
            Rename
          </button>
          <button class="viewer-action-btn danger" title="Delete" onclick={() => deletingEntry = selectedFile}>
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M9 6V4h6v2"/>
            </svg>
            Delete
          </button>
        </div>
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

<!-- Delete confirmation dialog -->
{#if deletingEntry}
  <div class="modal-backdrop" role="none" onclick={() => deletingEntry = null}>
    <div class="modal" role="dialog" aria-modal="true" onclick={(e) => e.stopPropagation()}>
      <div class="modal-title">Delete {deletingEntry.is_dir ? 'folder' : 'file'}?</div>
      <div class="modal-body">
        <strong>{deletingEntry.name}</strong>{deletingEntry.is_dir ? ' and all its contents' : ''} will be permanently deleted.
      </div>
      <div class="modal-actions">
        <button class="modal-btn" onclick={() => deletingEntry = null}>Cancel</button>
        <button class="modal-btn danger" onclick={confirmDelete}>Delete</button>
      </div>
    </div>
  </div>
{/if}

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

  .dir-icon-svg { flex-shrink: 0; color: var(--text-muted); }
  .file-icon-svg { flex-shrink: 0; color: var(--text-muted); }

  .tree-dir {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    padding-top: 0.35rem;
    padding-bottom: 0.2rem;
    padding-right: 0.375rem;
    user-select: none;
    position: relative;
  }
  .dir-name {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 1;
  }

  /* File row wrapper — holds the button + hover actions */
  .tree-file-wrap {
    display: flex;
    align-items: center;
    padding-right: 0.375rem;
    position: relative;
    min-height: 28px;
    transition: background 0.1s;
  }
  .tree-file-wrap:hover { background: var(--bg-hover); }
  .tree-file-wrap.selected { background: var(--bg-active); }

  .tree-file {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    flex: 1;
    min-width: 0;
    text-align: left;
    background: none;
    border: none;
    cursor: pointer;
    padding-top: 0.25rem;
    padding-bottom: 0.25rem;
    color: var(--nav-item-color);
  }
  .tree-file-wrap.selected .tree-file { color: var(--nav-active-color); }

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
    padding-left: 0.25rem;
  }

  /* Hover action buttons on rows */
  .row-actions {
    display: flex;
    align-items: center;
    gap: 2px;
    flex-shrink: 0;
    margin-left: 2px;
  }
  .row-action-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-muted);
    padding: 3px 4px;
    border-radius: 4px;
    transition: background 0.1s, color 0.1s;
  }
  .row-action-btn:hover { background: var(--bg-hover); color: var(--text-secondary); }
  .row-action-btn.danger:hover { color: #f87171; background: rgba(248,113,113,0.1); }

  /* Inline rename input */
  .rename-input {
    flex: 1;
    font-size: 0.8125rem;
    background: var(--bg-input, var(--bg-surface));
    border: 1px solid var(--accent-color, #7c6af7);
    border-radius: 4px;
    color: var(--text-primary);
    padding: 1px 5px;
    outline: none;
    min-width: 0;
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
  .placeholder-text { font-size: 0.875rem; }

  .viewer-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.5rem 0.875rem;
    border-bottom: 1px solid var(--border-subtle);
    flex-shrink: 0;
    background: var(--bg-surface);
    flex-wrap: wrap;
  }
  .viewer-header-left {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
    min-width: 0;
  }
  .viewer-path {
    font-size: 0.8125rem;
    font-weight: 600;
    color: var(--text-heading);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .viewer-info {
    font-size: 0.75rem;
    color: var(--text-muted);
    white-space: nowrap;
  }
  .viewer-header-actions {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    flex-shrink: 0;
  }
  .viewer-action-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    font-size: 0.75rem;
    padding: 3px 8px;
    border-radius: 5px;
    border: 1px solid var(--border-subtle);
    background: none;
    cursor: pointer;
    color: var(--text-secondary);
    transition: background 0.12s, color 0.12s, border-color 0.12s;
  }
  .viewer-action-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
  .viewer-action-btn.danger { color: var(--text-muted); }
  .viewer-action-btn.danger:hover { color: #f87171; border-color: #f87171; background: rgba(248,113,113,0.08); }

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
    user-select: text;
    -webkit-user-select: text;
    cursor: text;
  }

  /* ── Delete confirmation modal ── */
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.45);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }
  .modal {
    background: var(--bg-surface);
    border: 1px solid var(--border-subtle);
    border-radius: 10px;
    padding: 1.25rem 1.5rem;
    min-width: 300px;
    max-width: 420px;
    box-shadow: 0 8px 32px rgba(0,0,0,0.35);
  }
  .modal-title {
    font-size: 0.9375rem;
    font-weight: 600;
    color: var(--text-heading);
    margin-bottom: 0.625rem;
  }
  .modal-body {
    font-size: 0.875rem;
    color: var(--text-secondary);
    margin-bottom: 1.125rem;
    line-height: 1.5;
  }
  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }
  .modal-btn {
    font-size: 0.875rem;
    padding: 0.375rem 0.875rem;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    background: none;
    cursor: pointer;
    color: var(--text-secondary);
    transition: background 0.12s;
  }
  .modal-btn:hover { background: var(--bg-hover); }
  .modal-btn.danger { background: #dc2626; color: #fff; border-color: #dc2626; }
  .modal-btn.danger:hover { background: #b91c1c; border-color: #b91c1c; }
</style>
