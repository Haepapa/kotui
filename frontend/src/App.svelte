<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Sidebar from './components/Sidebar.svelte';
  import ChatArea from './components/ChatArea.svelte';
  import EngineRoom from './components/EngineRoom.svelte';
  import ModeToggle from './components/ModeToggle.svelte';
  import Settings from './components/Settings.svelte';
  import CompanyIdentityEditor from './components/CompanyIdentityEditor.svelte';
  import BrainPanel from './components/BrainPanel.svelte';
  import {
    wr,
    initWarRoom,
    destroyWarRoom,
    toggleMode,
    switchToSettings,
    switchToIdentity,
    switchToChat,
    switchToBrain,
    agentName,
  } from './stores/warroom.svelte';

  import { loadAccentColor } from './lib/theme';

  let theme = $state<'dark' | 'light'>('dark');

  onMount(() => {
    const saved = localStorage.getItem('kotui-theme');
    if (saved === 'light') theme = 'light';
    loadAccentColor();
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

  // Derived message lists — kept here (not imported fns) to guarantee Svelte 5 reactive tracking
  const visibleMsgs = $derived(
    wr.viewMode === 'boss'
      ? wr.messages.filter(m => m.tier === 'summary')
      : wr.messages
  );
  const engineMsgs = $derived(
    wr.messages.filter(m => m.tier === 'raw')
  );
  const dmVisibleMsgs = $derived(
    wr.dmConvMsgs[wr.activeDMConvID] ?? []
  );
  const dmEngineMsgs = $derived(
    wr.dmConvRaw[wr.activeDMConvID] ?? []
  );
  const dmBusy = $derived(wr.dmConvBusy[wr.activeDMConvID] ?? false);
  const dmStream = $derived(wr.dmConvStream[wr.activeDMConvID] ?? '');
  const channelTitle = $derived(() => {
    if (wr.activeView === 'settings') return 'Infrastructure Office';
    if (wr.activeView === 'identity') return 'Company Identity';
    if (wr.activeView === 'dm') return `@ ${agentName(wr.activeDMAgentID)}`;
    return null;
  });
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
    <button class="rail-btn" title="Settings" onclick={switchToSettings}>
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
    </button>
    <button class="rail-btn" title="Company Identity" onclick={switchToIdentity}>
      <!-- Building / office icon -->
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75"><path d="M6 22V4a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v18Z"/><path d="M6 12H4a2 2 0 0 0-2 2v6a2 2 0 0 0 2 2h2"/><path d="M18 9h2a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2h-2"/><line x1="10" y1="6" x2="10" y2="6"/><line x1="14" y1="6" x2="14" y2="6"/><line x1="10" y1="10" x2="10" y2="10"/><line x1="14" y1="10" x2="14" y2="10"/><line x1="10" y1="14" x2="10" y2="14"/><line x1="14" y1="14" x2="14" y2="14"/></svg>
    </button>
  </div>

  <!-- Channel / agent list -->
  <Sidebar />

  <!-- Main content -->
  <div class="content">
    <!-- Channel header — the only drag region in the web layer -->
    <header class="channel-header" style="--wails-draggable:drag">
      <div class="channel-title" style="--wails-draggable:drag">
        {#if wr.activeView === 'settings'}
          <!-- no title — settings page has its own heading -->
        {:else if wr.activeView === 'identity'}
          <!-- no title — identity editor has its own heading -->
        {:else if wr.activeView === 'brain'}
          <!-- no title — brain panel has its own heading -->
        {:else if wr.activeView === 'dm'}
          <span class="channel-hash">@</span>
          <span class="channel-name">{agentName(wr.activeDMAgentID)}</span>
        {:else if activeProject}
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
        {#if wr.activeView === 'dm'}
          <button class="brain-btn" title="View & edit {agentName(wr.activeDMAgentID)}'s brain files" onclick={() => switchToBrain(wr.activeDMAgentID)}>
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
              <path d="M9.5 2a2.5 2.5 0 0 1 5 0"/>
              <path d="M12 2v2"/>
              <path d="M4.5 9a2.5 2.5 0 0 0 0 5"/>
              <path d="M19.5 9a2.5 2.5 0 0 1 0 5"/>
              <path d="M4.5 9C4.5 6 7 4 12 4s7.5 2 7.5 5v6c0 3-2.5 5-7.5 5s-7.5-2-7.5-5V9z"/>
              <line x1="9" y1="12" x2="9" y2="12.01"/>
              <line x1="15" y1="12" x2="15" y2="12.01"/>
              <path d="M9 16s1 1 3 1 3-1 3-1"/>
            </svg>
          </button>
        {/if}
        {#if wr.activeView === 'chat' || wr.activeView === 'dm'}
          <ModeToggle mode={wr.viewMode} ontoggle={toggleMode} />
        {/if}
      </div>
    </header>

    <!-- Main body — route by activeView -->
    <div class="body">
      <!-- Floating error toast — readable, wraps, auto-dismisses -->
      {#if wr.errorBanner}
        <div class="error-toast" style="--wails-draggable:no-drag">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="flex-shrink:0;margin-top:1px"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
          <span>{wr.errorBanner}</span>
        </div>
      {/if}
      {#if wr.activeView === 'settings'}
        <Settings />
      {:else if wr.activeView === 'identity'}
        <CompanyIdentityEditor />
      {:else if wr.activeView === 'brain'}
        <BrainPanel agentID={wr.activeBrainAgentID} />
      {:else if wr.activeView === 'dm'}
        <ChatArea
          messages={dmVisibleMsgs}
          mode={wr.viewMode}
          isBusy={dmBusy}
          streamContent={dmStream}
          heartbeat={wr.heartbeat}
          isDM={true}
          dmAgentID={wr.activeDMAgentID}
        />
        {#if wr.viewMode === 'dev'}
          <EngineRoom messages={dmEngineMsgs} />
        {/if}
      {:else}
        <ChatArea messages={visibleMsgs} mode={wr.viewMode} isBusy={wr.isBusy} heartbeat={wr.heartbeat} streamContent={wr.channelStream} />
        {#if wr.viewMode === 'dev'}
          <EngineRoom messages={engineMsgs} />
        {/if}
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
    background: linear-gradient(145deg, var(--logo-grad-start) 0%, var(--logo-grad-end) 100%);
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
    position: relative;
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

  .brain-btn {
    width: 30px;
    height: 30px;
    border-radius: 7px;
    color: var(--text-muted);
    background: none;
    border: none;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background 0.15s, color 0.15s;
  }
  .brain-btn:hover { background: var(--bg-hover); color: var(--text-secondary); }

  /* Floating error toast — replaces the old truncating pill */
  .error-toast {
    position: absolute;
    top: calc(var(--titlebar-h) + 54px);
    right: 1.25rem;
    z-index: 200;
    display: flex;
    align-items: flex-start;
    gap: 0.5rem;
    background: #3b0a0a;
    border: 1px solid rgba(248, 113, 113, 0.35);
    border-radius: 10px;
    padding: 0.625rem 0.875rem;
    max-width: 360px;
    box-shadow: 0 4px 20px rgba(0, 0, 0, 0.35);
    color: #fca5a5;
    font-size: 0.8125rem;
    line-height: 1.45;
    word-break: break-word;
    animation: toast-in 0.18s ease;
  }
  @keyframes toast-in {
    from { opacity: 0; transform: translateY(-6px); }
    to   { opacity: 1; transform: translateY(0); }
  }

  .body {
    flex: 1;
    display: flex;
    overflow: hidden;
    min-height: 0;
  }
</style>


