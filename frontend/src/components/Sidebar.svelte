<script lang="ts">
  import { createProject, switchProject, getActiveConversation, getMessages, getProjects } from '../lib/warroom';
  import { wr, openDM, refreshApprovals, renameChannel, archiveChannel } from '../stores/warroom.svelte';
  import ApprovalCard from './ApprovalCard.svelte';

  let showNewProject = $state(false);
  let newName = $state('');
  let newDesc = $state('');
  let nameInput = $state<HTMLInputElement | null>(null);

  // Per-channel rename state
  let renamingID = $state('');
  let renameNameVal = $state('');
  let renameDescVal = $state('');
  let renameInput = $state<HTMLInputElement | null>(null);

  // Which channel has its context menu open
  let menuOpenID = $state('');
  // Which channel is showing the archive confirmation inline
  let confirmArchiveID = $state('');

  $effect(() => {
    if (showNewProject && nameInput) nameInput.focus();
  });

  $effect(() => {
    if (renamingID && renameInput) renameInput.focus();
  });

  const statusColour: Record<string, string> = {
    idle: '#4ade80',
    working: '#facc15',
    parked: '#94a3b8',
    offline: '#475569',
    onboarded: '#818cf8',
    rejected: '#f87171',
  };

  async function handleCreateProject() {
    if (!newName.trim()) return;
    try {
      const project = await createProject(newName.trim(), newDesc.trim());
      showNewProject = false;
      newName = '';
      newDesc = '';
      if (project) {
        // Backend already switched to the new project; sync frontend state.
        await handleSwitch(project.id);
      }
      // Refresh sidebar list (handleSwitch doesn't update wr.projects).
      wr.projects = (await getProjects()) ?? [];
    } catch (e) {
      console.error('createProject:', e);
    }
  }

  async function handleSwitch(id: string) {
    // If the user is already viewing this channel's chat, do nothing.
    // But if they're in a DM (or other view), clicking the active channel
    // should still switch back to its chat view.
    if (id === wr.activeProjectID && wr.activeView === 'chat') return;

    if (id !== wr.activeProjectID) {
      await switchProject(id);
      wr.activeProjectID = id;
    }
    wr.activeView = 'chat';
    wr.messages = [];
    try {
      wr.activeConvID = (await getActiveConversation()) ?? '';
      if (wr.activeConvID) {
        wr.messages = (await getMessages(wr.activeConvID, 200)) ?? [];
      }
    } catch (e) {
      console.error('handleSwitch load messages:', e);
    }
  }

  function startRename(p: { id: string; name: string; description: string }) {
    menuOpenID = '';
    renamingID = p.id;
    renameNameVal = p.name;
    renameDescVal = p.description ?? '';
  }

  async function commitRename() {
    if (!renameNameVal.trim()) return;
    try {
      await renameChannel(renamingID, renameNameVal.trim(), renameDescVal.trim());
    } catch (e) {
      console.error('renameChannel:', e);
    } finally {
      renamingID = '';
    }
  }

  async function handleArchive(id: string) {
    menuOpenID = '';
    try {
      await archiveChannel(id);
      confirmArchiveID = '';
      // Eagerly refresh project list (event from backend also triggers this).
      wr.projects = (await getProjects()) ?? [];
      const active = wr.projects.find((p) => p.active);
      if (active && active.id !== wr.activeProjectID) {
        wr.activeProjectID = active.id;
      } else if (!active) {
        wr.activeProjectID = '';
      }
    } catch (e) {
      console.error('archiveChannel:', e);
      confirmArchiveID = '';
    }
  }

  function initials(name: string) {
    return name.split(' ').map(w => w[0]).join('').slice(0, 2).toUpperCase();
  }
</script>

