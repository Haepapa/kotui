# Infrastructure & Model Settings

## 1. The "IT Manager" UX
The Settings view is the "Infrastructure Office," featuring a background persona who explains the technical impact of toggles on company performance.

## 2. Model Providers

### Local (Ollama) — Default
The Primary Lead and Workers run on the local Ollama instance. Any model size is supported; the system profiles available memory and selects `dual` or `swap` mode automatically.

### Senior Consultant — On-Demand, Local or Remote
Configured separately from the Primary Lead. Invoked only when the Lead signals a capability escalation. Three deployment options:

| Option | Config | Notes |
|--------|--------|-------|
| **Local larger model** | `endpoint = "http://localhost:11434"`, `model = "qwen2.5-coder:32b"` | Uses same Ollama instance; Parks Lead first |
| **Remote LAN machine** | `endpoint = "http://192.168.1.50:11434"` | No local VRAM cost; remote must be running Ollama |
| **Wake-on-demand (SSH)** | `ssh_host = "gpu-box"`, `ssh_start_cmd = "ollama serve"` | App SSH's in, starts Ollama, uses it, allows sleep after |

### Cloud (OpenAI / Anthropic) — Future
Architected as an optional senior consultant layer. API keys stored in an encrypted local vault. Not included in the MVP.

## 3. Connectivity
* **Remote Hubs:** Configure connections to IoT outposts (Raspberry Pi/LoRa).
* **Timezone:** Aligns agent journals with local New Zealand time.

## 4. Hardware Profiles
The Settings UI surfaces a recommended configuration based on detected hardware:

| Profile | RAM | Suggested Lead | Notes |
|---------|-----|---------------|-------|
| Nano | ≤ 4 GB | `llama3.2:3b` | Headless only; limited context |
| Standard | 8–16 GB | `llama3.1:8b` or `mistral:7b` | Full desktop use |
| Enhanced | 16–32 GB | `qwen2.5-coder:14b` | Good dual-mode with 8B worker |
| High-end | 32 GB+ | `qwen2.5-coder:32b` | Dual mode with 8B worker fits |