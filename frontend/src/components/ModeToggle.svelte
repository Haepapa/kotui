<script lang="ts">
  import type { ViewMode } from '../lib/types';

  interface Props {
    mode: ViewMode;
    ontoggle: () => void;
  }

  let { mode, ontoggle }: Props = $props();
</script>

<button
  class="mode-toggle"
  class:active={mode === 'dev'}
  onclick={ontoggle}
  title={mode === 'boss' ? 'Show agent activity' : 'Hide agent activity'}
  aria-label={mode === 'boss' ? 'Show agent activity' : 'Hide agent activity'}
>
  <!-- Terminal / activity icon -->
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75">
    <polyline points="4 17 10 11 4 5"/>
    <line x1="12" y1="19" x2="20" y2="19"/>
  </svg>
</button>

<style>
  .mode-toggle {
    width: 30px;
    height: 30px;
    border-radius: 7px;
    color: var(--text-muted);
    transition: background 0.15s, color 0.15s;
  }
  .mode-toggle:hover { background: var(--bg-hover); color: var(--text-secondary); }
  .mode-toggle.active {
    background: var(--bg-active);
    color: var(--accent);
  }
</style>

