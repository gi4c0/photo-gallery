import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { api, UNAUTHORIZED_EVENT, type Settings, type User } from './api';

// ---------- Studio branding ----------

interface SettingsCtx {
  settings: Settings;
  setSettings: (s: Settings) => void;
}

const DEFAULT_SETTINGS: Settings = { studioName: '', accentColor: '#1C1B19' };
const SettingsContext = createContext<SettingsCtx>({ settings: DEFAULT_SETTINGS, setSettings: () => {} });

export function SettingsProvider({ children }: { children: ReactNode }) {
  const [settings, setSettings] = useState<Settings>(DEFAULT_SETTINGS);
  useEffect(() => {
    api.settings().then(setSettings, () => {});
  }, []);
  useEffect(() => {
    document.documentElement.style.setProperty('--accent', settings.accentColor || DEFAULT_SETTINGS.accentColor);
  }, [settings.accentColor]);
  return <SettingsContext.Provider value={{ settings, setSettings }}>{children}</SettingsContext.Provider>;
}

export const useSettings = () => useContext(SettingsContext);

export function useStudioName(): string {
  return useSettings().settings.studioName || 'Studio';
}

// ---------- Signed-in user ----------

interface AuthCtx {
  // undefined while the session check is in flight, null when signed out.
  user: User | null | undefined;
  setUser: (u: User | null) => void;
}

const AuthContext = createContext<AuthCtx>({ user: undefined, setUser: () => {} });

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null | undefined>(undefined);
  useEffect(() => {
    api.me().then(setUser, () => setUser(null));
    const onUnauthorized = () => setUser(null);
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
  }, []);
  return <AuthContext.Provider value={{ user, setUser }}>{children}</AuthContext.Provider>;
}

export const useAuth = () => useContext(AuthContext);

// ---------- Toast ----------

const ToastContext = createContext<(msg: string) => void>(() => {});

export function ToastProvider({ children }: { children: ReactNode }) {
  const [msg, setMsg] = useState('');
  const timer = useRef<number | undefined>(undefined);
  const flash = useCallback((m: string) => {
    window.clearTimeout(timer.current);
    setMsg(m);
    timer.current = window.setTimeout(() => setMsg(''), 3000);
  }, []);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return (
    <ToastContext.Provider value={flash}>
      {children}
      <div role="status" aria-live="polite" className="toast-region">
        {msg && <div className="toast">{msg}</div>}
      </div>
    </ToastContext.Provider>
  );
}

export const useToast = () => useContext(ToastContext);
