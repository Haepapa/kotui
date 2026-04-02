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
  :global(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :global(html, body, #app) { height: 100%; overflow: hidden; }
  :global(body) {
    background: #1a1d23;
    color: #e2e8f0;
    font-family: -apple-system, 'SF Pro Text', 'Segoe UI', system-ui, sans-serif;
    font-size: 14px;
    line-height: 1.5;
    -webkit-font-smoothing: antialiased;
  }
  :global(button) { font-family: inherit; cursor: pointer; }

  .shell {
    display: flex;
    height: 100vh;
    overflow: hidden;
  }

  /* Icon rail */
  .rail {
    width: 56px;
    flex-shrink: 0;
    background: #111318;
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 0.75rem 0;
    gap: 0.25rem;
    /* macOS traffic lights sit over this area */
    padding-top: 44px;
    height: 100%;
    overflow-y: auto;
    overflow-x: hidden;
  }
  .rail::-webkit-scrollbar { display: none; }
  .rail-logo {
    width: 36px;
    height: 36px;
    border-radius: 10px;
    background: linear-gradient(135deg, #3b82f6 0%, #6366f1 100%);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 1rem;
    font-weight: 800;
    color: #fff;
    margin-bottom: 0.75rem;
    flex-shrink: 0;
  }
  .rail-spacer { flex: 1; }
  .rail-btn {
    width: 36px;
    height: 36px;
    border-radius: 8px;
    background: none;
    border: none;
    color: #475569;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background 0.15s, color 0.15s;
  }
  .rail-btn:hover:not(:disabled) { background: #1e2029; color: #94a3b8; }
  .rail-btn:disabled { opacity: 0.3; cursor: default; }

  /* Content area */
  .content {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-width: 0;
    min-height: 0;
    background: #1a1d23;
  }
  .channel-header {
    height: 48px;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    padding: 0 1rem;
    border-bottom: 1px solid #2a2d35;
    gap: 0.75rem;
    background: #1a1d23;
  }
  .channel-title {
    display: flex;
    align-items: baseline;
    gap: 0.375rem;
    overflow: hidden;
    flex: 1;
    min-width: 0;
  }
  .channel-hash { color: #475569; font-size: 1.125rem; flex-shrink: 0; }
  .channel-name {
    font-size: 0.9375rem;
    font-weight: 600;
    color: #e2e8f0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .channel-name-placeholder {
    font-size: 0.9375rem;
    color: #475569;
  }
  .channel-desc {
    font-size: 0.8125rem;
    color: #64748b;
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
    background: #450a0a;
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
  }</style>



