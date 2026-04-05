// warroom.ts — Wails service wrappers using auto-generated bindings.
//
// Uses Call.ByID (numeric IDs) from the generated bindings for reliability.
// Event subscriptions still use Events.On directly.

import {
  CreateProject as _CreateProject,
  DecideApproval as _DecideApproval,
  GetActiveConversation as _GetActiveConversation,
  GetAgentBrainFiles as _GetAgentBrainFiles,
  GetAgents as _GetAgents,
  GetCompanyIdentity as _GetCompanyIdentity,
  GetConfig as _GetConfig,
  GetHeartbeat as _GetHeartbeat,
  GetMessages as _GetMessages,
  GetOrCreateDirectConversation as _GetOrCreateDirectConversation,
  GetPendingApprovals as _GetPendingApprovals,
  GetProjects as _GetProjects,
  SaveAgentBrainFile as _SaveAgentBrainFile,
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
  InitFirstRun as _InitFirstRun,
  GetHandbook as _GetHandbook,
  SaveHandbook as _SaveHandbook,
  ResetAppData as _ResetAppData,
  ListSandboxFiles as _ListSandboxFiles,
  ReadSandboxFile as _ReadSandboxFile,
} from '../../bindings/github.com/haepapa/kotui/internal/warroom/warroomservice';
import type { BrainFiles as _BrainFiles, FirstRunResult as _FirstRunResult } from '../../bindings/github.com/haepapa/kotui/internal/warroom/models';

import { Events } from '@wailsio/runtime';
import type { AgentInfo, Approval, FileEntry, HeartbeatState, KotuiMessage, Project, UIConfig } from './types';

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

export function getHandbook(): Promise<string> {
  return _GetHandbook();
}

export function saveHandbook(content: string): Promise<void> {
  return _SaveHandbook(content);
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

export function pullOllamaModel(endpoint: string, name: string): Promise<void> {
  return _PullOllamaModel(endpoint, name);
}

export function deleteOllamaModel(endpoint: string, name: string): Promise<void> {
  return _DeleteOllamaModel(endpoint, name);
}

export function resetAppData(): Promise<void> {
  return _ResetAppData();
}

export type FirstRunResult = { conv_id: string; is_new: boolean };

export type BrainFiles = { soul: string; persona: string; skills: string };

export function getAgentBrainFiles(agentID: string): Promise<BrainFiles> {
  return _GetAgentBrainFiles(agentID) as Promise<BrainFiles>;
}

export function saveAgentBrainFile(agentID: string, fileKey: string, content: string, summary: string): Promise<void> {
  return _SaveAgentBrainFile(agentID, fileKey, content, summary);
}

export function initFirstRun(): Promise<FirstRunResult> {
  return _InitFirstRun() as Promise<FirstRunResult>;
}

// --- Event subscriptions -----------------------------------------------

type MessageHandler = (msg: KotuiMessage) => void;
type HeartbeatHandler = (hb: HeartbeatState) => void;
type ErrorHandler = (err: { error: string }) => void;
type ApprovalHandler = (approvals: Approval[]) => void;
type AgentsHandler = (agents: AgentInfo[]) => void;

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

export function onAgents(handler: AgentsHandler): () => void {
  return Events.On('kotui:agents', (ev) => handler(ev.data as AgentInfo[]));
}

export function onChannelBusy(handler: (busy: boolean) => void): () => void {
  return Events.On('kotui:channel_busy', (ev) => handler(ev.data as boolean));
}

export function onChannelStream(handler: (payload: { conversation_id: string; chunk: string }) => void): () => void {
  return Events.On('kotui:channel_stream', (ev) => handler(ev.data as { conversation_id: string; chunk: string }));
}

export type BrainUpdatePayload = {
  agent_id: string;
  file: string;
  summary: string;
  conv_id: string;
  message: KotuiMessage;
};

export function onBrainUpdate(handler: (payload: BrainUpdatePayload) => void): () => void {
  return Events.On('kotui:brain_update', (ev) => handler(ev.data as BrainUpdatePayload));
}

export function onFileWritten(handler: (payload: { path: string }) => void): () => void {
  return Events.On('kotui:file_written', (ev) => handler(ev.data as { path: string }));
}

export function listSandboxFiles(): Promise<FileEntry[]> {
  return _ListSandboxFiles() as Promise<FileEntry[]>;
}

export function readSandboxFile(relPath: string): Promise<string> {
  return _ReadSandboxFile(relPath);
}