<aside class="sidebar">
  <!-- Scrollable body: channels + agents -->
  <div class="sidebar-scroll">
    <!-- Channels (projects) -->
    <nav class="nav-section">
      <div class="nav-label">Channels</div>

      {#each wr.projects as p (p.id)}
        {#if renamingID === p.id}
          <!-- Inline rename form -->
          <div class="new-project-form">
            <input
              class="form-input"
              placeholder="Channel name"
              bind:value={renameNameVal}
              bind:this={renameInput}
              onkeydown={(e) => { if (e.key === 'Enter') commitRename(); if (e.key === 'Escape') renamingID = ''; }}
            />
            <input
              class="form-input"
              placeholder="Description (optional)"
              bind:value={renameDescVal}
              onkeydown={(e) => { if (e.key === 'Enter') commitRename(); if (e.key === 'Escape') renamingID = ''; }}
            />
            <div class="form-actions">
              <button class="btn-sm primary" onclick={commitRename}>Save</button>
              <button class="btn-sm" onclick={() => (renamingID = '')}>Cancel</button>
            </div>
          </div>
        {:else}
          <div class="nav-item-wrap" class:active={wr.activeView !== 'dm' && p.id === wr.activeProjectID}>
            <button
              class="nav-item"
              class:active={wr.activeView !== 'dm' && p.id === wr.activeProjectID}
              onclick={() => handleSwitch(p.id)}
              title={p.description || p.name}
            >
              {#if wr.activeView !== 'dm' && p.id === wr.activeProjectID}
                <span class="active-pip"></span>
              {/if}
              <span class="nav-hash">#</span>
              <span class="nav-item-text">{p.name}</span>
            </button>
            <!-- Channel context menu trigger -->
            <div class="channel-menu-wrap">
              <button
                class="channel-menu-btn"
                title="Channel options"
                onclick={(e) => { e.stopPropagation(); menuOpenID = menuOpenID === p.id ? '' : p.id; }}
                aria-label="Channel options"
              >⋯</button>
              {#if menuOpenID === p.id}
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div
                  class="channel-dropdown"
                  onmouseleave={() => { menuOpenID = ''; confirmArchiveID = ''; }}
                >
                  {#if confirmArchiveID === p.id}
                    <div class="confirm-archive">
                      <span class="confirm-label">Hide this channel?</span>
                      <div class="confirm-actions">
                        <button class="dropdown-item danger confirm-yes" onclick={() => handleArchive(p.id)}>Archive</button>
                        <button class="dropdown-item confirm-no" onclick={() => confirmArchiveID = ''}>Cancel</button>
                      </div>
                    </div>
                  {:else}
                    <button class="dropdown-item" onclick={() => startRename(p)}>Rename</button>
                    <button class="dropdown-item danger" onclick={() => confirmArchiveID = p.id}>Archive</button>
                  {/if}
                </div>
              {/if}
            </div>
          </div>
        {/if}
      {/each}

      {#if showNewProject}
        <div class="new-project-form">
          <input
            class="form-input"
            placeholder="Channel name"
            bind:value={newName}
            bind:this={nameInput}
            onkeydown={(e) => { if (e.key === 'Enter') handleCreateProject(); if (e.key === 'Escape') showNewProject = false; }}
          />
          <input
            class="form-input"
            placeholder="Description (optional)"
            bind:value={newDesc}
            onkeydown={(e) => { if (e.key === 'Enter') handleCreateProject(); if (e.key === 'Escape') showNewProject = false; }}
          />
          <div class="form-actions">
            <button class="btn-sm primary" onclick={handleCreateProject}>Create</button>
            <button class="btn-sm" onclick={() => (showNewProject = false)}>Cancel</button>
          </div>
        </div>
      {:else}
        <button class="nav-item add-item" onclick={() => (showNewProject = true)}>
          <span class="add-icon">+</span>
          <span class="nav-item-text">Add channel</span>
        </button>
      {/if}
    </nav>

    <!-- Agents -->
    <nav class="nav-section">
      <div class="nav-label">
        Agents
        {#if wr.approvals.length > 0}
          <span class="approval-badge">{wr.approvals.length}</span>
        {/if}
      </div>
      {#each wr.agents as agent (agent.id)}
        <button
          class="agent-item"
          class:dm-active={wr.activeView === 'dm' && wr.activeDMAgentID === agent.id}
          onclick={() => openDM(agent.id)}
          title="Open direct message with {agent.name}"
        >
          <div class="agent-avatar">
            {initials(agent.name)}
            <span
              class="agent-status-dot"
              style="background:{statusColour[agent.status] ?? '#475569'}"
              title={agent.status}
            ></span>
          </div>
          <div class="agent-text">
            <div class="agent-name">{agent.name}</div>
            <div class="agent-model">{agent.model || agent.role}</div>
          </div>
        </button>
      {/each}
      {#if wr.agents.length === 0}
        <div class="nav-empty">No agents active</div>
      {/if}
    </nav>

    <!-- Approvals -->
    {#if wr.approvals.length > 0}
      <nav class="nav-section">
        <div class="nav-label">Approvals</div>
        {#each wr.approvals as approval (approval.id)}
          <ApprovalCard {approval} />
        {/each}
      </nav>
    {/if}
  </div>
</aside>

<style>
  .sidebar {
    width: 232px;
    flex-shrink: 0;
    background: var(--bg-sidebar);
    border-right: 1px solid var(--border-subtle);
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    /* Clear macOS traffic lights */
    padding-top: var(--titlebar-h);
  }

  /* Scrollable content area */
  .sidebar-scroll {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
    min-height: 0;
  }
  .sidebar-scroll::-webkit-scrollbar { width: 3px; }
  .sidebar-scroll::-webkit-scrollbar-track { background: transparent; }
  .sidebar-scroll::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 4px; }

  /* Nav sections */
  .nav-section {
    padding: 0.875rem 0 0.375rem;
    display: flex;
    flex-direction: column;
  }
  .nav-label {
    font-size: 0.6875rem;
    font-weight: 700;
    letter-spacing: 0.09em;
    color: var(--nav-label-color);
    padding: 0 1rem 0.375rem;
    text-transform: uppercase;
  }

  /* Wrapper for channel row + kebab */
  .nav-item-wrap {
    display: flex;
    align-items: center;
    position: relative;
  }
  .nav-item-wrap:hover { background: var(--bg-hover); }
  .nav-item-wrap:hover .channel-menu-btn { opacity: 1; }
  .nav-item-wrap.active { background: var(--bg-active); }

  .nav-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.35rem 0.5rem 0.35rem 1rem;
    border: none;
    background: none;
    cursor: pointer;
    flex: 1;
    text-align: left;
    color: var(--nav-item-color);
    font-size: 0.9375rem;
    transition: color 0.1s;
    position: relative;
    min-height: 32px;
  }
  .nav-item:hover { color: var(--nav-item-hover); }
  .nav-item.active { color: var(--nav-active-color); font-weight: 500; }
  .nav-hash { color: var(--channel-hash); font-size: 0.9375rem; flex-shrink: 0; line-height: 1; }
  .nav-item.active .nav-hash { color: var(--active-hash); }
  .nav-item-text { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .active-pip {
    width: 2px;
    height: 14px;
    background: var(--accent);
    border-radius: 2px;
    position: absolute;
    left: 0;
    top: 50%;
    transform: translateY(-50%);
  }
  .add-item { color: var(--text-muted); }
  .add-item:hover { color: var(--nav-item-color); }
  .add-icon { font-size: 1rem; line-height: 1; flex-shrink: 0; }
  .nav-empty { font-size: 0.8125rem; color: var(--text-muted); padding: 0.375rem 1rem; }

  /* Channel context menu */
  .channel-menu-wrap {
    position: relative;
    flex-shrink: 0;
    padding-right: 0.375rem;
  }
  .channel-menu-btn {
    opacity: 0;
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-muted);
    font-size: 1rem;
    line-height: 1;
    padding: 2px 6px;
    border-radius: 0;
    transition: opacity 0.1s, color 0.1s;
  }
  .channel-menu-btn:hover { color: var(--text-primary); opacity: 1; }
  .channel-dropdown {
    position: absolute;
    right: 0;
    top: 100%;
    z-index: 100;
    background: var(--bg-surface);
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    box-shadow: 0 4px 16px rgba(0,0,0,0.18);
    min-width: 130px;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .dropdown-item {
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-primary);
    font-size: 0.8125rem;
    padding: 0.5rem 0.875rem;
    text-align: left;
    transition: background 0.1s;
  }
  .dropdown-item:hover { background: var(--bg-hover); }
  .dropdown-item.danger { color: #f87171; }
  .dropdown-item.danger:hover { background: rgba(248,113,113,0.1); }

  /* Inline archive confirmation */
  .confirm-archive {
    padding: 0.35rem 0.5rem 0.25rem;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }
  .confirm-label {
    font-size: 0.75rem;
    color: var(--text-secondary);
    padding: 0 0.25rem 0.1rem;
  }
  .confirm-actions {
    display: flex;
    gap: 0.25rem;
  }
  .confirm-yes, .confirm-no {
    flex: 1;
    text-align: center;
    font-size: 0.75rem;
    padding: 0.2rem 0.35rem;
  }

  /* New project form */
  .new-project-form {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
    padding: 0.375rem 0.75rem;
  }
  .form-input {
    background: var(--bg-surface);
    border: 1px solid var(--border-input);
    border-radius: 7px;
    color: var(--text-primary);
    font-size: 0.8125rem;
    padding: 0.35rem 0.625rem;
    outline: none;
    width: 100%;
    transition: border-color 0.15s;
    font-family: inherit;
  }
  .form-input:focus { border-color: var(--accent); }
  .form-actions { display: flex; gap: 0.375rem; }
  .btn-sm {
    flex: 1;
    padding: 0.35rem 0;
    font-size: 0.8125rem;
    border-radius: 7px;
    border: 1px solid var(--border-input);
    background: var(--bg-surface);
    color: var(--text-secondary);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background 0.12s, color 0.12s;
  }
  .btn-sm:hover { background: var(--bg-hover); color: var(--text-heading); }
  .btn-sm.primary { background: var(--accent-btn); color: #e0e9ff; border-color: var(--accent-btn-hover); }
  .btn-sm.primary:hover { background: var(--accent-btn-hover); }

  /* Agent items */
  .agent-item {
    display: flex;
    align-items: center;
    gap: 0.625rem;
    padding: 0.35rem 1rem;
    min-height: 36px;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    cursor: pointer;
    color: inherit;
    transition: background 0.1s;
    border-radius: 0;
  }
  .agent-item:hover { background: var(--bg-hover); }
  .agent-item.dm-active { background: var(--bg-active); }
  .agent-avatar {
    width: 26px;
    height: 26px;
    border-radius: 7px;
    background: var(--agent-avatar-bg);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.625rem;
    font-weight: 700;
    color: var(--agent-avatar-text);
    flex-shrink: 0;
    position: relative;
  }
  .approval-badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: #f87171;
    color: #fff;
    font-size: 0.625rem;
    font-weight: 700;
    border-radius: 99px;
    min-width: 16px;
    height: 16px;
    padding: 0 4px;
    margin-left: 4px;
    vertical-align: middle;
  }
  .agent-status-dot {
    position: absolute;
    bottom: -2px;
    right: -2px;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    border: 2px solid var(--bg-sidebar);
  }
  .agent-text { flex: 1; overflow: hidden; }
  .agent-name { font-size: 0.875rem; color: var(--agent-name-color); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .agent-model { font-size: 0.75rem; color: var(--agent-model-color); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
</style>
