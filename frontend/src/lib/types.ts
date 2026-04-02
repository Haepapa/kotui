// Shared TypeScript types mirroring the Go models and warroom service types.

export type MessageKind =
  | 'boss_command'
  | 'agent_message'
  | 'tool_call'
  | 'tool_result'
  | 'milestone'
  | 'system_event'
  | 'draft';

export type LogTier = 'summary' | 'raw';

export interface KotuiMessage {
  id: string;
  project_id: string;
  conversation_id: string;
  agent_id: string;
  kind: MessageKind;
  tier: LogTier;
  content: string;
  metadata: string;
  created_at: string;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  data_path: string;
  active: boolean;
  created_at: string;
}

export interface AgentInfo {
  id: string;
  name: string;
  role: 'lead' | 'specialist' | 'trial';
  status: 'idle' | 'working' | 'parked' | 'offline' | 'onboarded' | 'rejected';
  model: string;
}

export interface HeartbeatState {
  is_healthy: boolean;
  phase: string;
  breadcrumbs: string[];
  active_count: number;
  vram_profile: string;
  updated_at: string;
}

export type ViewMode = 'boss' | 'dev';
