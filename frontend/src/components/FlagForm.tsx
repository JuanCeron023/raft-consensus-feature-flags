import { useState } from "react";

interface Props {
  onError: (msg: string) => void;
}

/** Form to propose a new/updated flag to the Raft leader. */
export function FlagForm({ onError }: Props) {
  const [key, setKey] = useState("");
  const [rollout, setRollout] = useState(100);
  const [enabled, setEnabled] = useState(true);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const res = await fetch("/api/flags", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ key, rollout, enabled }),
    });
    if (!res.ok) {
      try {
        const j = (await res.json()) as { leader?: string; error?: string };
        onError(j.leader ? `Not leader — write to node ${j.leader}` : j.error ?? "request failed");
      } catch {
        onError("request failed");
      }
    } else {
      setKey("");
    }
  };

  return (
    <form onSubmit={submit} className="grid md:grid-cols-5 gap-3 items-end">
      <div className="md:col-span-2">
        <label className="block text-xs text-slate-400 mb-1">Key</label>
        <input
          value={key}
          onChange={(e) => setKey(e.target.value)}
          required
          placeholder="checkout.new_flow"
          className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-emerald-500"
        />
      </div>
      <div>
        <label className="block text-xs text-slate-400 mb-1">Rollout %</label>
        <input
          type="number"
          min={0}
          max={100}
          value={rollout}
          onChange={(e) => setRollout(Number(e.target.value))}
          className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-emerald-500"
        />
      </div>
      <div className="flex items-center gap-2 pb-2">
        <input
          type="checkbox"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
          className="h-4 w-4 accent-emerald-500"
        />
        <label className="text-sm text-slate-300">Enabled</label>
      </div>
      <button className="bg-emerald-500 hover:bg-emerald-400 text-slate-900 font-medium rounded-lg px-4 py-2 text-sm transition">
        Propose
      </button>
    </form>
  );
}
