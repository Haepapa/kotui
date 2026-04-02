<script lang="ts">
  import type { KotuiMessage, ViewMode } from '../lib/types';

  interface Props {
    messages: KotuiMessage[];
    mode: ViewMode;
  }

  let { messages, mode }: Props = $props();

  const kindIcon: Record<string, string> = {
    boss_command: '💬',
    agent_message: '🤖',
    tool_call: '🔧',
    tool_result: '✅',
    milestone: '🏁',
    system_event: 'ℹ️',
    draft: '📝',
  };

  const agentColour: Record<string, string> = {
    lead: '#7dd3fc',
    '': '#94a3b8',
  };

  function agentLabel(msg: KotuiMessage): string {
    if (msg.agent_id === 'boss' || msg.kind === 'boss_command') return 'You';
    if (msg.agent_id === 'lead') return 'Lead';
    if (msg.agent_id) return msg.agent_id;
    return 'System';
  }

  function labelColour(msg: KotuiMessage): string {
    if (msg.kind === 'boss_command') return '#fde68a';
    if (msg.agent_id === 'lead') return '#7dd3fc';
    return '#94a3b8';
  }

  function formatTime(iso: string): string {
    try {
      return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    } catch {
      return '';
    }
  }

  // Auto-scroll when new messages arrive.
  let chatEl = $state<HTMLDivElement | null>(null);
  $effect(() => {
    if (messages.length && chatEl) {
      chatEl.scrollTop = chatEl.scrollHeight;
    }
  });
</script>

<div class="chat" bind:this={chatEl}>
  {#each messages as msg (msg.id || msg.created_at)}
    <div
      class="message"
      class:milestone={msg.kind === 'milestone'}
      class:boss={msg.kind === 'boss_command'}
      class:tool={msg.kind === 'tool_call' || msg.kind === 'tool_result'}
    >
      <div class="msg-meta">
        <span class="msg-icon">{kindIcon[msg.kind] ?? '•'}</span>
        <span class="msg-agent" style="color:{labelColour(msg)}">{agentLabel(msg)}</span>
        <span class="msg-time">{formatTime(msg.created_at)}</span>
        {#if mode === 'dev' && msg.tier === 'raw'}
          <span class="tier-badge">RAW</span>
        {/if}
      </div>
      <div class="msg-content">{msg.content}</div>
    </div>
  {/each}

  {#if messages.length === 0}
    <div class="empty-state">
      <div class="empty-icon">⚔️</div>
      <div class="empty-title">War Room ready</div>
      <div class="empty-sub">Select a project and give the Lead a command.</div>
    </div>
  {/if}
</div>

<style>
  .chat {
    flex: 1;
    overflow-y: auto;
    padding: 1rem 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    scroll-behavior: smooth;
  }
  .message {
    background: #0f1726;
    border: 1px solid #1e293b;
    border-radius: 8px;
    padding: 0.625rem 0.875rem;
    transition: border-color 0.15s;
  }
  .message.milestone {
    border-color: #1d4ed8;
    background: #0c1a3a;
  }
  .message.boss {
    border-color: #92400e;
    background: #1c0f02;
  }
  .message.tool {
    border-color: #064e3b;
    background: #071a12;
  }
  .msg-meta {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-bottom: 0.25rem;
  }
  .msg-icon { font-size: 0.875rem; }
  .msg-agent {
    font-size: 0.75rem;
    font-weight: 600;
    letter-spacing: 0.02em;
  }
  .msg-time {
    font-size: 0.6875rem;
    color: #334155;
    margin-left: auto;
  }
  .tier-badge {
    font-size: 0.625rem;
    background: #1e293b;
    color: #64748b;
    border-radius: 3px;
    padding: 0.1rem 0.3rem;
    letter-spacing: 0.05em;
  }
  .msg-content {
    font-size: 0.875rem;
    color: #cbd5e1;
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .empty-state {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    color: #334155;
  }
  .empty-icon { font-size: 2.5rem; }
  .empty-title {
    font-size: 1rem;
    color: #475569;
    font-weight: 600;
  }
  .empty-sub { font-size: 0.8125rem; }
</style>
