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

  let theme = $state<'dark' | 'light'>('dark');

  onMount(() => {
    const saved = localStorage.getItem('kotui-theme');
    if (saved === 'light') theme = 'light';
    initWarRoom();
  });
  onDestroy(destroyWarRoom);

  $effect(() => {
    if (theme === 'light') {
      document.documentElement.setAttribute('data-theme', 'light');
    } else {
      document.documentElement.removeAttribute('data-theme');
    }
    localStorage.setItem('kotui-theme', theme);
  });

  function toggleTheme() { theme = theme === 'dark' ? 'light' : 'dark'; }

  // Active channel title
  const activeProject = $derived(wr.projects.find(p => p.id === wr.activeProjectID));
</script>

<div class="shell">
  <!-- Narrow icon rail (like Slack's app switcher) -->
  <div class="rail">
    <div class="rail-logo" title="Kōtui">K</div>
    <div class="rail-spacer"></div>
    <button class="rail-btn" title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'} onclick={toggleTheme}>
      {#if theme === 'dark'}
        <!-- Sun icon -->
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>
      {:else}
        <!-- Moon icon -->
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
      {/if}
    </button>
    <button class="rail-btn" title="Settings (coming soon)" disabled>
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
    </button>
    <button class="rail-btn" title="Help (coming soon)" disabled>
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75"><circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
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
  .shell {
    display: flex;
    width: 100%;
    height: 100%;
    overflow: hidden;
    background: var(--bg-app);
    color: var(--text-primary);
  }

  /* Icon rail */
  .rail {
    width: 52px;
    flex-shrink: 0;
    background: var(--bg-rail);
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 52px 0 14px;
    overflow: hidden;
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
    color: var(--rail-icon-color);
    transition: background 0.15s, color 0.15s;
    flex-shrink: 0;
  }
  .rail-btn:hover:not(:disabled) {
    background: var(--rail-icon-hover-bg);
    color: var(--rail-icon-hover-color);
  }
  .rail-btn:disabled { opacity: 0.28; cursor: default; }

  /* Content — everything right of sidebar */
  .content {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-width: 0;
    min-height: 0;
    background: var(--bg-content);
    border-radius: 0 12px 0 0;
    /* Push content below macOS traffic lights */
    padding-top: var(--titlebar-h);
  }

  .channel-header {
    height: 46px;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    padding: 0 1.25rem;
    border-bottom: 1px solid var(--border-subtle);
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
  .channel-hash { color: var(--channel-hash); font-size: 1.15rem; flex-shrink: 0; }
  .channel-name {
    font-size: 1rem;
    font-weight: 600;
    color: var(--text-heading);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .channel-name-placeholder { font-size: 1rem; color: var(--text-muted); }
  .channel-desc {
    font-size: 0.875rem;
    color: var(--channel-desc);
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


