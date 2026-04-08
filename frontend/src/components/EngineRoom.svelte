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

  function isThinking(msg: KotuiMessage): boolean {
    return msg.kind === 'system_event' && msg.content.startsWith('💭 thinking:');
  }

  function isDraft(msg: KotuiMessage): boolean {
    return msg.kind === 'draft';
  }

  function thinkingBody(content: string): string {
    const nl = content.indexOf('\n');
    return nl >= 0 ? content.slice(nl + 1).trim() : content;
  }

  function draftPreview(content: string): string {
    // First non-empty line, truncated to 60 chars
    const first = content.split('\n').find(l => l.trim()) ?? '';
    return first.length > 60 ? first.slice(0, 60) + '…' : first;
  }
</script>

<aside class="engine-room">
  <div class="er-header">
    <span class="er-title">Agent Activity</span>
    <span class="er-count">{messages.length}</span>
  </div>
  <div class="er-console" bind:this={consoleEl}>
    {#each messages as msg (msg.id || msg.created_at)}
      <div class="log-line">
        <div class="log-meta">
          <span class="log-time">{formatTime(msg.created_at)}</span>
          <span class="log-kind" style="color:{kindColour(msg.kind)}">[{msg.kind}]</span>
          {#if msg.agent_id}
            <span class="log-agent">{msg.agent_id}</span>
          {/if}
        </div>
        {#if isThinking(msg)}
          <details class="think-details">
            <summary class="think-summary-er">💭 thinking…</summary>
            <div class="log-content think-content">{thinkingBody(msg.content)}</div>
          </details>
        {:else if isDraft(msg)}
          <details class="draft-details">
            <summary class="draft-summary-er">📝 draft — {draftPreview(msg.content)}</summary>
            <div class="log-content draft-content">{msg.content}</div>
          </details>
        {:else}
          <div class="log-content">{msg.content}</div>
        {/if}
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
    background: var(--bg-console);
    border-left: 1px solid var(--border-console);
    display: flex;
    flex-direction: column;
    font-family: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace;
  }
  .er-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.5rem 0.75rem;
    border-bottom: 1px solid var(--border-console);
    flex-shrink: 0;
  }
  .er-title { font-size: 0.8125rem; color: var(--text-secondary); font-weight: 600; letter-spacing: 0.04em; }
  .er-count {
    font-size: 0.75rem;
    background: var(--border-console);
    color: var(--text-secondary);
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
  .er-console::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 4px; }
  .log-line {
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
    font-size: 0.75rem;
    line-height: 1.5;
    padding: 0.2rem 0;
    border-bottom: 1px solid var(--border-console);
  }
  .log-meta {
    display: flex;
    gap: 0.375rem;
    align-items: baseline;
  }
  .log-time { color: var(--text-muted); white-space: nowrap; }
  .log-kind { font-weight: 600; white-space: nowrap; }
  .log-agent { color: #60a5fa; white-space: nowrap; }
  .log-content { color: var(--text-secondary); word-break: break-word; padding-left: 0.25rem; }
  .er-empty { font-size: 0.75rem; color: var(--text-muted); padding: 1rem; text-align: center; }

  /* Collapsible thinking entries */
  .think-details {
    padding-left: 0.25rem;
  }
  .think-summary-er {
    cursor: pointer;
    color: #fb923c;
    font-size: 0.7rem;
    user-select: none;
    list-style: none;
    display: flex;
    align-items: center;
    gap: 0.25rem;
  }
  .think-summary-er::before {
    content: '▶';
    font-size: 0.55rem;
    transition: transform 0.15s;
    display: inline-block;
  }
  .think-details[open] .think-summary-er::before {
    transform: rotate(90deg);
  }
  .think-summary-er::-webkit-details-marker { display: none; }
  .think-content {
    margin-top: 0.25rem;
    white-space: pre-wrap;
    border-left: 2px solid rgba(251,146,60,0.3);
    padding-left: 0.375rem;
    color: rgba(251,146,60,0.75);
  }

  /* Collapsible draft entries */
  .draft-details {
    padding-left: 0.25rem;
  }
  .draft-summary-er {
    cursor: pointer;
    color: #94a3b8;
    font-size: 0.7rem;
    user-select: none;
    list-style: none;
    display: flex;
    align-items: center;
    gap: 0.25rem;
  }
  .draft-summary-er::before {
    content: '▶';
    font-size: 0.55rem;
    transition: transform 0.15s;
    display: inline-block;
  }
  .draft-details[open] .draft-summary-er::before {
    transform: rotate(90deg);
  }
  .draft-summary-er::-webkit-details-marker { display: none; }
  .draft-content {
    margin-top: 0.25rem;
    white-space: pre-wrap;
    border-left: 2px solid rgba(148,163,184,0.3);
    padding-left: 0.375rem;
    color: rgba(148,163,184,0.8);
  }
</style>

