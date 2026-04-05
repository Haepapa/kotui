<script lang="ts">
  import type { KotuiMessage, ViewMode, HeartbeatState } from '../lib/types';
  import { sendBossCommand, sendDirectMessage } from '../lib/warroom';
  import { wr, agentName } from '../stores/warroom.svelte';

  interface Props {
    messages: KotuiMessage[];
    mode: ViewMode;
    isBusy: boolean;
    heartbeat: HeartbeatState;
    isDM?: boolean;
    dmAgentID?: string;
    streamContent?: string; // live-streamed token accumulation for DM
  }

  let { messages, mode, isBusy, heartbeat, isDM = false, dmAgentID = '', streamContent = '' }: Props = $props();

  let input = $state('');
  let sendError = $state('');
  let scrollEl = $state<HTMLDivElement | null>(null);
  let inputEl = $state<HTMLTextAreaElement | null>(null);
  let copiedKey = $state('');
  // Ref to the streaming think-body so we can auto-scroll it to the bottom.
  let streamThinkEl = $state<HTMLDivElement | null>(null);

  $effect(() => {
    // Auto-scroll to bottom when messages change
    if (messages.length && scrollEl) {
      scrollEl.scrollTop = scrollEl.scrollHeight;
    }
  });

  // Also scroll when streaming content grows.
  $effect(() => {
    if (streamContent && scrollEl) {
      scrollEl.scrollTop = scrollEl.scrollHeight;
    }
  });

  // Auto-scroll the streaming think-body to the bottom so the user always
  // sees the latest thinking tokens as they arrive.
  $effect(() => {
    if (streamThinkEl && streamParsed.thinking) {
      streamThinkEl.scrollTop = streamThinkEl.scrollHeight;
    }
  });

  // Parse <think>...</think> blocks from streamed/full content.
  // Mirrors the Go extractThinkBlocks logic: searches for <think> anywhere in
  // the content (not anchored to start) so preamble tokens before the tag don't
  // break detection. Any text before <think> is silently discarded — it is
  // model preamble that should not appear in the response bubble.
  // Returns an `inThink` flag that is true when a <think> block has opened but
  // not yet closed — used to suppress the raw-content fallback bubble while
  // the model is still producing thinking tokens.
  function parseThink(content: string): { thinking: string; response: string; inThink: boolean } {
    const openIdx = content.indexOf('<think>');
    if (openIdx === -1) {
      // No think block present at all — plain response.
      return { thinking: '', response: content, inThink: false };
    }
    const afterOpen = content.slice(openIdx + 7); // skip '<think>'
    const closeIdx = afterOpen.indexOf('</think>');
    if (closeIdx === -1) {
      // Think block opened but not yet closed — still streaming thinking tokens.
      return { thinking: afterOpen.trim(), response: '', inThink: true };
    }
    // Complete think block: extract thinking and the response that follows.
    const thinking = afterOpen.slice(0, closeIdx).trim();
    const response = afterOpen.slice(closeIdx + 8).trimStart(); // skip '</think>'
    return { thinking, response, inThink: false };
  }

  // Extract thinking from a stored message's metadata JSON field.
  // Returns an empty string when no thinking was recorded.
  function metaThinking(metadata: string | undefined): string {
    if (!metadata || metadata === '{}') return '';
    try {
      const m = JSON.parse(metadata);
      return typeof m.thinking === 'string' ? m.thinking : '';
    } catch {
      return '';
    }
  }

  // Derived: split the live stream into thinking vs response parts.
  const streamParsed = $derived(parseThink(streamContent));

  async function send() {
    const cmd = input.trim();
    if (!cmd) return;
    if (!wr.activeProjectID) {
      sendError = 'Select or create a channel first.';
      return;
    }
    sendError = '';
    input = '';
    try {
      if (isDM && dmAgentID) {
        await sendDirectMessage(dmAgentID, cmd);
      } else {
        await sendBossCommand(cmd);
      }
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
    if (msg.agent_id) return agentName(msg.agent_id);
    return 'System';
  }

  function avatarInitials(name: string): string {
    return name.split(' ').map(w => w[0]).join('').slice(0, 2).toUpperCase();
  }

  // Artifact rendering
  const artifactPattern = /([\w./\-]+\.(go|ts|svelte|json|md|py|sh|toml|txt|yaml|yml))/g;

  type ContentPart = { type: 'text' | 'artifact'; value: string };

  function renderContent(content: string): ContentPart[] {
    const parts: ContentPart[] = [];
    let last = 0;
    let match: RegExpExecArray | null;
    artifactPattern.lastIndex = 0;
    while ((match = artifactPattern.exec(content)) !== null) {
      if (match.index > last) {
        parts.push({ type: 'text', value: content.slice(last, match.index) });
      }
      parts.push({ type: 'artifact', value: match[1] });
      last = match.index + match[0].length;
    }
    if (last < content.length) {
      parts.push({ type: 'text', value: content.slice(last) });
    }
    return parts.length ? parts : [{ type: 'text', value: content }];
  }

  // Pulse breadcrumb for status bar
  const statusLabel = $derived(
    isBusy ? heartbeat.breadcrumbs.at(-1) ?? 'Working…' : 'Online'
  );

  function copyText(key: string, text: string) {
    navigator.clipboard.writeText(text).then(() => {
      copiedKey = key;
      setTimeout(() => { if (copiedKey === key) copiedKey = ''; }, 1500);
    });
  }
</script>

<div class="chat-area">
  <!-- Message list -->
  <div class="messages" bind:this={scrollEl}>
    {#if messages.length === 0}
      {#if !isDM && !wr.activeProjectID}
        <!-- First-run: no project exists yet — guide the user to the Lead Agent -->
        <div class="empty">
          <div class="empty-icon">
            <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.25" opacity="0.35"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>
          </div>
          <p class="empty-title">Welcome to Kōtui</p>
          <p class="empty-sub">Click <strong>Lead</strong> in the sidebar to meet your Lead Agent.<br>Introduce yourself and give them a name, personality, and skills.</p>
        </div>
      {:else}
        <!-- Channel exists but no messages yet -->
        <div class="empty">
          <div class="empty-icon">
            <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.25" opacity="0.3"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
          </div>
          <p class="empty-title">No messages yet</p>
          <p class="empty-sub">{isDM ? 'Start the conversation below.' : 'Type a message below to get started.'}</p>
        </div>
      {/if}
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
          <button class="copy-btn" onclick={() => copyText(msg.id || msg.created_at, msg.content)}
            title="Copy">
            {#if copiedKey === (msg.id || msg.created_at)}✓{:else}
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
            {/if}
          </button>
        </div>
      {:else if msg.kind === 'system_event'}
        <!-- System event — subtle centred line -->
        <div class="system-event">
          <span class="system-event-text">{msg.content}</span>
          <button class="copy-btn" onclick={() => copyText(msg.id || msg.created_at, msg.content)}
            title="Copy">
            {#if copiedKey === (msg.id || msg.created_at)}✓{:else}
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
            {/if}
          </button>
        </div>
      {:else if msg.kind === 'consultation'}
        <!-- Consultation bubble — amber, agent seeking clarification -->
        <div class="row row-agent">
          <div class="avatar avatar-consult" title={senderName(msg)}>
            {avatarInitials(senderName(msg))}
          </div>
          <div class="agent-bubble-wrap">
            <div class="bubble-meta">
              <span class="bubble-sender">{senderName(msg)}</span>
              <span class="consult-badge">needs clarification</span>
              <span class="bubble-time">{formatTime(msg.created_at)}</span>
            </div>
            <div class="bubble bubble-consult">
              <p class="bubble-text">{msg.content}</p>
            </div>
          </div>
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
              {#if metaThinking(msg.metadata)}
                {@const think = metaThinking(msg.metadata)}
                <details class="think-block">
                  <summary class="think-summary">thinking…</summary>
                  <div class="think-body">{think}</div>
                </details>
                <p class="bubble-text">{#each renderContent(msg.content) as part}{#if part.type === 'artifact'}<span class="artifact-pill">📄 {part.value}</span>{:else}{part.value}{/if}{/each}</p>
              {:else if parseThink(msg.content).thinking}
                {@const parsed = parseThink(msg.content)}
                <details class="think-block">
                  <summary class="think-summary">thinking…</summary>
                  <div class="think-body">{parsed.thinking}</div>
                </details>
                <p class="bubble-text">{#each renderContent(parsed.response) as part}{#if part.type === 'artifact'}<span class="artifact-pill">📄 {part.value}</span>{:else}{part.value}{/if}{/each}</p>
              {:else}
                <p class="bubble-text">{#each renderContent(msg.content) as part}{#if part.type === 'artifact'}<span class="artifact-pill">📄 {part.value}</span>{:else}{part.value}{/if}{/each}</p>
              {/if}
            </div>
          </div>
        </div>
      {/if}
    {/each}

    <!-- Streaming bubble — live token preview while agent is responding (channel and DM) -->
    {#if streamContent}
      {@const streamAgentName = isDM ? agentName(dmAgentID) : agentName('lead')}
      <div class="row row-agent">
        <div class="avatar" title={streamAgentName}>
          {avatarInitials(streamAgentName || 'A')}
        </div>
        <div class="agent-bubble-wrap">
          <div class="bubble-meta">
            <span class="bubble-sender">{streamAgentName}</span>
            <span class="streaming-badge">streaming…</span>
          </div>
          <!-- Thinking block: shown whenever <think> has been opened, even if
               no content has arrived yet (prevents raw tag flash in bubble).
               Stays open and auto-scrolls to the bottom while streaming. -->
          {#if streamParsed.thinking || streamParsed.inThink}
            <details class="think-block" open>
              <summary class="think-summary">thinking…</summary>
              <div class="think-body" bind:this={streamThinkEl}>{streamParsed.thinking}</div>
            </details>
          {/if}
          <!-- Response bubble: shown once the model moves past the think block. -->
          {#if streamParsed.response}
            <div class="bubble bubble-agent">
              <p class="bubble-text">{streamParsed.response}</p>
            </div>
          {:else if !streamParsed.inThink && !streamParsed.thinking}
            <!-- No thinking at all — plain streaming response. -->
            <div class="bubble bubble-agent">
              <p class="bubble-text">{streamContent}</p>
            </div>
          {/if}
        </div>
      </div>
    {:else if isBusy}
      <!-- Typing indicator — shown when busy but no stream content yet -->
      {@const busyAgentName = isDM ? agentName(dmAgentID) : agentName('lead')}
      <div class="row row-agent">
        <div class="avatar">{avatarInitials(busyAgentName || 'A')}</div>
        <div class="agent-bubble-wrap">
          <div class="bubble-meta"><span class="bubble-sender">{busyAgentName}</span></div>
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
      <div class="send-error">
        <span>{sendError}</span>
        <button class="copy-btn copy-btn-error" onclick={() => copyText('send-error', sendError)}
          title="Copy error">
          {#if copiedKey === 'send-error'}✓{:else}
            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
          {/if}
        </button>
      </div>
    {/if}
    <div class="composer-box">
      <textarea
        class="composer-input"
        placeholder={wr.activeProjectID ? (isDM ? `Message ${dmAgentID || 'agent'}…` : 'Message the Lead…') : 'Select a channel first…'}
        disabled={!wr.activeProjectID}
        bind:value={input}
        bind:this={inputEl}
        onkeydown={onKeydown}
        oninput={onInput}
        rows={1}
      ></textarea>
      <button
        class="send-btn"
        disabled={!input.trim()}
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
  .empty-sub { font-size: 0.8125rem; text-align: center; line-height: 1.6; }
  .empty-sub strong { color: var(--text-secondary); font-weight: 600; }

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
  /* Consultation bubble — amber, agent is seeking clarification */
  .bubble-consult {
    background: color-mix(in srgb, #f59e0b 12%, transparent);
    border: 1px solid color-mix(in srgb, #f59e0b 40%, transparent);
    border-bottom-left-radius: 4px;
    color: var(--bubble-agent-text);
  }
  .avatar-consult {
    background: color-mix(in srgb, #f59e0b 25%, var(--bg-surface));
    border: 1px solid color-mix(in srgb, #f59e0b 50%, transparent);
    color: #d97706;
  }
  .consult-badge {
    font-size: 0.6875rem;
    color: #d97706;
    background: color-mix(in srgb, #f59e0b 15%, transparent);
    border-radius: 3px;
    padding: 0.1rem 0.35rem;
    letter-spacing: 0.04em;
  }
  .bubble-text {
    font-size: 1rem;
    line-height: 1.55;
    white-space: pre-wrap;
    margin: 0;
  }
  .artifact-pill {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    background: var(--bg-surface);
    border: 1px solid var(--border-subtle);
    border-radius: 5px;
    padding: 0.1rem 0.4rem;
    font-size: 0.8125rem;
    font-family: monospace;
    color: var(--accent);
    white-space: nowrap;
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
    display: flex;
    align-items: center;
    gap: 0.35rem;
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
    display: flex;
    align-items: center;
    gap: 0.35rem;
  }
  .system-event-text {
    font-size: 0.8125rem;
    color: var(--text-muted);
  }

  .copy-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-muted);
    opacity: 0;
    transition: opacity 0.15s, color 0.15s;
    padding: 2px;
    border-radius: 3px;
    font-size: 0.75rem;
    line-height: 1;
    flex-shrink: 0;
  }
  .milestone:hover .copy-btn,
  .system-event:hover .copy-btn,
  .send-error:hover .copy-btn {
    opacity: 1;
  }
  .copy-btn:hover { color: var(--text-primary); background: var(--bg-hover); }
  .copy-btn-error { color: #f87171; }
  .copy-btn-error:hover { color: #fca5a5; background: rgba(248,113,113,0.12); }

  /* Streaming badge */
  .streaming-badge {
    font-size: 0.6875rem;
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    border-radius: 3px;
    padding: 0.1rem 0.35rem;
    letter-spacing: 0.04em;
    animation: pulse-text 1.4s ease-in-out infinite;
  }
  @keyframes pulse-text {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.45; }
  }

  /* Think block */
  .think-block {
    margin-bottom: 0.25rem;
    max-width: 100%;
  }
  .think-summary {
    font-size: 0.75rem;
    color: var(--text-muted);
    cursor: pointer;
    user-select: none;
    list-style: none;
    display: flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.15rem 0;
  }
  .think-summary::before {
    content: '▸';
    font-size: 0.625rem;
    transition: transform 0.15s;
  }
  details[open] .think-summary::before {
    transform: rotate(90deg);
  }
  .think-body {
    font-size: 0.8125rem;
    color: var(--text-muted);
    font-style: italic;
    background: color-mix(in srgb, var(--bg-surface) 60%, transparent);
    border-left: 2px solid var(--border-subtle);
    border-radius: 0 6px 6px 0;
    padding: 0.4rem 0.625rem;
    margin-top: 0.2rem;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 240px;
    overflow-y: auto;
    line-height: 1.5;
  }
  .think-body::-webkit-scrollbar { width: 3px; }
  .think-body::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 3px; }

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
    display: flex;
    align-items: center;
    gap: 0.35rem;
    font-size: 0.75rem;
    color: #f87171;
    padding: 0.25rem 0.125rem 0.375rem;
  }
  .send-error span { flex: 1; }
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
