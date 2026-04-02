<script lang="ts">
  import { createProject, switchProject } from '../lib/warroom';
  import { wr } from '../stores/warroom.svelte';

  let showNewProject = $state(false);
  let newName = $state('');
  let newDesc = $state('');

  const roleIcon: Record<string, string> = {
    lead: '👑',
    specialist: '⚙️',
    trial: '🔍',
  };

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
</script>

<aside class="sidebar">
  <!-- Project selector -->
  <div class="section-label">PROJECT</div>
  <div class="project-list">
    {#each wr.projects as p (p.id)}
      <button
        class="project-item"
        class:active={p.id === wr.activeProjectID}
        onclick={() => handleSwitch(p.id)}
      >
        <span class="project-dot" style="background:{p.id === wr.activeProjectID ? '#7dd3fc' : '#475569'}"></span>
        <span class="project-name">{p.name}</span>
      </button>
    {/each}

    {#if showNewProject}
      <div class="new-project-form">
        <input
          class="input-sm"
          placeholder="Project name"
          bind:value={newName}
          onkeydown={(e) => e.key === 'Enter' && handleCreateProject()}
        />
        <input
          class="input-sm"
          placeholder="Description (optional)"
          bind:value={newDesc}
          onkeydown={(e) => e.key === 'Enter' && handleCreateProject()}
        />
        <div class="row">
          <button class="btn-xs primary" onclick={handleCreateProject}>Create</button>
          <button class="btn-xs" onclick={() => (showNewProject = false)}>Cancel</button>
        </div>
      </div>
    {:else}
      <button class="new-project-btn" onclick={() => (showNewProject = true)}>+ New Project</button>
    {/if}
  </div>

  <div class="divider"></div>

  <!-- Agent roster -->
  <div class="section-label">AGENTS</div>
  <div class="agent-list">
    {#each wr.agents as agent (agent.id)}
      <div class="agent-item">
        <div class="agent-avatar" title={agent.role}>
          {roleIcon[agent.role] ?? '🤖'}
        </div>
        <div class="agent-info">
          <div class="agent-name">{agent.name}</div>
          <div class="agent-model">{agent.model || 'no model'}</div>
        </div>
        <div
          class="status-dot"
          title={agent.status}
          style="background:{statusColour[agent.status] ?? '#475569'}"
        ></div>
      </div>
    {/each}
    {#if agents.length === 0}
      <div class="empty-agents">No agents spawned</div>
    {/if}
  </div>
</aside>

<style>
  .sidebar {
    width: 220px;
    flex-shrink: 0;
    background: #0b0f1a;
    border-right: 1px solid #1e293b;
    display: flex;
    flex-direction: column;
    overflow-y: auto;
    padding: 0.75rem 0;
  }
  .section-label {
    font-size: 0.625rem;
    letter-spacing: 0.1em;
    color: #475569;
    padding: 0.5rem 1rem 0.25rem;
    font-weight: 600;
  }
  .project-list {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
    padding: 0 0.5rem;
  }
  .project-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.375rem 0.625rem;
    background: none;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    width: 100%;
    text-align: left;
    transition: background 0.15s;
  }
  .project-item:hover { background: #1e293b; }
  .project-item.active { background: #172554; }
  .project-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .project-name {
    font-size: 0.8125rem;
    color: #cbd5e1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .new-project-form {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
    padding: 0.5rem 0.25rem;
  }
  .input-sm {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 5px;
    color: #e2e8f0;
    font-size: 0.75rem;
    padding: 0.3rem 0.5rem;
    outline: none;
  }
  .input-sm:focus { border-color: #38bdf8; }
  .row {
    display: flex;
    gap: 0.375rem;
  }
  .btn-xs {
    flex: 1;
    padding: 0.25rem;
    font-size: 0.7rem;
    border-radius: 4px;
    border: 1px solid #334155;
    background: #1e293b;
    color: #94a3b8;
    cursor: pointer;
  }
  .btn-xs.primary { background: #1e40af; color: #bfdbfe; border-color: #1d4ed8; }
  .new-project-btn {
    background: none;
    border: 1px dashed #1e293b;
    border-radius: 6px;
    color: #475569;
    cursor: pointer;
    font-size: 0.75rem;
    padding: 0.375rem 0.625rem;
    text-align: left;
    transition: all 0.15s;
    margin-top: 0.25rem;
  }
  .new-project-btn:hover { border-color: #334155; color: #94a3b8; }
  .divider { height: 1px; background: #1e293b; margin: 0.75rem 0; }
  .agent-list {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    padding: 0 0.5rem;
  }
  .agent-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.375rem 0.5rem;
    border-radius: 6px;
  }
  .agent-avatar {
    font-size: 1rem;
    flex-shrink: 0;
  }
  .agent-info {
    flex: 1;
    overflow: hidden;
  }
  .agent-name {
    font-size: 0.8125rem;
    color: #cbd5e1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .agent-model {
    font-size: 0.6875rem;
    color: #475569;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .status-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .empty-agents {
    font-size: 0.75rem;
    color: #334155;
    padding: 0.5rem 0.625rem;
  }
</style>
