// Shared types mirroring the FlagForge Go API payloads.

export type Role = "leader" | "candidate" | "follower" | "unknown";

export interface NodeStatus {
  id: string;
  role: Role;
  term: number;
  leader_id: string;
  commit_index: number;
  last_applied: number;
  log_length: number;
  voted_for: string;
}

export interface Flag {
  key: string;
  enabled: boolean;
  rollout: number;
  description: string;
  updated_at: string;
}

// Payload pushed over the /ws WebSocket every 250ms.
export interface LiveSnapshot {
  status: NodeStatus;
  flags: Flag[];
}
