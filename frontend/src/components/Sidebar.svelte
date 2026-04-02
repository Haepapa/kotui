<script lang="ts">
  import { createProject, switchProject } from '../lib/warroom';
  import { wr } from '../stores/warroom.svelte';

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
  <!-- Brand / workspace -->
  <div class="workspace-header">
    <div class="workspace-logo">K</div>
    <span class="workspace-name">Kōtui</span>
  </div>

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
      <div class="nav-label">Agents</div>
      {#each wr.agents as agent (agent.id)}
        <div class="agent-item">
          <div class="agent-avatar" title="{agent.role}">
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
        </div>
      {/each}
      {#if wr.agents.length === 0}
        <div class="nav-empty">No agents active</div>
      {/if}
    </nav>
  </div>
</aside>

<style>
  .sidebar {
    width: 240px;
    flex-shrink: 0;
    background: #16191f;
    border-right: 1px solid #2a2d35;
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
  }

  /* Workspace header — fixed, never scrolls */
  .workspace-header {
    display: flex;
    align-items: center;
    gap: 0.625rem;
    padding: 0.875rem 1rem;
    border-bottom: 1px solid #2a2d35;
    flex-shrink: 0;
  }
  .workspace-logo {
    width: 28px;
    height: 28px;
    border-radius: 6px;
    background: linear-gradient(135deg, #3b82f6, #6366f1);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.875rem;
    font-weight: 700;
    color: #fff;
    flex-shrink: 0;
  }
  .workspace-name {
    font-size: 0.9375rem;
    font-weight: 700;
    color: #e2e8f0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* Scrollable content area */
  .sidebar-scroll {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
    min-height: 0;
  }
  .sidebar-scroll::-webkit-scrollbar { width: 4px; }
  .sidebar-scroll::-webkit-scrollbar-track { background: transparent; }
  .sidebar-scroll::-webkit-scrollbar-thumb { background: #2a2d35; border-radius: 4px; }

  /* Nav sections */
  .nav-section {
    padding: 0.75rem 0 0.5rem;
    display: flex;
    flex-direction: column;
  }  .nav-label {
    font-size: 0.6875rem;
    font-weight: 600;
    letter-spacing: 0.07em;
    color: #64748b;
    padding: 0 1rem 0.375rem;
    text-transform: uppercase;
  }
  .nav-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.375rem 0.75rem 0.375rem 1rem;
    border: none;
    background: none;
    cursor: pointer;
    width: 100%;
    text-align: left;
    border-radius: 0;
    color: #94a3b8;
    font-size: 0.875rem;
    transition: background 0.12s, color 0.12s;
    position: relative;
    min-height: 32px;
  }
  .nav-item:hover { background: #1e2029; color: #cbd5e1; }
  .nav-item.active { background: #1e2a3a; color: #e2e8f0; font-weight: 500; }
  .nav-hash {
    color: #475569;
    font-size: 1rem;
    flex-shrink: 0;
    line-height: 1;
  }
  .nav-item.active .nav-hash { color: #60a5fa; }
  .nav-item-text {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .active-pip {
    width: 3px;
    height: 16px;
    background: #3b82f6;
    border-radius: 2px;
    position: absolute;
    left: 0;
    top: 50%;
    transform: translateY(-50%);
  }
  .add-item { color: #475569; font-style: normal; }
  .add-item:hover { color: #94a3b8; }
  .add-icon {
    font-size: 1.125rem;
    line-height: 1;
    flex-shrink: 0;
    color: inherit;
  }
  .nav-empty {
    font-size: 0.8125rem;
    color: #334155;
    padding: 0.5rem 1rem;
  }

  /* New project form */
  .new-project-form {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
    padding: 0.5rem 0.75rem;
  }
  .form-input {
    background: #1e2029;
    border: 1px solid #2a2d35;
    border-radius: 6px;
    color: #e2e8f0;
    font-size: 0.8125rem;
    padding: 0.375rem 0.625rem;
    outline: none;
    width: 100%;
    transition: border-color 0.15s;
  }
  .form-input:focus { border-color: #3b82f6; }
  .form-actions {
    display: flex;
    gap: 0.375rem;
  }
  .btn-sm {
    flex: 1;
    padding: 0.375rem 0;
    font-size: 0.8125rem;
    border-radius: 6px;
    border: 1px solid #2a2d35;
    background: #1e2029;
    color: #94a3b8;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background 0.12s, color 0.12s;
  }
  .btn-sm:hover { background: #2a2d35; color: #e2e8f0; }
  .btn-sm.primary {
    background: #1d4ed8;
    color: #eff6ff;
    border-color: #2563eb;
  }
  .btn-sm.primary:hover { background: #2563eb; }

  /* Agent items */
  .agent-item {
    display: flex;
    align-items: center;
    gap: 0.625rem;
    padding: 0.375rem 0.75rem 0.375rem 1rem;
    min-height: 36px;
  }
  .agent-avatar {
    width: 24px;
    height: 24px;
    border-radius: 6px;
    background: #2a2d35;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.625rem;
    font-weight: 700;
    color: #94a3b8;
    flex-shrink: 0;
    position: relative;
  }
  .agent-status-dot {
    position: absolute;
    bottom: -2px;
    right: -2px;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    border: 2px solid #16191f;
  }
  .agent-text { flex: 1; overflow: hidden; }
  .agent-name {
    font-size: 0.8125rem;
    color: #94a3b8;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .agent-model {
    font-size: 0.6875rem;
    color: #475569;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>

