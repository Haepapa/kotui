// warroom.ts — Wails service wrappers using auto-generated bindings.
//
// Uses Call.ByID (numeric IDs) from the generated bindings for reliability.
// Event subscriptions still use Events.On directly.

import {
  CreateProject as _CreateProject,
  DecideApproval as _DecideApproval,
  GetActiveConversation as _GetActiveConversation,
  GetAgents as _GetAgents,
  GetCompanyIdentity as _GetCompanyIdentity,
  GetConfig as _GetConfig,
  GetHeartbeat as _GetHeartbeat,
  GetMessages as _GetMessages,
  GetOrCreateDirectConversation as _GetOrCreateDirectConversation,
  GetPendingApprovals as _GetPendingApprovals,
  GetProjects as _GetProjects,
  SaveCompanyIdentity as _SaveCompanyIdentity,
  SaveConfig as _SaveConfig,
  SendBossCommand as _SendBossCommand,
  SendDirectMessage as _SendDirectMessage,
  SwitchProject as _SwitchProject,
  RenameProject as _RenameProject,
  ArchiveProject as _ArchiveProject,
  ListOllamaModels as _ListOllamaModels,
  PullOllamaModel as _PullOllamaModel,
  DeleteOllamaModel as _DeleteOllamaModel,
} from '../../bindings/github.com/haepapa/kotui/internal/warroom/warroomservice';

import { Events } from '@wailsio/runtime';
import type { AgentInfo, Approval, HeartbeatState, KotuiMessage, Project, UIConfig } from './types';

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

export function renameProject(id: string, name: string, description: string): Promise<void> {
  return _RenameProject(id, name, description);
}

export function archiveProject(id: string): Promise<void> {
  return _ArchiveProject(id);
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

export function getPendingApprovals(): Promise<Approval[]> {
  return _GetPendingApprovals() as Promise<Approval[]>;
}

export function decideApproval(id: string, decision: string): Promise<void> {
  return _DecideApproval(id, decision);
}

export function getConfig(): Promise<UIConfig> {
  return _GetConfig() as Promise<UIConfig>;
}

export function saveConfig(cfg: UIConfig): Promise<void> {
  return _SaveConfig(cfg as any);
}

export function getCompanyIdentity(): Promise<string> {
  return _GetCompanyIdentity();
}

export function saveCompanyIdentity(content: string): Promise<void> {
  return _SaveCompanyIdentity(content);
}

export function getOrCreateDirectConversation(agentID: string): Promise<string> {
  return _GetOrCreateDirectConversation(agentID);
}

export function sendDirectMessage(agentID: string, message: string): Promise<void> {
  return _SendDirectMessage(agentID, message);
}

export function listOllamaModels(endpoint: string = ''): Promise<string[]> {
  return _ListOllamaModels(endpoint) as Promise<string[]>;
}

export function pullOllamaModel(name: string): Promise<void> {
  return _PullOllamaModel(name);
}

export function deleteOllamaModel(name: string): Promise<void> {
  return _DeleteOllamaModel(name);
}

// --- Event subscriptions -----------------------------------------------

type MessageHandler = (msg: KotuiMessage) => void;
type HeartbeatHandler = (hb: HeartbeatState) => void;
type ErrorHandler = (err: { error: string }) => void;
type ApprovalHandler = (approvals: Approval[]) => void;

export function onMessage(handler: MessageHandler): () => void {
  return Events.On('kotui:message', (ev) => handler(ev.data as KotuiMessage));
}

export function onHeartbeat(handler: HeartbeatHandler): () => void {
  return Events.On('kotui:heartbeat', (ev) => handler(ev.data as HeartbeatState));
}

export function onError(handler: ErrorHandler): () => void {
  return Events.On('kotui:error', (ev) => handler(ev.data as { error: string }));
}

export function onApproval(handler: ApprovalHandler): () => void {
  return Events.On('kotui:approval', (ev) => handler(ev.data as Approval[]));
}
