import { useState, type FormEvent } from 'react';
import { setToken } from '../api';

interface Props {
  onSaved: () => void;
}

// Shown when the API answers 401. The server checks the token; the UI only
// stores it in sessionStorage and sends it as a bearer token.
export default function TokenPrompt({ onSaved }: Props) {
  const [value, setValue] = useState('');

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setToken(value.trim());
    onSaved();
  };

  return (
    <div className="overlay" role="dialog" aria-modal="true" aria-labelledby="token-title">
      <form className="card" onSubmit={submit}>
        <div className="card-header">
          <h2 className="card-title" id="token-title">
            접근 토큰이 필요합니다
          </h2>
        </div>
        <p className="muted" style={{ marginBottom: '1rem' }}>
          서버에 AUTH_TOKEN이 설정되어 있습니다. 운영자에게 받은 토큰을 입력하세요. 토큰은 이 탭이 닫히면 지워집니다.
        </p>
        <div className="form-group">
          <label className="form-label" htmlFor="token">
            Bearer token
          </label>
          <input
            id="token"
            className="form-input"
            type="password"
            autoComplete="off"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            autoFocus
          />
        </div>
        <button type="submit" className="btn btn-primary" disabled={!value.trim()}>
          저장
        </button>
      </form>
    </div>
  );
}
