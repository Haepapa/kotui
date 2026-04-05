<script lang="ts">
  import { onMount } from 'svelte';

  interface Props {
    diagram: string;
  }

  let { diagram }: Props = $props();

  let svg = $state('');
  let renderError = $state('');
  let container: HTMLElement;

  onMount(async () => {
    try {
      const mermaid = (await import('mermaid')).default;
      mermaid.initialize({
        startOnLoad: false,
        theme: 'dark',
        securityLevel: 'loose',
        fontFamily: 'inherit',
      });
      const id = 'mmd-' + Math.random().toString(36).slice(2, 9);
      const { svg: rendered } = await mermaid.render(id, diagram);
      svg = rendered;
    } catch (e) {
      renderError = String(e);
    }
  });
</script>

{#if svg}
  <div class="mermaid-wrap" bind:this={container}>
    <!-- Mermaid generates safe SVG — @html is intentional here -->
    {@html svg}
  </div>
{:else if renderError}
  <pre class="mermaid-error">⚠ Diagram error: {renderError}

{diagram}</pre>
{:else}
  <div class="mermaid-loading">⠋ Rendering diagram…</div>
{/if}

<style>
  .mermaid-wrap {
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 0.5rem;
    padding: 1rem;
    margin: 0.5rem 0;
    overflow-x: auto;
    text-align: center;
  }

  .mermaid-wrap :global(svg) {
    max-width: 100%;
    height: auto;
  }

  .mermaid-error {
    background: rgba(248, 113, 113, 0.08);
    border: 1px solid rgba(248, 113, 113, 0.3);
    border-radius: 0.375rem;
    padding: 0.75rem;
    font-size: 0.75rem;
    color: #f87171;
    white-space: pre-wrap;
    overflow-x: auto;
  }

  .mermaid-loading {
    color: var(--text-secondary);
    font-size: 0.75rem;
    padding: 0.5rem 0;
    animation: pulse 1.2s ease-in-out infinite;
  }

  @keyframes pulse {
    0%, 100% { opacity: 0.4; }
    50% { opacity: 1; }
  }
</style>
