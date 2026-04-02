# Company Identity: Vision & Values

This document defines the "Global State" of the project. It is the highest-level context provided to every agent in the "War Room."

## 1. Core Identity
* **Vision:** [e.g., "To build robust, local-first hardware solutions for New Zealand agriculture."]
* **Purpose:** [e.g., "Prototyping a LoRa-based virtual fence for livestock."]
* **Long-Term Goals:** [e.g., "Achieve 99.9% uptime on remote nodes," "Minimize battery consumption."]

## 2. Company Values (The "How We Work")
These act as decision-making tie-breakers for agents:
* **Precision over Speed:** We prefer a slow, verified result over a fast, hallucinated one.
* **Radical Transparency:** If an agent is confused or a tool fails, it must report it immediately to the Boss.
* **Privacy First:** Never suggest a cloud-based tool if a local alternative exists.
* **Resource Mindfulness:** Be aware of the Boss's local hardware limits (VRAM/CPU).

## 3. The "Strategy Team" Interaction
The Boss can modify this document in two ways:
1. **Direct Edit:** Manually updating the Markdown file. The Go backend detects the "save" and broadcasts a "Culture Update" to all active agents.
2. **Strategy Session:** In the Chat UI, the Boss can call for a "Strategy Meeting." This temporarily pivots the Lead Agent's focus from execution to consultation.