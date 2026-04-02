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
  class:dev={mode === 'dev'}
  onclick={ontoggle}
  title={mode === 'boss' ? 'Switch to Dev Mode' : 'Switch to Boss Mode'}
>
  {mode === 'boss' ? '🎯 Boss' : '⚙️ Dev'}
</button>

<style>
  .mode-toggle {
    background: #0f172a;
    border: 1px solid #1e293b;
    border-radius: 5px;
    color: #64748b;
    cursor: pointer;
    font-size: 0.6875rem;
    font-weight: 600;
    padding: 0.2rem 0.6rem;
    letter-spacing: 0.03em;
    transition: all 0.15s;
    white-space: nowrap;
  }
  .mode-toggle:hover {
    border-color: #334155;
    color: #94a3b8;
  }
  .mode-toggle.dev {
    background: #172554;
    border-color: #1d4ed8;
    color: #93c5fd;
  }
</style>
