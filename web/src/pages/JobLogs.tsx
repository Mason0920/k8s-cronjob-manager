import { useEffect, useRef, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { api } from '../api';
import { useInterval, useLoader } from '../hooks';

const TAIL_CHOICES = [100, 500, 1000, 5000];

export default function JobLogs() {
  const { namespace = '', name = '', job = '' } = useParams();
  const [params, setParams] = useSearchParams();
  const pod = params.get('pod') ?? '';
  const container = params.get('container') ?? '';
  const tailLines = Number(params.get('tailLines') ?? '500') || 500;
  const [follow, setFollow] = useState(false);
  const bottom = useRef<HTMLDivElement>(null);

  const logs = useLoader(
    () => api.logs(namespace, name, job, { pod, container, tailLines }),
    [namespace, name, job, pod, container, tailLines],
  );
  useInterval(logs.reload, 5000, follow);

  useEffect(() => {
    if (follow) bottom.current?.scrollIntoView({ block: 'end' });
  }, [logs.data, follow]);

  const update = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    // A different pod may not have the same containers.
    if (key === 'pod') next.delete('container');
    setParams(next, { replace: true });
  };

  const download = () => {
    if (!logs.data) return;
    const blob = new Blob([logs.data.log], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${logs.data.pod || job}-${logs.data.container || 'log'}.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const d = logs.data;

  return (
    <div>
      <div className="page-header">
        <Link to={`/cronjobs/${namespace}/${name}`} className="back-link">
          &larr; {name}
        </Link>
        <h1 className="page-title">로그</h1>
        <p className="page-subtitle">
          {namespace} / {job}
        </p>
      </div>

      <div className="card">
        <div className="toolbar">
          <div className="form-group">
            <label className="form-label" htmlFor="pod">
              Pod
            </label>
            <select id="pod" className="form-select" value={d?.pod ?? ''} onChange={(e) => update('pod', e.target.value)}>
              {(d?.pods ?? []).map((p, i) => (
                <option key={p} value={p}>
                  {p}
                  {i === 0 ? ' (최신)' : ''}
                </option>
              ))}
            </select>
          </div>
          <div className="form-group">
            <label className="form-label" htmlFor="container">
              컨테이너
            </label>
            <select
              id="container"
              className="form-select"
              value={d?.container ?? ''}
              onChange={(e) => update('container', e.target.value)}
            >
              {(d?.containers ?? []).map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </div>
          <div className="form-group">
            <label className="form-label" htmlFor="tail">
              마지막 줄 수
            </label>
            <select
              id="tail"
              className="form-select"
              value={tailLines}
              onChange={(e) => update('tailLines', e.target.value)}
            >
              {TAIL_CHOICES.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </div>
          <div className="actions">
            <button className="btn btn-outline" onClick={logs.reload} disabled={logs.loading}>
              새로고침
            </button>
            <button className={`btn ${follow ? 'btn-primary' : 'btn-outline'}`} onClick={() => setFollow((f) => !f)}>
              {follow ? '자동 새로고침 끄기' : '5초마다 새로고침'}
            </button>
            <button className="btn btn-outline" onClick={download} disabled={!d?.log}>
              다운로드
            </button>
          </div>
        </div>

        {logs.error && <div className="error">{logs.error}</div>}
        {d?.message && <div className="notice">{d.message}</div>}
        {d?.truncated && (
          <div className="notice">
            응답 크기 상한({Math.round(d.limitBytes / 1024)} KiB)에 걸려 앞부분만 표시합니다. 줄 수를 줄이거나 kubectl logs로 확인하세요.
          </div>
        )}

        {logs.loading && !d ? (
          <div className="loading">불러오는 중...</div>
        ) : (
          <div className="log-container">
            {d?.log || '로그가 없습니다.'}
            <div ref={bottom} />
          </div>
        )}
      </div>
    </div>
  );
}
