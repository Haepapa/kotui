<script lang="ts">
  import type { KotuiMessage, ViewMode, HeartbeatState } from '../lib/types';
  import { sendBossCommand } from '../lib/warroom';
  import { wr } from '../stores/warroom.svelte';

  interface Props {
    messages: KotuiMessage[];
    mode: ViewMode;
    isBusy: boolean;
    heartbeat: HeartbeatState;
  }

  let { messages, mode, isBusy, heartbeat }: Props = $props();

  let input = $state('');
  let sendError = $state('');
  let scrollEl = $state<HTMLDivElement | null>(null);
  let inputEl = $state<HTMLTextAreaElement | null>(null);

  $effect(() => {
    // Auto-scroll to bottom when messages change
    if (messages.length && scrollEl) {
      scrollEl.scrollTop = scrollEl.scrollHeight;
    }
  });

  async function send() {
    const cmd = input.trim();
    if (!cmd || isBusy) return;
    if (!wr.activeProjectID) {
      sendError = 'Select or create a channel first.';
      return;
    }
    sendError = '';
    input = '';
    try {
      await sendBossCommand(cmd);
    } catch (e: unknown) {
      sendError = e instanceof Error ? e.message : String(e);
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  }

  // Auto-grow textarea
  function onInput(e: Event) {
    const el = e.currentTarget as HTMLTextAreaElement;
    el.style.height = 'auto';
    el.style.height = Math.min(el.scrollHeight, 160) + 'px';
  }

  function formatTime(iso: string): string {
    if (!iso) return '';
    try { return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }); }
    catch { return ''; }
  }

  function isUserMessage(msg: KotuiMessage) {
    return msg.kind === 'boss_command';
  }

  function senderName(msg: KotuiMessage): string {
    if (msg.kind === 'boss_command') return 'You';
    if (msg.agent_id === 'lead') return 'Lead';
    if (msg.agent_id) return msg.agent_id;
    return 'System';
  }

  function avatarInitials(name: string): string {
    return name.split(' ').map(w => w[0]).join('').slice(0, 2).toUpperCase();
  }

  // Pulse breadcrumb for status bar
  const statusLabel = $derived(
    isBusy ? heartbeat.breadcrumbs.at(-1) ?? 'Working…' : 'Online'
  );
</script>

