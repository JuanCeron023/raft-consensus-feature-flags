import { useEffect, useState } from "react";
import { useLiveSnapshot } from "./hooks/useLiveSnapshot";
import { NodePanel } from "./components/NodePanel";
import { FlagForm } from "./components/FlagForm";
import { FlagTable } from "./components/FlagTable";

export default function App() {
  const { snapshot, conn } = useLiveSnapshot();
  const [hint, setHint] = useState("");

  useEffect(() => {
    if (!hint) return;
    const t = setTimeout(() => setHint(""), 4000);
    return () => clearTimeout(t);
  }, [hint]);

  const connStyle =
    conn === "live"
      ? "bg-emerald-500/15 text-emerald-400 border-emerald-500/30"
      : "bg-slate-800 border-slate-700";

  return (
    <div className="max-w-6xl mx-auto px-6 py-8">
      <header className="flex items-center justify-between mb-8">
        <div className="flex items-center gap-3">
          <div className="h-10 w-10 rounded-xl bg-gradient-to-br from-emerald-400 to-cyan-500 grid place-items-center font-bold text-slate-900">
            FF
          </div>
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">FlagForge</h1>
            <p className="text-sm text-slate-400">
              Strongly-consistent feature flags on a from-scratch Raft cluster
            </p>
          </div>
        </div>
        <div className={`text-xs px-3 py-1.5 rounded-full border ${connStyle}`}>
          {conn === "live" ? "● live" : conn === "reconnecting" ? "○ reconnecting…" : "connecting…"}
        </div>
      </header>

      <section className="mb-8">
        <h2 className="text-sm uppercase tracking-widest text-slate-500 mb-3">This Node</h2>
        {snapshot ? (
          <NodePanel status={snapshot.status} />
        ) : (
          <div className="text-slate-500 text-sm">Waiting for node status…</div>
        )}
      </section>

      <section className="mb-8 bg-slate-900/60 border border-slate-800 rounded-2xl p-5">
        <h2 className="text-sm uppercase tracking-widest text-slate-500 mb-4">Create / Update Flag</h2>
        <FlagForm onError={setHint} />
        <p className="text-xs text-amber-400 mt-2 h-4">{hint}</p>
      </section>

      <section>
        <h2 className="text-sm uppercase tracking-widest text-slate-500 mb-3">
          Feature Flags (replicated state)
        </h2>
        <FlagTable flags={snapshot?.flags ?? []} onError={setHint} />
      </section>
    </div>
  );
}
