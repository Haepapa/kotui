# Docker & Server Deployment

## 1. The Dockerfile Strategy
Multi-stage build compiling the Go backend and bundling MCP tools into a lightweight runtime image.

## 2. Docker Compose
Links the Orchestrator with a local Ollama instance, using volumes to persist agent souls, company values, and journals across restarts.

## 3. Security
Docker provides filesystem isolation, ensuring agents can only interact with files within the mounted `/data` volume.