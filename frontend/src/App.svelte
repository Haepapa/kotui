<script lang="ts">
  // War Room — Phase 7 will replace this placeholder with the full UI.
  let status = $state('Initialising...');
</script>

<main>
  <div class="war-room-placeholder">
    <h1>Kotui</h1>
    <p class="subtitle">AgentFlow Orchestrator</p>
    <div class="status">{status}</div>
    <p class="note">War Room UI coming in Phase 7.</p>
  </div>
</main>

<style>
  :global(body) {
    margin: 0;
    background: #0f141e;
    color: #e2e8f0;
    font-family: system-ui, sans-serif;
  }
  .war-room-placeholder {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100vh;
    gap: 0.5rem;
  }
  h1 {
    font-size: 2.5rem;
    font-weight: 700;
    margin: 0;
    color: #7dd3fc;
  }
  .subtitle {
    font-size: 1rem;
    color: #94a3b8;
    margin: 0;
  }
  .status {
    margin-top: 1rem;
    font-size: 0.875rem;
    color: #4ade80;
  }
  .note {
    font-size: 0.75rem;
    color: #475569;
    margin: 0;
  }
</style>

