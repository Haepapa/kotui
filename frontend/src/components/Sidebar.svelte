<script lang="ts">
  import { createProject, switchProject, decideApproval } from '../lib/warroom';
  import { wr, openDM, refreshApprovals } from '../stores/warroom.svelte';
  import ApprovalCard from './ApprovalCard.svelte';

  let showNewProject = $state(false);
  let newName = $state('');
  let newDesc = $state('');
  let nameInput = $state<HTMLInputElement | null>(null);

  $effect(() => {
    if (showNewProject && nameInput) nameInput.focus();
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
      await createProject(newName.trim(), newDesc.trim());
      showNewProject = false;
      newName = '';
      newDesc = '';
    } catch (e) {
      console.error('createProject:', e);
    }
  }

  async function handleSwitch(id: string) {
    if (id === wr.activeProjectID) return;
    await switchProject(id);
    wr.messages.length = 0;
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
        <button
          class="nav-item"
          class:active={p.id === wr.activeProjectID}
          onclick={() => handleSwitch(p.id)}
          title={p.description || p.name}
        >
          <span class="nav-hash">#</span>
          <span class="nav-item-text">{p.name}</span>
          {#if p.id === wr.activeProjectID}
            <span class="active-pip"></span>
          {/if}
        </button>
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
        <div class="agent-item">
          <button class="agent-avatar" title="{agent.role}" onclick={() => openDM(agent.id)}>
            {initials(agent.name)}
            <span
              class="agent-status-dot"
              style="background:{statusColour[agent.status] ?? '#475569'}"
              title={agent.status}
            ></span>
          </button>
          <div class="agent-text">
            <div class="agent-name">{agent.name}</div>
            <div class="agent-model">{agent.model || agent.role}</div>
          </div>
        </div>
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
  .nav-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.35rem 1rem;
    border: none;
    background: none;
    cursor: pointer;
    width: 100%;
    text-align: left;
    color: var(--nav-item-color);
    font-size: 0.9375rem;
    transition: background 0.1s, color 0.1s;
    position: relative;
    min-height: 32px;
  }
  .nav-item:hover { background: var(--bg-hover); color: var(--nav-item-hover); }
  .nav-item.active { background: var(--bg-active); color: var(--nav-active-color); font-weight: 500; }
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
  }
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
    border: none;
    cursor: pointer;
    transition: opacity 0.12s;
  }
  .agent-avatar:hover { opacity: 0.8; }
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
