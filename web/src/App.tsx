// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import { Sessions } from "./Sessions";
import { SessionInspector } from "./SessionInspector";
import { Requests } from "./Requests";
import { RequestInspector } from "./RequestInspector";

type Route =
  | { view: "sessions"; params: URLSearchParams }
  | { view: "session"; id: number; params: URLSearchParams }
  | { view: "requests"; params: URLSearchParams }
  | { view: "request"; id: number };

function readRoute(): Route {
  const raw = window.location.hash.replace(/^#/, "");
  const [path, query = ""] = raw.split("?");
  const params = new URLSearchParams(query);
  if (path === "/requests") return { view: "requests", params };
  const requestMatch = path.match(/^\/request\/(\d+)/);
  if (requestMatch) return { view: "request", id: Number(requestMatch[1]) };
  const sessionMatch = path.match(/^\/session\/(\d+)/);
  if (sessionMatch) return { view: "session", id: Number(sessionMatch[1]), params };
  return { view: "sessions", params };
}

export default function App() {
  const [route, setRoute] = useState<Route>(readRoute);
  const [navKey, setNavKey] = useState(0);
  const [refreshKey, setRefreshKey] = useState(0);
  const [newCaptures, setNewCaptures] = useState(0);
  const baselineTotal = useRef<number | null>(null);
  // Remember the last list URL (including page, filters and sort) so an
  // inspector can return to exactly where the investigation started.
  const listHashes = useRef({ sessions: "#/", requests: "#/requests" });
  // Request IDs in the current Requests list order, used for prev/next
  // navigation inside the request inspector.
  const requestIdsRef = useRef<number[]>([]);

  useEffect(() => {
    const onHash = () => {
      setRoute(readRoute());
      setNavKey((v) => v + 1);
    };
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  function navigate(hash: string) {
    if (window.location.hash === hash) {
      setRoute(readRoute());
      return;
    }
    window.location.hash = hash;
    setRoute(readRoute());
  }

  function rememberListHash(view: "sessions" | "requests", hash: string) {
    listHashes.current[view] = hash;
  }

  function openSession(id: number) {
    navigate(`#/session/${id}`);
  }

  function openRequest(id: number) {
    navigate(`#/request/${id}`);
  }

  function openSessions() {
    navigate(listHashes.current.sessions);
  }

  function openRequests() {
    navigate(listHashes.current.requests);
  }

  function refresh() {
    setRefreshKey((v) => v + 1);
    setNewCaptures(0);
    api
      .requests({ limit: 1 })
      .then((r) => {
        baselineTotal.current = r.total;
      })
      .catch(() => {
        // Ignore polling errors; the manual refresh still works.
      });
  }

  useEffect(() => {
    let cancelled = false;
    async function poll() {
      try {
        const result = await api.requests({ limit: 1 });
        if (cancelled) return;
        if (baselineTotal.current === null) {
          baselineTotal.current = result.total;
        } else if (result.total > baselineTotal.current) {
          setNewCaptures(result.total - baselineTotal.current);
        }
      } catch {
        // Ignore polling errors.
      }
    }
    poll();
    const timer = window.setInterval(poll, 20_000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, []);

  // Navigate chronologically, independent of the current list sort. The list
  // is newest-first by default, so using its order would make Prev move to a
  // higher request ID.
  const requestIds = [...requestIdsRef.current].sort((a, b) => a - b);
  const requestIndex = route.view === "request" ? requestIds.indexOf(route.id) : -1;
  const prevRequestId = requestIndex > 0 ? requestIds[requestIndex - 1] : undefined;
  const nextRequestId =
    requestIndex >= 0 && requestIndex < requestIds.length - 1 ? requestIds[requestIndex + 1] : undefined;

  return (
    <div className="app-shell">
      <header className="topbar">
        <button className="brand" onClick={openSessions}>
          <span className="brand-mark">◈</span>
          <span>dumbllmproxy</span>
        </button>
        <span className="topbar-separator" />
        <nav className="topnav">
          <button
            className={route.view === "sessions" || route.view === "session" ? "active" : ""}
            onClick={openSessions}
          >
            Sessions
          </button>
          <button
            className={route.view === "requests" || route.view === "request" ? "active" : ""}
            onClick={openRequests}
          >
            Requests
          </button>
        </nav>
        <span className="muted small">local LLM request capture</span>
        <div className="topbar-right">
          {newCaptures > 0 && (
            <button className="new-captures" onClick={refresh} title="New captures are available">
              {newCaptures} new · refresh
            </button>
          )}
          <button className="secondary" onClick={refresh}>Refresh</button>
        </div>
      </header>

      {route.view === "sessions" && (
        <Sessions
          key={`sessions-${navKey}`}
          initialParams={route.params}
          refreshKey={refreshKey}
          onSelect={openSession}
          onListHash={(hash) => rememberListHash("sessions", hash)}
        />
      )}
      {route.view === "requests" && (
        <Requests
          key={`requests-${navKey}`}
          initialParams={route.params}
          refreshKey={refreshKey}
          onOpenRequest={openRequest}
          onOpenSession={openSession}
          onListHash={(hash) => rememberListHash("requests", hash)}
          onListData={(ids) => {
            requestIdsRef.current = ids;
          }}
        />
      )}
      {route.view === "request" && (
        <RequestInspector
          requestId={route.id}
          refreshKey={refreshKey}
          onBack={openRequests}
          onOpenSession={openSession}
          prevRequestId={prevRequestId}
          nextRequestId={nextRequestId}
          onNavigateRequest={openRequest}
        />
      )}
      {route.view === "session" && (
        <SessionInspector
          sessionId={route.id}
          refreshKey={refreshKey}
          onBack={openSessions}
          onOpenRequest={openRequest}
          initialNodeId={route.params.get("node") ?? undefined}
        />
      )}
    </div>
  );
}
