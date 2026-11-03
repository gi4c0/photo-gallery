import { useEffect, useState, type FormEvent } from 'react';
import { Navigate, useNavigate } from 'react-router';
import { api } from '../api';
import { useAuth, useSettings, useStudioName } from '../app';
import PasswordInput from '../PasswordInput';

// Login, or first-run sign-up while the server has no accounts yet.
export default function Login() {
  const { user, setUser } = useAuth();
  const { settings, setSettings } = useSettings();
  const studioName = useStudioName();
  const navigate = useNavigate();
  const [registerOpen, setRegisterOpen] = useState<boolean | null>(null);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [newStudioName, setNewStudioName] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api.registrationOpen().then((r) => setRegisterOpen(r.open), () => setRegisterOpen(false));
  }, []);

  if (user) return <Navigate to="/" replace />;
  if (user === undefined || registerOpen === null) return null;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!email.trim() || !password) {
      setError('Enter your email and password.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      if (registerOpen) {
        const u = await api.register(email.trim(), password, newStudioName.trim());
        if (newStudioName.trim()) setSettings({ ...settings, studioName: newStudioName.trim() });
        setUser(u);
      } else {
        setUser(await api.login(email.trim(), password));
      }
      navigate('/', { replace: true });
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  return (
    <div className="center-page">
      <form className="login-card" onSubmit={submit} noValidate>
        <div className="login-head">
          <div className="brand brand-lg">{registerOpen ? 'Welcome' : studioName}</div>
          <div className="muted">
            {registerOpen ? 'Create the admin account for your studio' : 'Sign in to manage client galleries'}
          </div>
        </div>
        {registerOpen && (
          <label className="field">
            Studio name
            <input className="input" type="text" value={newStudioName} onChange={(e) => setNewStudioName(e.target.value)}
              autoComplete="organization" />
          </label>
        )}
        <label className="field">
          Email
          <input className="input input-lg" type="email" value={email} onChange={(e) => setEmail(e.target.value)}
            autoComplete="username" required />
        </label>
        <label className="field">
          Password
          <PasswordInput className="input input-lg" value={password} onChange={(e) => setPassword(e.target.value)}
            autoComplete={registerOpen ? 'new-password' : 'current-password'} required
            minLength={registerOpen ? 8 : undefined} />
        </label>
        {registerOpen && <p className="hint">At least 8 characters.</p>}
        {error && <div role="alert" className="error">{error}</div>}
        <button type="submit" className="btn btn-primary btn-xl" disabled={busy}>
          {registerOpen ? 'Create account' : 'Sign in'}
        </button>
      </form>
    </div>
  );
}
