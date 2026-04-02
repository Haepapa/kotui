<script lang="ts">
  import { sendBossCommand } from '../lib/warroom';
  import { wr } from '../stores/warroom.svelte';

  let input = $state('');
  let sendError = $state('');

  async function handleSend() {
    const cmd = input.trim();
    if (!cmd || wr.isBusy) return;
    if (!wr.activeProjectID) {
      sendError = 'Select or create a project first.';
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

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }
</script>

<div class="command-input">
  {#if sendError}
    <div class="send-error">{sendError}</div>
  {/if}
  <div class="input-row">
    <textarea
      class="cmd-textarea"
      placeholder={wr.isBusy ? 'Lead is working…' : 'Give the Lead a command…'}
      disabled={wr.isBusy}
      bind:value={input}
      onkeydown={handleKeydown}
      rows={1}
    ></textarea>
    <button
      class="send-btn"
      disabled={wr.isBusy || !input.trim()}
      onclick={handleSend}
      title="Send (Enter)"
    >
      {wr.isBusy ? '⏳' : '▶'}
    </button>
  </div>
  <div class="hint">Enter to send · Shift+Enter for newline</div>
</div>

<style>
  .command-input {
    border-top: 1px solid #1e293b;
    padding: 0.625rem 1rem;
    background: #0f141e;
  }
  .send-error {
    font-size: 0.75rem;
    color: #f87171;
    margin-bottom: 0.375rem;
  }
  .input-row {
    display: flex;
    gap: 0.5rem;
    align-items: flex-end;
  }
  .cmd-textarea {
    flex: 1;
    background: #0b1020;
    border: 1px solid #1e293b;
    border-radius: 7px;
    color: #e2e8f0;
    font-size: 0.875rem;
    line-height: 1.5;
    padding: 0.5rem 0.75rem;
    resize: none;
    outline: none;
    font-family: inherit;
    max-height: 120px;
    transition: border-color 0.15s;
    field-sizing: content;
  }
  .cmd-textarea:focus { border-color: #38bdf8; }
  .cmd-textarea:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .send-btn {
    background: #1d4ed8;
    border: none;
    border-radius: 7px;
    color: #bfdbfe;
    cursor: pointer;
    font-size: 1rem;
    height: 38px;
    width: 42px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    transition: background 0.15s;
  }
  .send-btn:hover:not(:disabled) { background: #2563eb; }
  .send-btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .hint {
    font-size: 0.625rem;
    color: #1e293b;
    margin-top: 0.3rem;
    text-align: right;
  }
</style>
