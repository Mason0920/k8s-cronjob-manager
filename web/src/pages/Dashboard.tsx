import { Link } from 'react-router-dom';
import { api } from '../api';
import StatusBadge from '../components/StatusBadge';
import { formatDateTime, formatDuration } from '../format';
import { useLoader } from '../hooks';

const DAY_MS = 24 * 60 * 60 * 1000;

export default function Dashboard() {
  const cronJobs = useLoader(() => api.listCronJobs(), []);
  const recent = useLoader(() => api.recentJobs(50), []);

  if (cronJobs.loading && !cronJobs.data) return <div className="loading">불러오는 중...</div>;
  if (cronJobs.error) return <div className="error">{cronJobs.error}</div>;

  const items = cronJobs.data ?? [];
  const jobs = recent.data ?? [];
  const since = Date.now() - DAY_MS;
  const stats = {
    total: items.length,
    suspended: items.filter((c) => c.suspend).length,
    running: jobs.filter((j) => j.status === 'Running').length,
    failed24h: jobs.filter((j) => j.status === 'Failed' && new Date(j.createdAt).getTime() >= since).length,
  };

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">대시보드</h1>
        <p className="page-subtitle">허용된 네임스페이스의 CronJob과 최근 실행 결과</p>
      </div>

      <div className="stats-grid">
        <div className="stat-card">
          <div className="stat-value" style={{ color: 'var(--primary-color)' }}>{stats.total}</div>
          <div className="stat-label">CronJob</div>
        </div>
        <div className="stat-card">
          <div className="stat-value" style={{ color: 'var(--warning-color)' }}>{stats.suspended}</div>
          <div className="stat-label">일시정지</div>
        </div>
        <div className="stat-card">
          <div className="stat-value" style={{ color: 'var(--success-color)' }}>{stats.running}</div>
          <div className="stat-label">실행 중 Job</div>
        </div>
        <div className="stat-card">
          <div className="stat-value" style={{ color: 'var(--error-color)' }}>{stats.failed24h}</div>
          <div className="stat-label">최근 24시간 실패</div>
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <h2 className="card-title">최근 실행</h2>
          <div className="actions">
            <button className="btn btn-outline" onClick={recent.reload}>
              새로고침
            </button>
            <Link to="/cronjobs" className="btn btn-outline">
              CronJob 목록
            </Link>
          </div>
        </div>

        {recent.error && <div className="error">{recent.error}</div>}
        {jobs.length === 0 ? (
          <p className="empty">최근 실행 기록이 없습니다.</p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Job</th>
                <th>CronJob</th>
                <th>상태</th>
                <th>시작</th>
                <th>소요 시간</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {jobs.slice(0, 15).map((j) => (
                <tr key={`${j.namespace}/${j.name}`}>
                  <td>
                    <code className="inline">{j.name}</code>
                    {j.manual && <span className="tag">수동</span>}
                  </td>
                  <td>
                    <Link className="name-link" to={`/cronjobs/${j.namespace}/${j.cronJob}`}>
                      {j.cronJob}
                    </Link>
                    <div className="muted">{j.namespace}</div>
                  </td>
                  <td>
                    <StatusBadge status={j.status} />
                  </td>
                  <td>{formatDateTime(j.startTime ?? j.createdAt)}</td>
                  <td>{formatDuration(j.startTime, j.completionTime)}</td>
                  <td>
                    <Link className="btn btn-outline btn-sm" to={`/cronjobs/${j.namespace}/${j.cronJob}/jobs/${j.name}/logs`}>
                      로그
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
