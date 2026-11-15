import type { ReactNode } from 'react';
import { Link, useLocation, useSearchParams } from 'react-router-dom';
import { useAppConfig } from '../config';

export default function Layout({ children }: { children: ReactNode }) {
  const { namespaces, readOnly, labelSelector } = useAppConfig();
  const location = useLocation();
  const [params] = useSearchParams();
  const currentNs = params.get('namespace') ?? '';

  const onList = location.pathname === '/cronjobs';

  return (
    <div className="layout">
      <nav className="sidebar">
        <div className="sidebar-header">
          <h2 className="sidebar-title">CronJob Manager</h2>
        </div>

        <ul className="nav-menu">
          <li>
            <Link to="/" className={`nav-link ${location.pathname === '/' ? 'active' : ''}`}>
              대시보드
            </Link>
          </li>
          <li>
            <Link to="/cronjobs" className={`nav-link ${onList && !currentNs ? 'active' : ''}`}>
              전체 CronJob
            </Link>
          </li>
        </ul>

        <div className="nav-section">
          <div className="nav-section-title">네임스페이스</div>
          <ul className="nav-menu">
            {namespaces.map((ns) => (
              <li key={ns}>
                <Link
                  to={`/cronjobs?namespace=${encodeURIComponent(ns)}`}
                  className={`nav-link small ${onList && currentNs === ns ? 'active' : ''}`}
                >
                  {ns}
                </Link>
              </li>
            ))}
          </ul>
        </div>

        <div className="sidebar-footer">
          <div>
            모드:{' '}
            <span className={`status-badge ${readOnly ? 'status-warning' : 'status-success'}`}>
              {readOnly ? '읽기 전용' : '실행 가능'}
            </span>
          </div>
          {labelSelector && (
            <div>
              라벨 셀렉터: <code className="inline">{labelSelector}</code>
            </div>
          )}
        </div>
      </nav>

      <main className="main-content">{children}</main>
    </div>
  );
}
