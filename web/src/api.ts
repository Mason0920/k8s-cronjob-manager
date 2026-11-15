import type { AppConfig, CronJobDetail, CronJobSummary, JobSummary, LogResult } from './types';

const TOKEN_KEY = 'cronjob-manager-token';

// Fired when the API answers 401 so the app can ask for a token.
export const AUTH_REQUIRED_EVENT = 'cronjob-manager:auth-required';

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

// The token lives in sessionStorage so it is gone when the tab closes.
export function getToken(): string {
  return sessionStorage.getItem(TOKEN_KEY) ?? '';
}

export function setToken(token: string): void {
  if (token) {
    sessionStorage.setItem(TOKEN_KEY, token);
  } else {
    sessionStorage.removeItem(TOKEN_KEY);
  }
}

async function request<T>(method: 'GET' | 'POST', path: string): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  const token = getToken();
  if (token) headers.Authorization = `Bearer ${token}`;

  const res = await fetch(path, { method, headers });
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body && typeof body.error === 'string') message = body.error;
    } catch {
      // body was not JSON; keep the status line
    }
    if (res.status === 401) window.dispatchEvent(new Event(AUTH_REQUIRED_EVENT));
    throw new ApiError(res.status, message);
  }
  return (await res.json()) as T;
}

const enc = encodeURIComponent;
const cronJobPath = (ns: string, name: string) => `/api/v1/namespaces/${enc(ns)}/cronjobs/${enc(name)}`;

export interface LogQuery {
  pod?: string;
  container?: string;
  tailLines?: number;
}

export const api = {
  config: () => request<AppConfig>('GET', '/api/v1/config'),

  listCronJobs: async (namespace?: string) => {
    const q = namespace ? `?namespace=${enc(namespace)}` : '';
    return (await request<{ items: CronJobSummary[] }>('GET', `/api/v1/cronjobs${q}`)).items;
  },

  recentJobs: async (limit = 20) =>
    (await request<{ items: JobSummary[] }>('GET', `/api/v1/jobs?limit=${limit}`)).items,

  getCronJob: (ns: string, name: string) => request<CronJobDetail>('GET', cronJobPath(ns, name)),

  runNow: (ns: string, name: string) => request<JobSummary>('POST', `${cronJobPath(ns, name)}/run`),

  suspend: (ns: string, name: string) => request<CronJobDetail>('POST', `${cronJobPath(ns, name)}/suspend`),

  resume: (ns: string, name: string) => request<CronJobDetail>('POST', `${cronJobPath(ns, name)}/resume`),

  jobs: async (ns: string, name: string, limit = 20) =>
    (await request<{ items: JobSummary[] }>('GET', `${cronJobPath(ns, name)}/jobs?limit=${limit}`)).items,

  logs: (ns: string, name: string, job: string, q: LogQuery = {}) => {
    const params = new URLSearchParams();
    if (q.pod) params.set('pod', q.pod);
    if (q.container) params.set('container', q.container);
    if (q.tailLines) params.set('tailLines', String(q.tailLines));
    const qs = params.toString();
    return request<LogResult>('GET', `${cronJobPath(ns, name)}/jobs/${enc(job)}/logs${qs ? `?${qs}` : ''}`);
  },
};