<div class="chat-area">
  <!-- Message list -->
  <div class="messages" bind:this={scrollEl}>
    {#if messages.length === 0}
      <div class="empty">
        <div class="empty-icon">
          <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.25" opacity="0.3"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
        </div>
        <p class="empty-title">No messages yet</p>
        <p class="empty-sub">Type a message below to get started.</p>
      </div>
    {/if}

    {#each messages as msg (msg.id || msg.created_at)}
      {#if isUserMessage(msg)}
        <!-- User bubble — right aligned -->
        <div class="row row-user">
          <div class="bubble bubble-user">
            <p class="bubble-text">{msg.content}</p>
            <span class="bubble-time">{formatTime(msg.created_at)}</span>
          </div>
        </div>
      {:else if msg.kind === 'milestone'}
        <!-- Milestone — centred pill -->
        <div class="milestone">
          <span class="milestone-text">{msg.content}</span>
        </div>
      {:else if msg.kind === 'system_event'}
        <!-- System event — subtle centred line -->
        <div class="system-event">
          <span class="system-event-text">{msg.content}</span>
        </div>
      {:else}
        <!-- Agent bubble — left aligned -->
        <div class="row row-agent">
          <div class="avatar" title={senderName(msg)}>
            {avatarInitials(senderName(msg))}
          </div>
          <div class="agent-bubble-wrap">
            <div class="bubble-meta">
              <span class="bubble-sender">{senderName(msg)}</span>
              {#if mode === 'dev' && msg.kind !== 'agent_message'}
                <span class="kind-chip">{msg.kind}</span>
              {/if}
              <span class="bubble-time">{formatTime(msg.created_at)}</span>
            </div>
            <div class="bubble bubble-agent" class:tool={msg.kind === 'tool_call' || msg.kind === 'tool_result'}>
              <p class="bubble-text">{msg.content}</p>
            </div>
          </div>
        </div>
      {/if}
    {/each}

    {#if isBusy}
      <div class="row row-agent">
        <div class="avatar">L</div>
        <div class="agent-bubble-wrap">
          <div class="bubble-meta"><span class="bubble-sender">Lead</span></div>
          <div class="bubble bubble-agent typing">
            <span class="dot"></span><span class="dot"></span><span class="dot"></span>
          </div>
        </div>
      </div>
    {/if}
  </div>

  <!-- Status bar -->
  <div class="status-bar">
    <span class="status-dot" class:busy={isBusy}></span>
    <span class="status-label">{statusLabel}</span>
  </div>

  <!-- Input -->
  <div class="composer">
    {#if sendError}
      <div class="send-error">{sendError}</div>
    {/if}
    <div class="composer-box">
      <textarea
        class="composer-input"
        placeholder={wr.activeProjectID ? 'Message the Lead…' : 'Select a channel first…'}
        disabled={isBusy || !wr.activeProjectID}
        bind:value={input}
        bind:this={inputEl}
        onkeydown={onKeydown}
        oninput={onInput}
        rows={1}
      ></textarea>
      <button
        class="send-btn"
        disabled={isBusy || !input.trim()}
        onclick={send}
        title="Send (Enter)"
        aria-label="Send"
      >
        <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z"/></svg>
      </button>
    </div>
    <p class="composer-hint">Enter to send · Shift+Enter for new line</p>
  </div>
</div>

<style>
  .chat-area {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-width: 0;
    min-height: 0;
    height: 100%;
  }

  /* Messages */
  .messages {
    flex: 1;
    overflow-y: auto;
    padding: 1.5rem 1.5rem 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    scroll-behavior: smooth;
    min-height: 0;
  }
  .messages::-webkit-scrollbar { width: 4px; }
  .messages::-webkit-scrollbar-track { background: transparent; }
  .messages::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 4px; }

  /* Empty state */
  .empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    color: var(--text-muted);
    padding: 4rem 0;
    text-align: center;
  }
  .empty-icon { margin-bottom: 0.25rem; }
  .empty-title { font-size: 0.9375rem; font-weight: 500; color: var(--text-secondary); }
  .empty-sub { font-size: 0.8125rem; }

  /* Message rows */
  .row {
    display: flex;
    gap: 0.625rem;
    align-items: flex-end;
    max-width: 75%;
  }
  .row-user {
    align-self: flex-end;
    flex-direction: row-reverse;
  }
  .row-agent { align-self: flex-start; }

  /* Bubbles */
  .bubble {
    padding: 0.5rem 0.875rem;
    border-radius: 14px;
    max-width: 100%;
    word-break: break-word;
  }
  .bubble-user {
    background: var(--bubble-user-bg);
    border-bottom-right-radius: 4px;
    color: var(--bubble-user-text);
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 0.2rem;
  }
  .bubble-agent {
    background: var(--bubble-agent-bg);
    border-bottom-left-radius: 4px;
    color: var(--bubble-agent-text);
    border: 1px solid var(--bubble-agent-border);
  }
  .bubble-agent.tool {
    background: var(--bubble-tool-bg);
    border-color: var(--bubble-tool-border);
  }
  .bubble-text {
    font-size: 1rem;
    line-height: 1.55;
    white-space: pre-wrap;
    margin: 0;
  }
  .bubble-time {
    font-size: 0.75rem;
    opacity: 0.55;
    white-space: nowrap;
    flex-shrink: 0;
  }

  /* Agent avatar */
  .avatar {
    width: 30px;
    height: 30px;
    border-radius: 8px;
    background: linear-gradient(145deg, #2d55c8, #5b4de8);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.6875rem;
    font-weight: 700;
    color: #fff;
    flex-shrink: 0;
  }

  .agent-bubble-wrap {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    max-width: 100%;
  }
  .bubble-meta {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    padding-left: 0.125rem;
  }
  .bubble-sender {
    font-size: 0.8125rem;
    font-weight: 600;
    color: var(--text-secondary);
  }
  .kind-chip {
    font-size: 0.6875rem;
    background: var(--bg-surface);
    color: var(--text-muted);
    border-radius: 3px;
    padding: 0.1rem 0.35rem;
    letter-spacing: 0.04em;
  }

  /* Milestone / system */
  .milestone {
    align-self: center;
    margin: 0.5rem 0;
  }
  .milestone-text {
    font-size: 0.875rem;
    color: var(--milestone-color);
    background: var(--milestone-bg);
    border: 1px solid var(--milestone-border);
    border-radius: 99px;
    padding: 0.25rem 0.875rem;
  }
  .system-event {
    align-self: center;
    margin: 0.25rem 0;
  }
  .system-event-text {
    font-size: 0.8125rem;
    color: var(--text-muted);
  }

  /* Typing indicator */
  .typing {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 0.625rem 0.875rem;
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--typing-dot-color);
    animation: bounce 1.2s ease-in-out infinite;
  }
  .dot:nth-child(2) { animation-delay: 0.2s; }
  .dot:nth-child(3) { animation-delay: 0.4s; }
  @keyframes bounce {
    0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
    40% { transform: translateY(-5px); opacity: 1; }
  }

  /* Status bar */
  .status-bar {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.25rem 1.5rem;
    flex-shrink: 0;
  }
  .status-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--status-online);
    flex-shrink: 0;
    transition: background 0.3s;
  }
  .status-dot.busy {
    background: var(--status-busy);
    animation: pulse-dot 1s ease-in-out infinite;
  }
  @keyframes pulse-dot {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
  }
  .status-label {
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  /* Composer — fixed at bottom, never pushes messages */
  .composer {
    padding: 0 1.25rem 1.25rem;
    flex-shrink: 0;
  }
  .send-error {
    font-size: 0.75rem;
    color: #f87171;
    padding: 0.25rem 0.125rem 0.375rem;
  }
  .composer-box {
    display: flex;
    align-items: flex-end;
    gap: 0.5rem;
    background: var(--bg-surface);
    border: 1px solid var(--border-input);
    border-radius: 12px;
    padding: 0.5rem 0.5rem 0.5rem 0.875rem;
    transition: border-color 0.15s;
  }
  .composer-box:focus-within { border-color: rgba(79,124,247,0.5); }
  .composer-input {
    flex: 1;
    background: none;
    border: none;
    color: var(--text-heading);
    font-size: 0.9375rem;
    line-height: 1.5;
    outline: none;
    resize: none;
    min-height: 24px;
    max-height: 160px;
    font-family: inherit;
  }
  .composer-input::placeholder { color: var(--placeholder-color); }
  .composer-input:disabled { opacity: 0.45; }
  .send-btn {
    width: 34px;
    height: 34px;
    border-radius: 9px;
    border: none;
    background: var(--accent-btn);
    color: #fff;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    transition: background 0.15s, opacity 0.15s;
  }
  .send-btn:hover:not(:disabled) { background: var(--accent-btn-hover); }
  .send-btn:disabled { opacity: 0.25; cursor: default; }
  .composer-hint {
    font-size: 0.75rem;
    color: var(--composer-hint);
    text-align: right;
    margin-top: 0.3rem;
  }
</style>
