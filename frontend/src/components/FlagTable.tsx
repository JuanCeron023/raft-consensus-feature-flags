import type { Flag } from "../types";

interface Props {
  flags: Flag[];
  onError: (msg: string) => void;
}

export function FlagTable({ flags, onError }: Props) {
  const del = async (key: string) => {
    const res = await fetch(`/api/flags/${encodeURIComponent(key)}`, { method: "DELETE" });
    if (!res.ok) {
      try {
        const j = (await res.json()) as { leader?: string; error?: string };
        onError(j.leader ? `Not leader — delete on node ${j.leader}` : j.error ?? "request failed");
      } catch {
        onError("request failed");
      }
    }
  };

  return (
    <div className="overflow-hidden rounded-2xl border border-slate-800">
      <table className="w-full text-sm">
        <thead className="bg-slate-900/80 text-slate-400">
          <tr>
            <th className="text-left px-4 py-3 font-medium">Key</th>
            <th className="text-left px-4 py-3 font-medium">Status</th>
            <th className="text-left px-4 py-3 font-medium">Rollout</th>
            <th className="text-left px-4 py-3 font-medium">Updated</th>
            <th className="px-4 py-3" />
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-800">
          {flags.length === 0 ? (
            <tr>
              <td colSpan={5} className="px-4 py-8 text-center text-slate-500">
                No flags yet — propose one above.
              </td>
            </tr>
          ) : (
            flags.map((f) => (
              <tr key={f.key} className="hover:bg-slate-900/40">
                <td className="px-4 py-3 font-mono">{f.key}</td>
                <td className="px-4 py-3">
                  <span
                    className={`inline-flex items-center gap-1.5 text-xs px-2 py-0.5 rounded-full ${
                      f.enabled ? "bg-emerald-500/15 text-emerald-400" : "bg-slate-700/50 text-slate-400"
                    }`}
                  >
                    <span className={`h-1.5 w-1.5 rounded-full ${f.enabled ? "bg-emerald-400" : "bg-slate-500"}`} />
                    {f.enabled ? "on" : "off"}
                  </span>
                </td>
                <td className="px-4 py-3">
                  <div className="flex items-center gap-2">
                    <div className="w-24 h-1.5 rounded-full bg-slate-700 overflow-hidden">
                      <div className="h-full bg-cyan-400" style={{ width: `${f.rollout}%` }} />
                    </div>
                    <span className="text-xs text-slate-400 font-mono">{f.rollout}%</span>
                  </div>
                </td>
                <td className="px-4 py-3 text-xs text-slate-500 font-mono">
                  {new Date(f.updated_at).toLocaleTimeString()}
                </td>
                <td className="px-4 py-3 text-right">
                  <button onClick={() => del(f.key)} className="text-xs text-rose-400 hover:text-rose-300">
                    delete
                  </button>
                </td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}
