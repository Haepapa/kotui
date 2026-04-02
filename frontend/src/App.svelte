<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Sidebar from './components/Sidebar.svelte';
  import GroupChat from './components/GroupChat.svelte';
  import EngineRoom from './components/EngineRoom.svelte';
  import HeartbeatBar from './components/HeartbeatBar.svelte';
  import ModeToggle from './components/ModeToggle.svelte';
  import CommandInput from './components/CommandInput.svelte';
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
</script>

<div class="app">
  <!-- macOS-style draggable title bar -->
  <div class="titlebar" style="--wails-draggable:drag">
    <span class="app-name">Kotui</span>
    <div class="titlebar-right">
      {#if wr.errorBanner}
        <span class="error-banner">⚠️ {wr.errorBanner}</span>
      {/if}
      <ModeToggle mode={wr.viewMode} ontoggle={toggleMode} />
    </div>
  </div>

  <!-- Main 3-column layout -->
  <div class="body">
    <Sidebar />

    <div class="main-panel">
      <GroupChat messages={visibleMessages()} mode={wr.viewMode} />
      <CommandInput />
    </div>

    {#if wr.viewMode === 'dev'}
      <EngineRoom messages={engineRoomMessages()} />
    {/if}
  </div>

  <!-- Bottom heartbeat bar -->
  <HeartbeatBar heartbeat={wr.heartbeat} isBusy={wr.isBusy} />
</div>

<style>
  :global(*) { box-sizing: border-box; }
  :global(body) {
    margin: 0;
    background: #0f141e;
    color: #e2e8f0;
    font-family: -apple-system, 'Inter', 'Segoe UI', system-ui, sans-serif;
    -webkit-font-smoothing: antialiased;
    height: 100vh;
    overflow: hidden;
  }
  :global(html) { height: 100%; }
  :global(#app) { height: 100%; }

  .app {
    display: flex;
    flex-direction: column;
    height: 100vh;
    overflow: hidden;
  }
  .titlebar {
    height: 50px;
    background: #0b0f1a;
    border-bottom: 1px solid #1e293b;
    display: flex;
    align-items: center;
    padding: 0 1rem 0 5.5rem; /* space for traffic lights */
    flex-shrink: 0;
    user-select: none;
  }
  .app-name {
    font-size: 0.875rem;
    font-weight: 700;
    color: #7dd3fc;
    letter-spacing: 0.05em;
  }
  .titlebar-right {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }
  .error-banner {
    font-size: 0.75rem;
    color: #fca5a5;
    background: #450a0a;
    border-radius: 4px;
    padding: 0.2rem 0.6rem;
    max-width: 300px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .body {
    flex: 1;
    display: flex;
    overflow: hidden;
  }
  .main-panel {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
</style>

