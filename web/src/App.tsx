import { useEffect, useState } from 'react';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { AUTH_REQUIRED_EVENT, api } from './api';
import { ConfigContext } from './config';
import { useLoader } from './hooks';
import Layout from './components/Layout';
import TokenPrompt from './components/TokenPrompt';
import Dashboard from './pages/Dashboard';
import CronJobList from './pages/CronJobList';
import CronJobDetail from './pages/CronJobDetail';
import JobLogs from './pages/JobLogs';
import './App.css';

export default function App() {
  const [needToken, setNeedToken] = useState(false);
  // Bumping session remounts the pages after a new token is entered.
  const [session, setSession] = useState(0);
  const config = useLoader(() => api.config(), [session]);

  useEffect(() => {
    const onAuth = () => setNeedToken(true);
    window.addEventListener(AUTH_REQUIRED_EVENT, onAuth);
    return () => window.removeEventListener(AUTH_REQUIRED_EVENT, onAuth);
  }, []);

  const onToken = () => {
    setNeedToken(false);
    setSession((s) => s + 1);
  };

  return (
    <BrowserRouter>
      {needToken && <TokenPrompt onSaved={onToken} />}
      {config.data ? (
        <ConfigContext.Provider value={config.data}>
          <Layout>
            <Routes key={session}>
              <Route path="/" element={<Dashboard />} />
              <Route path="/cronjobs" element={<CronJobList />} />
              <Route path="/cronjobs/:namespace/:name" element={<CronJobDetail />} />
              <Route path="/cronjobs/:namespace/:name/jobs/:job/logs" element={<JobLogs />} />
              <Route path="*" element={<div className="empty">페이지를 찾을 수 없습니다.</div>} />
            </Routes>
          </Layout>
        </ConfigContext.Provider>
      ) : config.loading ? (
        <div className="loading">설정을 불러오는 중...</div>
      ) : (
        !needToken && <div className="error" style={{ margin: '2rem' }}>설정을 불러오지 못했습니다: {config.error}</div>
      )}
    </BrowserRouter>
  );
}
