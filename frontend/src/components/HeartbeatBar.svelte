<script lang="ts">
  import type { HeartbeatState } from '../lib/types';

  interface Props {
    heartbeat: HeartbeatState;
    isBusy: boolean;
  }

  let { heartbeat, isBusy }: Props = $props();
</script>

<footer class="heartbeat-bar">
  <!-- Pulse indicator -->
  <div class="pulse-wrap" title={heartbeat.is_healthy ? 'Healthy' : 'Degraded'}>
    <div class="pulse-dot" class:healthy={heartbeat.is_healthy} class:busy={isBusy}></div>
  </div>

  <!-- Breadcrumb trail -->
  <div class="breadcrumbs">
    {#each heartbeat.breadcrumbs as crumb, i (i)}
      {#if i > 0}<span class="sep">›</span>{/if}
      <span
        class="crumb"
        class:active={i === heartbeat.breadcrumbs.length - 1}
      >{crumb}</span>
    {/each}
  </div>

  <!-- VRAM badge (when known) -->
  {#if heartbeat.vram_profile}
    <div class="vram-badge" title="VRAM profile">
      {heartbeat.vram_profile === 'dual' ? '⚡ Dual' : '🔄 Swap'}
    </div>
  {/if}
</footer>

<style>
  .heartbeat-bar {
    height: 32px;
    background: #0b0f1a;
    border-top: 1px solid #1e293b;
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0 0.875rem;
    flex-shrink: 0;
  }
  .pulse-wrap {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 14px;
    height: 14px;
  }
  .pulse-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #475569;
    transition: background 0.3s;
  }
  .pulse-dot.healthy { background: #22c55e; }
  .pulse-dot.busy {
    background: #facc15;
    animation: pulse 1s ease-in-out infinite;
  }
  @keyframes pulse {
    0%, 100% { opacity: 1; transform: scale(1); }
    50% { opacity: 0.5; transform: scale(1.3); }
  }
  .breadcrumbs {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    overflow: hidden;
    flex: 1;
  }
  .sep { color: #334155; font-size: 0.6875rem; }
  .crumb {
    font-size: 0.6875rem;
    color: #475569;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .crumb.active { color: #94a3b8; font-weight: 600; }
  .vram-badge {
    font-size: 0.625rem;
    color: #475569;
    background: #0f172a;
    border: 1px solid #1e293b;
    border-radius: 4px;
    padding: 0.1rem 0.4rem;
    white-space: nowrap;
  }
</style>
