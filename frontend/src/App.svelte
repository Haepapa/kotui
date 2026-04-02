<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Sidebar from './components/Sidebar.svelte';
  import ChatArea from './components/ChatArea.svelte';
  import EngineRoom from './components/EngineRoom.svelte';
  import ModeToggle from './components/ModeToggle.svelte';
  import {
    wr,
    initWarRoom,
    destroyWarRoom,
    visibleMessages,
    engineRoomMessages,
    toggleMode,
  } from './stores/warroom.svelte';

  onMount(initWarRoom);
  onDestroy(destroyWarRoom);

  // Active channel title
  const activeProject = $derived(wr.projects.find(p => p.id === wr.activeProjectID));
</script>

<div class="shell">
  <!-- Narrow icon rail (like Slack's app switcher) -->
  <div class="rail">
    <div class="rail-logo" title="Kōtui">K</div>
    <div class="rail-spacer"></div>
    <button class="rail-btn" title="Settings (coming soon)" disabled>
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
    </button>
    <button class="rail-btn" title="Help (coming soon)" disabled>
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75"><circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
    </button>
  </div>

  <!-- Channel / agent list -->
  <Sidebar />

  <!-- Main content -->
  <div class="content" style="--wails-draggable:drag">
    <!-- Channel header (draggable title bar on macOS) -->
    <header class="channel-header">
      <div class="channel-title">
        {#if activeProject}
          <span class="channel-hash">#</span>
          <span class="channel-name">{activeProject.name}</span>
          {#if activeProject.description}
            <span class="channel-desc">{activeProject.description}</span>
          {/if}
        {:else}
          <span class="channel-name-placeholder">Select a channel</span>
        {/if}
      </div>
      <div class="header-actions" style="--wails-draggable:no-drag">
        {#if wr.errorBanner}
          <span class="error-pill">{wr.errorBanner}</span>
        {/if}
        <ModeToggle mode={wr.viewMode} ontoggle={toggleMode} />
      </div>
    </header>

    <!-- Chat + optional dev panel -->
    <div class="body">
      <ChatArea messages={visibleMessages()} mode={wr.viewMode} isBusy={wr.isBusy} heartbeat={wr.heartbeat} />
      {#if wr.viewMode === 'dev'}
        <EngineRoom messages={engineRoomMessages()} />
      {/if}
    </div>
  </div>
</div>

<style>
  /* Shell fills the #app flex container from style.css */
  .shell {
    display: flex;
    width: 100%;
    height: 100%;
    overflow: hidden;
    background: #13151a;
    color: #d1d5db;
  }

  /* Icon rail — fixed width, traffic lights at top, action icons at bottom */
  .rail {
    width: 52px;
    flex-shrink: 0;
    background: #0f1117;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    /* macOS traffic lights need ~44px clearance */
    padding: 52px 0 12px;
    overflow: hidden; /* no scroll — icons stay pinned */
  }
  .rail-logo {
    width: 32px;
    height: 32px;
    border-radius: 9px;
    background: linear-gradient(145deg, #4f7cf7 0%, #7c6df7 100%);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.875rem;
    font-weight: 800;
    color: #fff;
    margin-bottom: 8px;
    flex-shrink: 0;
    letter-spacing: -0.02em;
  }
  .rail-spacer { flex: 1; min-height: 0; }
  .rail-btn {
    width: 34px;
    height: 34px;
    border-radius: 8px;
    color: #4b5563;
    transition: background 0.15s, color 0.15s;
    flex-shrink: 0;
  }
  .rail-btn:hover:not(:disabled) { background: #1a1d24; color: #9ca3af; }
  .rail-btn:disabled { opacity: 0.28; cursor: default; }

  /* Sidebar */
  /* (Sidebar component handles its own width/bg) */

  /* Content — everything right of sidebar */
  .content {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-width: 0;
    min-height: 0;
    /* Soft inner background */
    background: #171920;
    /* Top-right rounded corner only (left edge is the sidebar) */
    border-radius: 0 12px 0 0;
  }

  .channel-header {
    height: 46px;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    padding: 0 1rem;
    border-bottom: 1px solid rgba(255,255,255,0.05);
    gap: 0.75rem;
    background: transparent;
  }
  .channel-title {
    display: flex;
    align-items: baseline;
    gap: 0.375rem;
    overflow: hidden;
    flex: 1;
    min-width: 0;
  }
  .channel-hash { color: #374151; font-size: 1.1rem; flex-shrink: 0; }
  .channel-name {
    font-size: 0.9375rem;
    font-weight: 600;
    color: #e5e7eb;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .channel-name-placeholder { font-size: 0.9375rem; color: #374151; }
  .channel-desc {
    font-size: 0.8125rem;
    color: #4b5563;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .header-actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-shrink: 0;
  }
  .error-pill {
    font-size: 0.75rem;
    color: #fca5a5;
    background: #3b0a0a;
    border-radius: 99px;
    padding: 0.2rem 0.625rem;
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .body {
    flex: 1;
    display: flex;
    overflow: hidden;
    min-height: 0;
  }
</style>
