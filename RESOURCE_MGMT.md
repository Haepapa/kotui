# Resource & Performance Management

## 1. Model Lifecycle (VRAM Offloading)
* **The "Active Model" Rule:** Only the Lead Agent (30B) and the currently executing Specialist (8B) should be loaded into VRAM.
* **Dynamic Unloading:** When a Specialist finishes, the orchestrator sends a `keep_alive: 0` request to Ollama to free memory.
* **Lead Priority:** The Lead Agent (30B) is kept in memory (`keep_alive: -1`) for instant Boss responses.

## 2. Tiered Data Streaming
* **Primary Thread:** The Group Chat uses a lightweight event stream.
* **Console Thread:** Raw logs are buffered in Go and only sent to the UI when the Engine Room window is active.