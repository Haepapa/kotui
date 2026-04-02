<script lang="ts">
  import type { Approval } from '../lib/types';
  import { decideApproval } from '../lib/warroom';
  import { refreshApprovals } from '../stores/warroom.svelte';

  interface Props {
    approval: Approval;
  }
  let { approval }: Props = $props();

  let deciding = $state(false);

  async function decide(decision: 'approved' | 'rejected') {
    deciding = true;
    try {
      await decideApproval(approval.id, decision);
      await refreshApprovals();
    } catch (e) {
      console.error('decideApproval:', e);
    } finally {
      deciding = false;
    }
  }

  function formatTime(iso: string): string {
    if (!iso) return '';
    try { return new Date(iso).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }); }
    catch { return ''; }
  }

  function kindLabel(kind: string): string {
    switch (kind) {
      case 'hiring': return '👤 Hire';
      case 'skill_promotion': return '⬆️ Promote';
      case 'sudo': return '🔐 Sudo';
      default: return kind;
    }
  }
</script>

<div class="card">
  <div class="card-header">
    <span class="kind-badge">{kindLabel(approval.kind)}</span>
    <span class="card-time">{formatTime(approval.created_at)}</span>
  </div>
  <p class="card-desc">{approval.description}</p>
  <div class="card-actions">
    <button class="btn approve" onclick={() => decide('approved')} disabled={deciding}>✓ Approve</button>
    <button class="btn reject" onclick={() => decide('rejected')} disabled={deciding}>✗ Reject</button>
  </div>
</div>

<style>
  .card {
    background: var(--bg-surface);
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    padding: 0.625rem 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
    margin: 0 0.5rem;
  }
  .card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.375rem;
  }
  .kind-badge {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--accent);
  }
  .card-time {
    font-size: 0.6875rem;
    color: var(--text-muted);
  }
  .card-desc {
    font-size: 0.8125rem;
    color: var(--text-secondary);
    margin: 0;
    line-height: 1.4;
  }
  .card-actions {
    display: flex;
    gap: 0.375rem;
  }
  .btn {
    flex: 1;
    padding: 0.3rem 0;
    font-size: 0.75rem;
    border-radius: 6px;
    border: none;
    cursor: pointer;
    font-weight: 600;
    transition: opacity 0.12s;
  }
  .btn:disabled { opacity: 0.4; cursor: default; }
  .approve { background: rgba(74,222,128,0.15); color: #4ade80; }
  .approve:hover:not(:disabled) { background: rgba(74,222,128,0.25); }
  .reject { background: rgba(248,113,113,0.15); color: #f87171; }
  .reject:hover:not(:disabled) { background: rgba(248,113,113,0.25); }
</style>
