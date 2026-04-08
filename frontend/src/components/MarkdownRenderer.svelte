<script lang="ts">
  import { marked, type Renderer } from 'marked';
  import hljs from 'highlight.js';
  import MermaidDiagram from './MermaidDiagram.svelte';

  interface Props {
    text: string;
  }
  const { text = '' }: Props = $props();

  // Configure marked with a custom renderer once at module scope.
  const renderer: Partial<Renderer> = {
    // Code blocks — syntax highlight with hljs, mermaid blocks handled separately below.
    code({ text: code, lang }) {
      const language = lang && hljs.getLanguage(lang) ? lang : '';
      const highlighted = language
        ? hljs.highlight(code, { language }).value
        : hljs.highlightAuto(code).value;
      const langLabel = lang ? `<span class="code-lang">${lang}</span>` : '';
      return `<div class="code-block">${langLabel}<pre><code class="hljs">${highlighted}</code></pre></div>`;
    },
    // Inline code
    codespan({ text: code }) {
      return `<code class="inline-code">${code}</code>`;
    },
  };

  marked.use({ renderer, gfm: true, breaks: true });

  type Segment = { type: 'markdown'; html: string } | { type: 'mermaid'; code: string };

  // Split text into mermaid blocks (passed to MermaidDiagram) and everything
  // else (rendered through marked).
  function parseSegments(raw: string): Segment[] {
    const segments: Segment[] = [];
    const mermaidRe = /```mermaid\n([\s\S]*?)```/g;
    let last = 0;
    let m: RegExpExecArray | null;
    while ((m = mermaidRe.exec(raw)) !== null) {
      if (m.index > last) {
        segments.push({ type: 'markdown', html: marked.parse(raw.slice(last, m.index)) as string });
      }
      segments.push({ type: 'mermaid', code: m[1].trim() });
      last = m.index + m[0].length;
    }
    if (last < raw.length) {
      segments.push({ type: 'markdown', html: marked.parse(raw.slice(last)) as string });
    }
    return segments.length ? segments : [{ type: 'markdown', html: marked.parse(raw) as string }];
  }

  const segments = $derived(parseSegments(text));
</script>

