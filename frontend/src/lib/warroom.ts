// warroom.ts — Wails service wrappers using auto-generated bindings.
//
// Uses Call.ByID (numeric IDs) from the generated bindings for reliability.
// Event subscriptions still use Events.On directly.

import {
  CreateProject as _CreateProject,
  GetActiveConversation as _GetActiveConversation,
  GetAgents as _GetAgents,
  GetHeartbeat as _GetHeartbeat,
  GetMessages as _GetMessages,
  GetProjects as _GetProjects,
  SendBossCommand as _SendBossCommand,
  SwitchProject as _SwitchProject,
} from '../../bindings/github.com/haepapa/kotui/internal/warroom/warroomservice';

import { Events } from '@wailsio/runtime';
import type { AgentInfo, HeartbeatState, KotuiMessage, Project } from './types';

// --- Service method wrappers -------------------------------------------

export function getProjects(): Promise<Project[]> {
  return _GetProjects() as Promise<Project[]>;
}

export function createProject(name: string, description: string): Promise<Project> {
  return _CreateProject(name, description) as Promise<Project>;
}

export function switchProject(id: string): Promise<void> {
  return _SwitchProject(id);
}

export function getActiveConversation(): Promise<string> {
  return _GetActiveConversation();
}

export function getMessages(conversationID: string, limit: number): Promise<KotuiMessage[]> {
  return _GetMessages(conversationID, limit) as Promise<KotuiMessage[]>;
}

/** Sends a boss command. Returns immediately; responses arrive via events. */
export function sendBossCommand(command: string): Promise<void> {
  return _SendBossCommand(command);
}

export function getAgents(): Promise<AgentInfo[]> {
  return _GetAgents() as Promise<AgentInfo[]>;
}

export function getHeartbeat(): Promise<HeartbeatState> {
  return _GetHeartbeat() as Promise<HeartbeatState>;
}

// --- Event subscriptions -----------------------------------------------

type MessageHandler = (msg: KotuiMessage) => void;
type HeartbeatHandler = (hb: HeartbeatState) => void;
type ErrorHandler = (err: { error: string }) => void;

export function onMessage(handler: MessageHandler): () => void {
  return Events.On('kotui:message', (ev) => handler(ev.data as KotuiMessage));
}

export function onHeartbeat(handler: HeartbeatHandler): () => void {
  return Events.On('kotui:heartbeat', (ev) => handler(ev.data as HeartbeatState));
}

export function onError(handler: ErrorHandler): () => void {
  return Events.On('kotui:error', (ev) => handler(ev.data as { error: string }));
}
