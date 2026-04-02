# Resource & Performance Management

## 1. Adaptive Model Lifecycle (VRAM Management)

The system profiles available memory at startup and adapts its model loading strategy accordingly. It makes **no assumption** about model sizes — the same logic works whether the Primary Lead is a 3B model on a Raspberry Pi or a 32B model on a desktop GPU.

### VRAM Profiles
* **Dual mode:** Lead + one Worker fit in available memory simultaneously. Lead stays persistent (`keep_alive: -1`), Workers release immediately on completion (`keep_alive: 0`).
* **Swap mode:** Only one model fits at a time. Before loading a Worker, the Lead is "parked" (`keep_alive: 0`, wait for unload). After the Worker completes, the Lead is reloaded. This is transparent — the Boss never notices the swap.

### Senior Consultant VRAM Strategy
When a Senior Consultant is needed and runs locally, the Dispatcher first parks **both** the Lead and any active Worker to free maximum memory before loading the larger model. On return, it reloads the Lead (and any queued Worker). If the Senior Consultant runs on a **remote endpoint**, there is no local VRAM impact.

### The "Active Model" Rule
At any moment, only the following models should be loaded locally:
* The **Primary Lead** (always-on in dual mode; parked during worker turns in swap mode).
* The **currently executing Worker** (one at a time; released immediately after).
* OR the **Senior Consultant** (on-demand only; released after the escalation task).

### Power Efficiency
When the Primary Lead is small (3B–8B), idle power consumption is minimal. This makes 24/7 headless operation on a low-power device (NUC, MacMini, Raspberry Pi 5) practical. The Senior Consultant is only pulled into service when needed and may run on a separate machine that is otherwise off.

## 2. Tiered Data Streaming
* **Primary Thread:** The Group Chat uses a lightweight event stream.
* **Console Thread:** Raw logs are buffered in Go and only sent to the UI when the Engine Room window is active.