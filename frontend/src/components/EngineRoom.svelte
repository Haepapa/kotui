<script lang="ts">
  import type { KotuiMessage } from '../lib/types';

  interface Props {
    messages: KotuiMessage[];
  }

  let { messages }: Props = $props();

  let consoleEl = $state<HTMLDivElement | null>(null);

  $effect(() => {
    if (messages.length && consoleEl) {
      consoleEl.scrollTop = consoleEl.scrollHeight;
    }
  });

  function kindColour(kind: string): string {
    switch (kind) {
      case 'tool_call': return '#4ade80';
      case 'tool_result': return '#a78bfa';
      case 'draft': return '#94a3b8';
      case 'system_event': return '#fb923c';
      default: return '#64748b';
    }
  }

  function formatTime(iso: string): string {
    try {
      return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    } catch { return ''; }
  }
</script>

<aside class="engine-room">
  <div class="er-header">
    <span class="er-title">Dev Console</span>
    <span class="er-count">{messages.length}</span>
  </div>
  <div class="er-console" bind:this={consoleEl}>
    {#each messages as msg (msg.id || msg.created_at)}
      <div class="log-line">
        <span class="log-time">{formatTime(msg.created_at)}</span>
        <span class="log-kind" style="color:{kindColour(msg.kind)}">[{msg.kind}]</span>
        {#if msg.agent_id}
          <span class="log-agent">{msg.agent_id}</span>
        {/if}
        <span class="log-content">{msg.content}</span>
      </div>
    {/each}
    {#if messages.length === 0}
      <div class="er-empty">Raw logs appear here in Dev mode.</div>
    {/if}
  </div>
</aside>

<style>
  .engine-room {
    width: 280px;
    flex-shrink: 0;
    background: #0e1017;
    border-left: 1px solid #1e2029;
    display: flex;
    flex-direction: column;
    font-family: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace;
  }
  .er-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.5rem 0.75rem;
    border-bottom: 1px solid #1e2029;
    flex-shrink: 0;
  }
  .er-title { font-size: 0.75rem; color: #475569; font-weight: 600; letter-spacing: 0.04em; }
  .er-count {
    font-size: 0.6875rem;
    background: #1e2029;
    color: #475569;
    border-radius: 10px;
    padding: 0.1rem 0.4rem;
  }
  .er-console {
    flex: 1;
    overflow-y: auto;
    padding: 0.5rem;
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
  }
  .er-console::-webkit-scrollbar { width: 4px; }
  .er-console::-webkit-scrollbar-thumb { background: #1e2029; border-radius: 4px; }
  .log-line {
    display: flex;
    gap: 0.375rem;
    font-size: 0.6875rem;
    line-height: 1.5;
    align-items: baseline;
    flex-wrap: wrap;
  }
  .log-time { color: #2a2d35; white-space: nowrap; }
  .log-kind { font-weight: 600; white-space: nowrap; }
  .log-agent { color: #60a5fa; white-space: nowrap; }
  .log-content { color: #475569; word-break: break-word; flex: 1; min-width: 0; }
  .er-empty { font-size: 0.6875rem; color: #1e2029; padding: 1rem; text-align: center; }
</style>