<div class="md-root">
  {#each segments as seg}
    {#if seg.type === 'mermaid'}
      <MermaidDiagram diagram={seg.code} />
    {:else}
      <!-- svelte-ignore security-review -- content is local AI output, not user-supplied HTML -->
      <div class="md-content">{@html seg.html}</div>
    {/if}
  {/each}
</div>

<style>
  .md-root {
    font-size: 1rem;
    line-height: 1.6;
    color: inherit;
    min-width: 0;
    user-select: text;
    -webkit-user-select: text;
    cursor: text;
  }

  /* Remove top margin from the first block and bottom from the last */
  .md-content :global(> *:first-child) { margin-top: 0; }
  .md-content :global(> *:last-child)  { margin-bottom: 0; }

  /* Paragraphs */
  .md-content :global(p) {
    margin: 0 0 0.55em;
  }

  /* Headings */
  .md-content :global(h1),
  .md-content :global(h2),
  .md-content :global(h3),
  .md-content :global(h4),
  .md-content :global(h5),
  .md-content :global(h6) {
    font-weight: 600;
    line-height: 1.3;
    margin: 1em 0 0.35em;
    color: var(--text-heading);
  }
  .md-content :global(h1) { font-size: 1.25rem; }
  .md-content :global(h2) { font-size: 1.125rem; }
  .md-content :global(h3) { font-size: 1rem; }
  .md-content :global(h4),
  .md-content :global(h5),
  .md-content :global(h6) { font-size: 0.9375rem; }

  /* Lists */
  .md-content :global(ul),
  .md-content :global(ol) {
    margin: 0.35em 0 0.55em 1.5em;
    padding: 0;
  }
  .md-content :global(li) { margin: 0.2em 0; }
  .md-content :global(li > p) { margin: 0; }

  /* Horizontal rule */
  .md-content :global(hr) {
    border: none;
    border-top: 1px solid var(--border-subtle);
    margin: 0.75em 0;
  }

  /* Blockquote */
  .md-content :global(blockquote) {
    margin: 0.5em 0;
    padding: 0.35em 0.75em;
    border-left: 3px solid var(--accent);
    background: var(--bg-surface);
    border-radius: 0 6px 6px 0;
    color: var(--text-secondary);
    font-style: italic;
  }
  .md-content :global(blockquote p) { margin: 0; }

  /* Inline code */
  .md-content :global(.inline-code) {
    font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
    font-size: 0.875em;
    background: var(--bg-surface);
    border: 1px solid var(--border-subtle);
    border-radius: 4px;
    padding: 0.1em 0.35em;
    color: var(--accent);
    white-space: nowrap;
  }

  /* Code blocks */
  .md-content :global(.code-block) {
    position: relative;
    margin: 0.55em 0;
    border-radius: 8px;
    overflow: hidden;
    border: 1px solid var(--border-subtle);
    background: var(--bg-console, #0c0e14);
  }
  .md-content :global(.code-lang) {
    display: block;
    padding: 0.2rem 0.75rem;
    font-family: 'SF Mono', 'Fira Code', monospace;
    font-size: 0.7rem;
    font-weight: 600;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--text-muted);
    background: rgba(255,255,255,0.04);
    border-bottom: 1px solid var(--border-subtle);
  }
  .md-content :global(.code-block pre) {
    margin: 0;
    padding: 0.65rem 0.875rem;
    overflow-x: auto;
  }
  .md-content :global(.code-block code) {
    font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', 'Consolas', monospace;
    font-size: 0.8125rem;
    line-height: 1.6;
    background: none;
    border: none;
    padding: 0;
    color: #cdd6f4;
    white-space: pre;
  }

  /* highlight.js token colours — Catppuccin-inspired palette */
  .md-content :global(.hljs-keyword)   { color: #cba6f7; }
  .md-content :global(.hljs-built_in)  { color: #89dceb; }
  .md-content :global(.hljs-type)      { color: #f38ba8; }
  .md-content :global(.hljs-literal)   { color: #fab387; }
  .md-content :global(.hljs-number)    { color: #fab387; }
  .md-content :global(.hljs-operator)  { color: #89dceb; }
  .md-content :global(.hljs-string)    { color: #a6e3a1; }
  .md-content :global(.hljs-comment)   { color: #585b70; font-style: italic; }
  .md-content :global(.hljs-variable)  { color: #cdd6f4; }
  .md-content :global(.hljs-title),
  .md-content :global(.hljs-title.function_) { color: #89b4fa; }
  .md-content :global(.hljs-title.class_)    { color: #f9e2af; }
  .md-content :global(.hljs-params)    { color: #cdd6f4; }
  .md-content :global(.hljs-attr)      { color: #89b4fa; }
  .md-content :global(.hljs-tag)       { color: #f38ba8; }
  .md-content :global(.hljs-name)      { color: #cba6f7; }
  .md-content :global(.hljs-selector-class),
  .md-content :global(.hljs-selector-id) { color: #f9e2af; }
  .md-content :global(.hljs-property)  { color: #89b4fa; }
  .md-content :global(.hljs-punctuation) { color: #cdd6f4; }
  .md-content :global(.hljs-meta)      { color: #fab387; }
  .md-content :global(.hljs-symbol)    { color: #f9e2af; }
  .md-content :global(.hljs-deletion)  { color: #f38ba8; }
  .md-content :global(.hljs-addition)  { color: #a6e3a1; }

  /* Light theme token overrides */
  :global([data-theme="light"]) .md-content :global(.code-block) {
    background: #f5f5f7;
  }
  :global([data-theme="light"]) .md-content :global(.code-block code) {
    color: #24292e;
  }
  :global([data-theme="light"]) .md-content :global(.hljs-keyword)  { color: #d73a49; }
  :global([data-theme="light"]) .md-content :global(.hljs-built_in) { color: #005cc5; }
  :global([data-theme="light"]) .md-content :global(.hljs-type)     { color: #6f42c1; }
  :global([data-theme="light"]) .md-content :global(.hljs-string)   { color: #22863a; }
  :global([data-theme="light"]) .md-content :global(.hljs-number)   { color: #005cc5; }
  :global([data-theme="light"]) .md-content :global(.hljs-comment)  { color: #6a737d; }
  :global([data-theme="light"]) .md-content :global(.hljs-title),
  :global([data-theme="light"]) .md-content :global(.hljs-title.function_) { color: #6f42c1; }
  :global([data-theme="light"]) .md-content :global(.hljs-attr)     { color: #005cc5; }

  /* Tables */
  .md-content :global(table) {
    border-collapse: collapse;
    width: 100%;
    margin: 0.55em 0;
    font-size: 0.875rem;
  }
  .md-content :global(th),
  .md-content :global(td) {
    padding: 0.3rem 0.6rem;
    border: 1px solid var(--border-subtle);
    text-align: left;
  }
  .md-content :global(th) {
    background: var(--bg-surface);
    font-weight: 600;
    color: var(--text-heading);
  }
  .md-content :global(tr:nth-child(even) td) {
    background: rgba(255,255,255,0.02);
  }
  :global([data-theme="light"]) .md-content :global(tr:nth-child(even) td) {
    background: rgba(0,0,0,0.02);
  }

  /* Links */
  .md-content :global(a) {
    color: var(--accent);
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .md-content :global(a:hover) { opacity: 0.8; }

  /* Strong / em */
  .md-content :global(strong) { font-weight: 700; color: var(--text-heading); }
  .md-content :global(em) { font-style: italic; }
</style>
