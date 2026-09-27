import { useEffect, useRef, useState } from "react";
import type { LiveSnapshot } from "../types";

type ConnState = "connecting" | "live" | "reconnecting";

/**
 * Subscribes to the FlagForge /ws stream and returns the latest snapshot plus
 * a connection status. Auto-reconnects on close so killing/restarting a node
 * (the whole point of the demo) recovers seamlessly.
 */
export function useLiveSnapshot(): { snapshot: LiveSnapshot | null; conn: ConnState } {
  const [snapshot, setSnapshot] = useState<LiveSnapshot | null>(null);
  const [conn, setConn] = useState<ConnState>("connecting");
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    let closed = false;

    const connect = () => {
      const proto = location.protocol === "https:" ? "wss" : "ws";
      const ws = new WebSocket(`${proto}://${location.host}/ws`);
      wsRef.current = ws;

      ws.onopen = () => setConn("live");
      ws.onmessage = (ev) => {
        try {
          setSnapshot(JSON.parse(ev.data) as LiveSnapshot);
        } catch {
          /* ignore malformed frame */
        }
      };
      ws.onclose = () => {
        if (closed) return;
        setConn("reconnecting");
        setTimeout(connect, 1000);
      };
    };

    connect();
    return () => {
      closed = true;
      wsRef.current?.close();
    };
  }, []);

  return { snapshot, conn };
}
