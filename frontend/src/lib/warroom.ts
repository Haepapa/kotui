// warroom.ts — TypeScript wrappers for the WarRoom Wails service.
//
// Since wails3 generate bindings is broken on Go 1.25, we manually
// write Call.ByName wrappers. The service is registered with Name: "WarRoom"
// so all calls use the format "WarRoom.MethodName".

import { Call, Events } from '@wailsio/runtime';
import type { AgentInfo, HeartbeatState, KotuiMessage, Project } from './types';

const SVC = 'WarRoom';

function call<T>(method: string, ...args: unknown[]): Promise<T> {
  return Call.ByName(`${SVC}.${method}`, ...args) as Promise<T>;
}

// --- Service method wrappers -------------------------------------------

export function getProjects(): Promise<Project[]> {
  return call<Project[]>('GetProjects');
}

export function createProject(name: string, description: string): Promise<Project> {
  return call<Project>('CreateProject', name, description);
}

export function switchProject(id: string): Promise<void> {
  return call<void>('SwitchProject', id);
}

export function getActiveConversation(): Promise<string> {
  return call<string>('GetActiveConversation');
}

export function getMessages(conversationID: string, limit: number): Promise<KotuiMessage[]> {
  return call<KotuiMessage[]>('GetMessages', conversationID, limit);
}

/** Sends a boss command. Returns immediately; responses arrive via events. */
export function sendBossCommand(command: string): Promise<void> {
  return call<void>('SendBossCommand', command);
}

export function getAgents(): Promise<AgentInfo[]> {
  return call<AgentInfo[]>('GetAgents');
}

export function getHeartbeat(): Promise<HeartbeatState> {
  return call<HeartbeatState>('GetHeartbeat');
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
