# Technical Architecture

## Core Stack
* **Language:** Go (Single-binary, high memory efficiency).
* **Frontend:** Wails v3 (Desktop GUI) / Headless Mode (Docker/Server).
* **Inference:** Ollama (Targeting 8B to 30B parameter models).
* **Protocol:** MCP (Model Context Protocol) for tool-calling interoperability.

## Containerization & Headless Operation
To support Docker and server-side deployment, the application supports a `--headless` flag.
* **Decoupled UI:** In headless mode, the Wails window is suppressed. The Go backend communicates exclusively through the Remote Messaging Relays (Slack/Telegram).
* **Persistence:** All state is maintained in a mounted `/data` volume, ensuring agent journals and company identity persist across container restarts.

## Data Flow
* **Dispatcher (Go):** Manages the central State Store and agent coordination.
* **Event Stream:** Broadcasts "Summary" data to the local UI and remote relays, and "Raw" data to the console/logs.