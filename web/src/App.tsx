import { Link, Route, Routes } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from './api';
import { useRealtime, useWsState } from './realtime';
import { Toaster } from './toast';
import ProjectsPage from './pages/ProjectsPage';
import ProjectPage from './pages/ProjectPage';
import IssuePage from './pages/IssuePage';

function WsIndicator() {
  const s = useWsState();
  const label = s === 'open' ? '实时已连接' : s === 'connecting' ? '连接中…' : '已断开，重连中';
  return (
    <span className={`ws-ind ${s}`} title="WebSocket">
      <span className="dot" /> {label}
    </span>
  );
}

export default function App() {
  const qc = useQueryClient();
  useRealtime(qc);
  const health = useQuery({ queryKey: qk.health, queryFn: api.health, staleTime: Infinity });

  return (
    <div className="app">
      <header className="topbar">
        <Link to="/" className="brand">
          xIssue
        </Link>
        <div id="crumbs" className="crumbs grow" />
        <WsIndicator />
        {health.data && <span className="dim small">v{health.data.version}</span>}
      </header>
      <main className="main">
        <Routes>
          <Route path="/" element={<ProjectsPage />} />
          <Route path="/projects/:id" element={<ProjectPage />} />
          <Route path="/issues/:id" element={<IssuePage />} />
          <Route path="*" element={<div className="page empty">页面不存在</div>} />
        </Routes>
      </main>
      <Toaster />
    </div>
  );
}
