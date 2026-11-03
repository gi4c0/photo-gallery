import { Link, Navigate, Outlet, useNavigate } from 'react-router';
import { api } from '../api';
import { useAuth, useStudioName } from '../app';

// Header and session gate for every photographer page.
export default function StudioLayout() {
  const { user, setUser } = useAuth();
  const studioName = useStudioName();
  const navigate = useNavigate();

  if (user === undefined) return null;
  if (user === null) return <Navigate to="/login" replace />;

  const logout = async () => {
    await api.logout().catch(() => {});
    setUser(null);
    navigate('/login', { replace: true });
  };

  return (
    <>
      <header className="studio-header">
        <Link to="/" className="brand brand-md">{studioName}</Link>
        <nav className="row gap-12 wrap">
          <span className="muted small">{user.email}</span>
          <Link to="/account" className="btn btn-outline btn-sm">Account</Link>
          <button type="button" className="btn btn-outline btn-sm" onClick={logout}>Log out</button>
        </nav>
      </header>
      <Outlet />
    </>
  );
}
