# Technical Architecture

## Core Stack
* **Language:** Go (Single-binary, high memory efficiency).
* **Frontend:** Wails v3 (Desktop GUI) / Headless Mode (Docker/Server).
* **Inference:** Ollama (any model size — designed to work from 3B upwards).
* **Protocol:** MCP (Model Context Protocol) for tool-calling interoperability.

## Adaptive Model Architecture

The system does **not** require a large model to function. It is designed to run 24/7 on low-power hardware (a Raspberry Pi, NUC, or MacMini) using whatever model fits in available memory — and to optionally escalate to a larger model on demand.

### Model Tiers

| Tier | Typical Size | Role | Availability |
|------|-------------|------|--------------|
| **Primary Lead** | Any (3B–32B+) | Orchestration, task decomposition, verification | Always-on, local |
| **Worker** | ≤ Lead size | Sub-task execution under Lead direction | On-demand, local |
| **Senior Consultant** | Larger than Lead | Complex reasoning when Lead signals capability limit | On-demand, local **or remote** |

The Primary Lead can be an 8B model. Workers are typically the same size or smaller. The Senior Consultant — if configured — is invoked only when the Lead identifies that a task exceeds its own reasoning capacity.

### Capability Escalation
When the Primary Lead determines a task is beyond its reliable capability, it emits a `capability_escalation` signal. The Dispatcher:
1. Checks whether a Senior Consultant endpoint is configured.
2. Connects to that endpoint (starting the remote Ollama service if necessary).
3. Routes the complex sub-task to the Senior Consultant.
4. Returns the result to the Primary Lead, which resumes orchestration.

This means a modest 8B Lead can still handle sophisticated, long-running projects — it delegates the hard reasoning rather than attempting it directly and hallucinating.

### Remote Model Server Management
The Senior Consultant can run on:
* **Local:** A larger model on the same machine, loaded only when needed.
* **Remote Ollama:** A second machine on the LAN (home GPU box, cloud VM).
* **Wake-on-demand:** The app can SSH into a remote host and start `ollama serve`, then allow it to sleep after the task is complete — supporting power-efficient "big brain on demand" patterns.

## Containerization & Headless Operation
To support Docker and server-side deployment, the application supports a `--headless` flag.
* **Decoupled UI:** In headless mode, the Wails window is suppressed. The Go backend communicates exclusively through the Remote Messaging Relays (Slack/Telegram).
* **Persistence:** All state is maintained in a mounted `/data` volume, ensuring agent journals and company identity persist across container restarts.

## Data Flow
* **Dispatcher (Go):** Manages the central State Store and agent coordination.
* **Event Stream:** Broadcasts "Summary" data to the local UI and remote relays, and "Raw" data to the console/logs.