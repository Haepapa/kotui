1. Frontend Framework

The War Room UI will be built using Svelte 5.

    Rationale: Svelte’s minimal runtime and "runes" reactivity system are ideal for handling high-frequency data streams (like the Engine Room logs) without taxing the CPU, which must remain available for local LLM inference.

2. Target Ollama Models

The system is optimized for the following model families to ensure reliable tool calling and reasoning:

    Lead Agent (30B Class): qwen2.5-coder:32b or command-r. These models are selected for their superior context window management and high-precision structured JSON output.

    Specialist Agents (8B Class): llama3.1:8b or mistral:7b-v0.3. These provide the speed and efficiency required for rapid sub-task execution.

3. Data Persistence / State Store

The application will utilize an embedded SQLite database.

    Location: The database file resides within the mounted /data volume.

    Usage: It serves as the authoritative "Company Ledger," persisting threaded conversations, the hierarchical Task Tree, agent metadata, and the audit log of Boss approvals.

4. MCP Tools Inventory

The initial "Corporate Toolset" is categorized by agent clearance level:

    Lead Agent (Planning & Verification):

        file_manager: Directory mapping and high-level project oversight.

        project_critic: Advanced code analysis and logical verification.

    Specialist Agents (Execution):

        filesystem: Scoped read/write/delete operations within the project workspace.

        shell_executor: Execution of build and test commands (e.g., go build, npm test).

        web_search: Documentation retrieval and real-time data gathering.

        iot_gateway: Communication with remote nodes (e.g., Raspberry Pi Pico, LoRa gateways) via serial or SSH.

5. Vector Search for Journal Recall

    Embedding Model: nomic-embed-text (served locally via Ollama).

    Vector Storage: SQLite-VSS (Vector Syntax Support).

    Implementation: This allow for semantic "Recall" queries to be executed as standard SQL, keeping the technical stack consolidated and local.

6. handbook.md Definition

The handbook.md is a static reference document that serves as the "Employee Orientation" guide.

    Content: It defines the immutable Standard Operating Procedures (SOPs), including mandatory journal formatting, inter-agent etiquette, and "Hard-Fail" safety constraints. It acts as the anchor for the agent's initial system prompt.

7. Inter-Agent Communication

Agents communicate through a high-speed, in-process Go Channel message bus.

    Flow: When an agent "speaks," the message is routed through the Dispatcher. The Dispatcher simultaneously updates the SQLite State Store, broadcasts the event to the Wails UI, and pushes to any active Remote Relays.

8. Frontend ↔ Backend Event Protocol

Events are delivered via Wails v3 native event bindings.

    Headless Mode: In Docker/Server deployments, a "Relay Gateway" intercepts these internal events and translates them into the appropriate API calls for Slack, Telegram, or WhatsApp.

9. Error Handling & Resilience

    System Failure: The Go backend maintains a heartbeat with Ollama. If the inference engine hangs or OOMs, the Lead Agent triggers a "System Emergency" notification to the Boss.

    Execution Loops: If a Specialist fails to produce output within 60 seconds, the Lead intervenes to kill the process and re-evaluate.

    Retries: Tool execution permits 3 retries. Upon the third failure, the task is paused and escalated for Boss intervention.

10. Agent-Produced Artifacts

    Storage: Artifacts are stored in /data/projects/{project_name}/artifacts.

    Access: The Lead Agent generates relative file paths that the Svelte UI renders as interactive links or preview components within the Group Chat.

11. Cloud Provider Integration

    MVP Scope: Local-only (Ollama) is the mandatory MVP requirement.

    Future-Proofing: Integration for OpenAI/Anthropic is architected as an optional "External Consultant" layer. API keys will be managed via the Settings UI and stored in an encrypted local vault.

12. Remote Messaging Authentication

    Setup: Credentials (Tokens/IDs) are configured via the Settings UI.

    Security: For the MVP, communication is secured via static bot tokens and HMAC signature verification for all incoming mobile webhooks.

13. Project / Workspace Concept

The "Company" supports Multiple Independent Projects.

    The War Room: Only one project may be active in the primary UI at a time.

    Context Switching: The Boss can switch projects via a workspace selector, which reloads the specific SQLite context and the associated agent team for that project.