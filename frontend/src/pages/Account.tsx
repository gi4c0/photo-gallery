import { useEffect, useState, type FormEvent } from 'react';
import { api, type User } from '../api';
import { useAuth, useSettings, useToast } from '../app';
import PasswordInput from '../PasswordInput';

export default function Account() {
  const { user } = useAuth();
  return (
    <main className="page page-narrow">
      <h1 className="h1 mb-20">Account</h1>
      <div className="stack-16">
        <PasswordPanel />
        {user?.isAdmin && <StudioPanel />}
        {user?.isAdmin && <UsersPanel me={user} />}
      </div>
    </main>
  );
}

function PasswordPanel() {
  const toast = useToast();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await api.changePassword(current, next);
      setCurrent('');
      setNext('');
      toast('Password changed');
    } catch (err) {
      setError((err as Error).message);
    }
    setBusy(false);
  };

  return (
    <form className="panel" onSubmit={submit}>
      <h2 className="panel-title">Change password</h2>
      <label className="field">
        Current password
        <PasswordInput value={current} onChange={(e) => setCurrent(e.target.value)}
          autoComplete="current-password" required />
      </label>
      <label className="field">
        New password
        <PasswordInput value={next} onChange={(e) => setNext(e.target.value)}
          autoComplete="new-password" minLength={8} required />
      </label>
      <p className="hint">8 to 72 characters.</p>
      {error && <div role="alert" className="error">{error}</div>}
      <button type="submit" className="btn btn-primary self-start" disabled={busy}>Change password</button>
    </form>
  );
}

function StudioPanel() {
  const { settings, setSettings } = useSettings();
  const toast = useToast();
  const [name, setName] = useState(settings.studioName);
  const [color, setColor] = useState(settings.accentColor);
  const [error, setError] = useState('');

  useEffect(() => {
    setName(settings.studioName);
    setColor(settings.accentColor);
  }, [settings]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError('');
    try {
      setSettings(await api.updateSettings({ studioName: name.trim(), accentColor: color.toUpperCase() }));
      toast('Studio settings saved');
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <form className="panel" onSubmit={submit}>
      <h2 className="panel-title">Studio</h2>
      <label className="field">
        Studio name
        <input className="input" type="text" value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      <label className="field">
        Accent colour
        <div className="row gap-8">
          <input type="color" className="color-input" value={color} onChange={(e) => setColor(e.target.value)} />
          <span className="muted small">{color.toUpperCase()}</span>
        </div>
      </label>
      <p className="hint">Shown on the sign-in page, the studio and every client gallery.</p>
      {error && <div role="alert" className="error">{error}</div>}
      <button type="submit" className="btn btn-primary self-start">Save</button>
    </form>
  );
}

function UsersPanel({ me }: { me: User }) {
  const toast = useToast();
  const [users, setUsers] = useState<User[]>([]);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [isAdmin, setIsAdmin] = useState(false);
  const [error, setError] = useState('');
  const [confirmId, setConfirmId] = useState<string | null>(null);

  useEffect(() => {
    api.users().then(setUsers, (e: Error) => toast(e.message));
  }, [toast]);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setError('');
    try {
      const u = await api.createUser(email.trim(), password, isAdmin);
      setUsers((us) => [...us, u]);
      setEmail('');
      setPassword('');
      setIsAdmin(false);
      toast(`Account created for ${u.email}`);
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const remove = async (u: User) => {
    try {
      await api.deleteUser(u.id);
      setUsers((us) => us.filter((x) => x.id !== u.id));
      setConfirmId(null);
      toast(`${u.email} deleted`);
    } catch (err) {
      toast((err as Error).message);
    }
  };

  return (
    <section className="panel">
      <h2 className="panel-title">Users</h2>
      <ul className="user-list">
        {users.map((u) => (
          <li key={u.id} className="user-row">
            <span className="user-email">{u.email}{u.isAdmin && <span className="badge">Admin</span>}</span>
            {u.id === me.id ? (
              <span className="muted small">You</span>
            ) : confirmId === u.id ? (
              <span className="row gap-8 wrap">
                <span className="small">Delete with all their galleries?</span>
                <button type="button" className="btn btn-danger btn-sm" onClick={() => remove(u)}>Delete</button>
                <button type="button" className="btn btn-outline btn-sm" onClick={() => setConfirmId(null)}>Cancel</button>
              </span>
            ) : (
              <button type="button" className="text-btn danger" onClick={() => setConfirmId(u.id)}
                aria-label={`Delete ${u.email}`}>Delete</button>
            )}
          </li>
        ))}
      </ul>
      <form className="stack-14 add-user" onSubmit={create}>
        <h3 className="panel-sub">Add a user</h3>
        <label className="field">
          Email
          <input className="input" type="email" value={email} onChange={(e) => setEmail(e.target.value)}
            autoComplete="off" required />
        </label>
        <label className="field">
          Password
          <PasswordInput value={password} onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password" minLength={8} required />
        </label>
        <label className="check">
          <input type="checkbox" checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} />
          Admin (can manage users and studio settings)
        </label>
        {error && <div role="alert" className="error">{error}</div>}
        <button type="submit" className="btn btn-primary self-start">Add user</button>
      </form>
    </section>
  );
}
