import type { NodeStatus, Role } from "../types";

const roleGradient: Record<Role, string> = {
  leader: "from-emerald-400 to-cyan-500",
  candidate: "from-amber-400 to-orange-500",
  follower: "from-slate-500 to-slate-600",
  unknown: "from-slate-600 to-slate-700",
};

function Card({ children, ring }: { children: React.ReactNode; ring?: boolean }) {
  return (
    <div className={`bg-slate-900/60 border border-slate-800 rounded-2xl p-4 ${ring ? "leader-ring" : ""}`}>
      {children}
    </div>
  );
}

export function NodePanel({ status }: { status: NodeStatus }) {
  return (
    <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
      <Card ring={status.role === "leader"}>
        <div className="text-xs text-slate-400">Node</div>
        <div className="text-lg font-semibold">{status.id}</div>
      </Card>
      <Card>
        <div className="text-xs text-slate-400">Role</div>
        <div
          className={`inline-flex mt-1 items-center gap-2 text-sm font-medium px-2.5 py-1 rounded-full bg-gradient-to-r ${roleGradient[status.role]} text-slate-900 capitalize`}
        >
          {status.role}
        </div>
      </Card>
      <Card>
        <div className="text-xs text-slate-400">Term</div>
        <div className="text-lg font-semibold font-mono">{status.term}</div>
      </Card>
      <Card>
        <div className="text-xs text-slate-400">Leader / Commit</div>
        <div className="text-sm font-mono">
          {status.leader_id || "—"} · idx {status.commit_index}
        </div>
      </Card>
    </div>
  );
}
