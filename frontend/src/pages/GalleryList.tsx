import { useEffect, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router';
import { api, type Gallery } from '../api';
import { useToast } from '../app';
import { fmtDate, lastOpenDay, plural, previewLastOpenDay, todayISO } from '../format';
import { CloseIcon, PlusIcon } from '../icons';

export function statusLabel(g: Gallery): string {
  if (!g.downloadsOpen) return 'Downloads expired';
  return g.expiresOn ? `Open until ${lastOpenDay(g.expiresOn)}` : 'Open · no limit';
}

export default function GalleryList() {
  const [galleries, setGalleries] = useState<Gallery[] | null>(null);
  const [modal, setModal] = useState(false);
  const toast = useToast();

  useEffect(() => {
    api.galleries().then(setGalleries, (e: Error) => toast(e.message));
  }, [toast]);

  const photoTotal = galleries?.reduce((n, g) => n + g.photoCount, 0) ?? 0;

  return (
    <main className="page page-list">
      <div className="page-head">
        <div>
          <h1 className="h1">Client galleries</h1>
          {galleries && (
            <p className="muted sub">{plural(galleries.length, 'gallery', 'galleries')} · {plural(photoTotal, 'photo')}</p>
          )}
        </div>
        <button type="button" className="btn btn-primary btn-lg" onClick={() => setModal(true)}>
          <PlusIcon /> New gallery
        </button>
      </div>

      {galleries?.length === 0 && (
        <div className="empty">No galleries yet. Create one to start uploading photos.</div>
      )}

      <div className="card-grid">
        {galleries?.map((g) => (
          <Link key={g.id} to={`/galleries/${g.id}`} className="gallery-card">
            <div className="card-cover">
              {g.coverThumbUrl && <img src={g.coverThumbUrl} alt="" />}
              <span className={g.downloadsOpen ? 'chip' : 'chip chip-dark'}>{statusLabel(g)}</span>
            </div>
            <div>
              <div className="card-name">{g.name}</div>
              <div className="muted card-meta">
                {g.shotDate ? fmtDate(g.shotDate) + ' · ' : ''}{plural(g.photoCount, 'photo')}
              </div>
            </div>
          </Link>
        ))}
      </div>

      {modal && <NewGalleryModal onClose={() => setModal(false)} />}
    </main>
  );
}

const CREATE_MONTHS = [1, 2, 3, 6, 9, 12, 0];

function NewGalleryModal({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate();
  const [name, setName] = useState('');
  const [shotDate, setShotDate] = useState(todayISO());
  const [months, setMonths] = useState(3);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim()) {
      setError('Give the gallery a name.');
      return;
    }
    setBusy(true);
    try {
      const g = await api.createGallery(name.trim(), shotDate, months);
      navigate(`/galleries/${g.id}`);
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  return (
    <div className="backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <form role="dialog" aria-modal="true" aria-labelledby="new-gallery-title" className="modal" onSubmit={submit} noValidate>
        <div className="row between">
          <h2 id="new-gallery-title" className="h2">New gallery</h2>
          <button type="button" className="icon-btn" onClick={onClose} aria-label="Close"><CloseIcon /></button>
        </div>
        <label className="field">
          Client / gallery name
          <input className="input" type="text" value={name} onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Maria & João" maxLength={200} autoFocus />
        </label>
        <label className="field">
          Photoshoot date
          <input className="input" type="date" value={shotDate} onChange={(e) => setShotDate(e.target.value)} />
        </label>
        <label className="field">
          Download access
          <select className="input" value={months} onChange={(e) => setMonths(Number(e.target.value))}>
            {CREATE_MONTHS.map((m) => (
              <option key={m} value={m}>{m ? plural(m, 'month') : 'No limit'}</option>
            ))}
          </select>
        </label>
        <p className="hint">
          {months
            ? `Your client can download until ${previewLastOpenDay(months)}.`
            : 'Your client can download with no time limit.'}{' '}
          You can change this later.
        </p>
        {error && <div role="alert" className="error">{error}</div>}
        <button type="submit" className="btn btn-primary btn-xl" disabled={busy}>Create gallery</button>
      </form>
    </div>
  );
}
