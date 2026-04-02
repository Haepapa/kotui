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
  {mode === 'boss' ? 'Boss' : 'Dev'}
</button>

<style>
  .mode-toggle {
    background: #22262f;
    border: 1px solid #2a2d35;
    border-radius: 6px;
    color: #64748b;
    cursor: pointer;
    font-size: 0.75rem;
    font-weight: 600;
    padding: 0.25rem 0.75rem;
    letter-spacing: 0.03em;
    transition: all 0.15s;
    display: flex;
    align-items: center;
    justify-content: center;
    height: 28px;
    white-space: nowrap;
  }
  .mode-toggle:hover {
    border-color: #3a3d46;
    color: #94a3b8;
  }
  .mode-toggle.dev {
    background: #1e2d4a;
    border-color: #2563eb;
    color: #93c5fd;
  }
</style>

