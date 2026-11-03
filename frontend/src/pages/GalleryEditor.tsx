import { useEffect, useRef, useState, type DragEvent } from 'react';
import { Link, useNavigate, useParams } from 'react-router';
import { api, uploadPhoto, type ApiError, type Gallery, type Photo } from '../api';
import { useToast } from '../app';
import { fmtDate, lastOpenDay, plural } from '../format';
import { BackIcon, EyeIcon, LeftIcon, LinkIcon, RightIcon, StarIcon, TrashIcon, UploadIcon } from '../icons';

interface Upload {
  key: string;
  file: File;
  progress: number;
  state: 'queued' | 'uploading' | 'failed';
  error?: string;
  retryable?: boolean;
}

const EDIT_MONTHS = [1, 3, 6, 12, 0];
const monthsLabel = (m: number) => (m === 0 ? 'No limit' : m === 12 ? '1 year' : plural(m, 'month'));

export default function GalleryEditor() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const toast = useToast();
  const [gallery, setGallery] = useState<Gallery | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [nameDraft, setNameDraft] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [dragId, setDragId] = useState<string | null>(null);
  const [overId, setOverId] = useState<string | null>(null);
  const [dropActive, setDropActive] = useState(false);
  const [uploads, setUploads] = useState<Upload[]>([]);
  const [copied, setCopied] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const uploadingRef = useRef(false);
  const refreshAfterUploads = useRef(false);
  const copyTimer = useRef<number | undefined>(undefined);

  const load = () =>
    api.gallery(id).then(setGallery, (e: ApiError) => (e.status === 404 ? setNotFound(true) : toast(e.message)));

  useEffect(() => {
    load();
    return () => window.clearTimeout(copyTimer.current);
  }, [id]);

  // Sync the name field only when a gallery is first loaded, so a server
  // response for another change never overwrites what the user is typing.
  useEffect(() => {
    if (gallery) setNameDraft(gallery.name);
  }, [gallery?.id]);

  // Upload queue: one file per request, in order, so new photos keep the
  // order they were picked in and a failure only affects its own file.
  useEffect(() => {
    if (!gallery || uploadingRef.current) return;
    const next = uploads.find((u) => u.state === 'queued');
    if (!next) {
      if (refreshAfterUploads.current) {
        refreshAfterUploads.current = false;
        load(); // picks up the server-chosen cover and final order
      }
      return;
    }
    uploadingRef.current = true;
    const patch = (fields: Partial<Upload>) =>
      setUploads((us) => us.map((u) => (u.key === next.key ? { ...u, ...fields } : u)));
    patch({ state: 'uploading', progress: 0, error: undefined });
    uploadPhoto(gallery.id, next.file, (progress) => patch({ progress })).then(
      (res) => {
        uploadingRef.current = false;
        if (res.photos.length) {
          refreshAfterUploads.current = true;
          setGallery((g) => g && {
            ...g,
            photos: [...(g.photos ?? []), ...res.photos],
            photoCount: g.photoCount + res.photos.length,
          });
        }
        if (res.rejected.length) patch({ state: 'failed', error: res.rejected[0].error, retryable: false });
        else setUploads((us) => us.filter((u) => u.key !== next.key));
      },
      (e: ApiError) => {
        uploadingRef.current = false;
        patch({ state: 'failed', error: e.message, retryable: e.status === 0 || e.status === 429 || e.status >= 500 });
      },
    );
  }, [uploads, gallery]);

  if (notFound) {
    return (
      <main className="page">
        <div className="empty">This gallery does not exist. <Link to="/">Back to all galleries</Link></div>
      </main>
    );
  }
  if (!gallery) return null;

  const photos = gallery.photos ?? [];
  const shareUrl = `${window.location.origin}/g/${gallery.slug}`;

  const run = async (call: () => Promise<Gallery>, done?: string) => {
    try {
      setGallery(await call());
      if (done) toast(done);
    } catch (e) {
      toast((e as Error).message);
    }
  };

  const addFiles = (list: FileList | null | undefined) => {
    const files = Array.from(list ?? []);
    if (!files.length) return;
    const stamp = Date.now();
    setUploads((us) => [
      ...us,
      ...files.map((file, i) => ({ key: `${stamp}-${i}`, file, progress: 0, state: 'queued' as const })),
    ]);
  };

  const reorder = (ids: string[]) => {
    const byId = new Map(photos.map((p) => [p.id, p]));
    setGallery({ ...gallery, photos: ids.map((pid) => byId.get(pid)!) });
    api.reorderPhotos(gallery.id, ids).then(setGallery, (e: Error) => {
      toast(e.message);
      load();
    });
  };
  const moveTo = (from: string, target: string) => {
    if (from === target) return;
    const ids = photos.map((p) => p.id).filter((pid) => pid !== from);
    const fromIdx = photos.findIndex((p) => p.id === from);
    const toIdx = ids.indexOf(target);
    ids.splice(fromIdx <= toIdx ? toIdx + 1 : toIdx, 0, from);
    reorder(ids);
  };
  const moveBy = (pid: string, delta: number) => {
    const ids = photos.map((p) => p.id);
    const i = ids.indexOf(pid);
    const j = i + delta;
    if (j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j], ids[i]];
    reorder(ids);
  };

  const removePhotos = (ids: string[]) => {
    setSelected(new Set());
    run(() => api.deletePhotos(gallery.id, ids), ids.length === 1 ? 'Photo deleted' : `${ids.length} photos deleted`);
  };

  const saveName = () => {
    const name = nameDraft.trim();
    if (name === gallery.name) return;
    if (!name) {
      setNameDraft(gallery.name);
      toast('Give the gallery a name.');
      return;
    }
    run(() => api.updateGallery(gallery.id, { name }), 'Name saved');
  };

  const copyLink = async () => {
    try {
      await navigator.clipboard.writeText(shareUrl);
      setCopied(true);
      window.clearTimeout(copyTimer.current);
      copyTimer.current = window.setTimeout(() => setCopied(false), 2000);
    } catch {
      toast('Copy failed. Select the link and copy it manually.');
    }
  };

  const rotateLink = () => {
    if (!window.confirm('Generate a new link? The current link stops working immediately.')) return;
    run(() => api.rotateLink(gallery.id), 'New link created. The old link no longer works.');
  };

  const deleteGallery = async () => {
    try {
      await api.deleteGallery(gallery.id);
      toast('Gallery deleted');
      navigate('/', { replace: true });
    } catch (e) {
      toast((e as Error).message);
    }
  };

  const selIds = photos.filter((p) => selected.has(p.id)).map((p) => p.id);
  const allSelected = photos.length > 0 && selIds.length === photos.length;
  const toggle = (pid: string) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(pid)) n.delete(pid);
      else n.add(pid);
      return n;
    });

  const onCardDrop = (e: DragEvent, p: Photo) => {
    e.preventDefault();
    e.stopPropagation();
    if (dragId) moveTo(dragId, p.id);
    else addFiles(e.dataTransfer.files);
    setDragId(null);
    setOverId(null);
    setDropActive(false);
  };

  const monthOptions = EDIT_MONTHS.includes(gallery.months) ? EDIT_MONTHS : [...EDIT_MONTHS.slice(0, -1), gallery.months, 0].sort((a, b) => (a || 999) - (b || 999));
  const expiryText = !gallery.expiresOn
    ? 'Your client can download photos with no time limit.'
    : gallery.downloadsOpen
      ? `Your client can download until ${lastOpenDay(gallery.expiresOn)} (counted from ${fmtDate(gallery.sharedOn)}).`
      : `Downloads closed on ${fmtDate(gallery.expiresOn)}. Your client can still view the photos.`;

  return (
    <main className="page page-edit">
      <div className="row between wrap gap-12 mb-20">
        <Link to="/" className="link-btn"><BackIcon /> All galleries</Link>
        <a href={`/g/${gallery.slug}`} target="_blank" rel="noopener noreferrer" className="btn btn-outline">
          <EyeIcon /> Preview as client
        </a>
      </div>

      <div className="edit-layout">
        <section className="photos-col">
          <div className="row between wrap gap-12 align-end">
            <div>
              <h1 className="h1-wide">{gallery.name}</h1>
              <p className="muted sub">{plural(photos.length, 'photo')} · drag photos to reorder</p>
            </div>
            {photos.length > 0 && (
              <button type="button" className="btn btn-outline btn-sm" onClick={() =>
                setSelected(allSelected ? new Set() : new Set(photos.map((p) => p.id)))}>
                {allSelected ? 'Deselect all' : 'Select all'}
              </button>
            )}
          </div>

          {selIds.length > 0 && (
            <div role="toolbar" aria-label="Selected photos" className="sel-bar">
              <span className="sel-count">{selIds.length} selected</span>
              <button type="button" className="btn btn-ghost-light btn-sm" onClick={() => setSelected(new Set())}>Clear</button>
              <button type="button" className="btn btn-white-danger btn-sm" onClick={() => removePhotos(selIds)}>
                <TrashIcon /> Delete selected
              </button>
            </div>
          )}

          <label
            className={'dropzone' + (dropActive ? ' active' : '') + (photos.length ? '' : ' tall')}
            onDragOver={(e) => { e.preventDefault(); if (!dragId) setDropActive(true); }}
            onDragLeave={() => setDropActive(false)}
            onDrop={(e) => { e.preventDefault(); setDropActive(false); if (!dragId) addFiles(e.dataTransfer.files); }}
          >
            <UploadIcon />
            <span className="dz-title">Drop photos here or <u>browse</u></span>
            <span className="muted small">JPG, PNG or HEIC · multiple files at once</span>
            <input type="file" className="visually-hidden" multiple accept="image/jpeg,image/png,image/heic,image/heif,.heic,.heif"
              onChange={(e) => { addFiles(e.target.files); e.target.value = ''; }} />
          </label>

          {uploads.length > 0 && (
            <ul className="upload-list" aria-label="Uploads">
              {uploads.map((u) => (
                <li key={u.key} className={'upload-item' + (u.state === 'failed' ? ' failed' : '')}>
                  <span className="upload-name">{u.file.name}</span>
                  {u.state === 'failed' ? (
                    <>
                      <span className="error small">{u.error}</span>
                      {u.retryable && (
                        <button type="button" className="text-btn" onClick={() =>
                          setUploads((us) => us.map((x) => (x.key === u.key ? { ...x, state: 'queued', progress: 0 } : x)))}>
                          Retry
                        </button>
                      )}
                      <button type="button" className="text-btn" onClick={() =>
                        setUploads((us) => us.filter((x) => x.key !== u.key))}>
                        Dismiss
                      </button>
                    </>
                  ) : (
                    <>
                      <progress max={1} value={u.state === 'uploading' ? u.progress : 0} aria-label={`Uploading ${u.file.name}`} />
                      <span className="muted small">
                        {u.state === 'queued' ? 'Waiting' : u.progress < 1 ? `${Math.round(u.progress * 100)}%` : 'Processing…'}
                      </span>
                    </>
                  )}
                </li>
              ))}
            </ul>
          )}

          <div className="photo-grid">
            {photos.map((p, i) => {
              const isCover = gallery.coverPhotoId === p.id;
              const ringed = selected.has(p.id) || (dragId !== null && overId === p.id && dragId !== p.id);
              return (
                <div key={p.id} className={'photo-card' + (ringed ? ' ringed' : '') + (dragId === p.id ? ' dragging' : '')}
                  draggable
                  onDragStart={(e) => { e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData('text/plain', p.id); setDragId(p.id); }}
                  onDragEnter={() => dragId && setOverId(p.id)}
                  onDragOver={(e) => e.preventDefault()}
                  onDrop={(e) => onCardDrop(e, p)}
                  onDragEnd={() => { setDragId(null); setOverId(null); }}
                >
                  <div className="photo-thumb">
                    <img src={p.thumbUrl} alt={p.name} draggable={false} loading="lazy" />
                    <label className="thumb-btn thumb-tl">
                      <input type="checkbox" checked={selected.has(p.id)} onChange={() => toggle(p.id)} aria-label={`Select ${p.name}`} />
                    </label>
                    <button type="button" className="thumb-btn thumb-tr" aria-pressed={isCover}
                      aria-label={isCover ? 'Cover photo' : `Set ${p.name} as cover`}
                      onClick={() => !isCover && run(() => api.setCover(gallery.id, p.id), 'Cover photo updated')}>
                      <StarIcon filled={isCover} />
                    </button>
                    {isCover && <span className="cover-tag">Cover</span>}
                  </div>
                  <div className="photo-name">{i + 1}. {p.name}</div>
                  <div className="photo-actions">
                    <button type="button" className="act-btn" aria-label={`Move ${p.name} earlier`} disabled={i === 0}
                      onClick={() => moveBy(p.id, -1)}><LeftIcon /></button>
                    <button type="button" className="act-btn" aria-label={`Move ${p.name} later`} disabled={i === photos.length - 1}
                      onClick={() => moveBy(p.id, 1)}><RightIcon /></button>
                    <button type="button" className="act-btn act-del" aria-label={`Delete ${p.name}`}
                      onClick={() => removePhotos([p.id])}><TrashIcon /> Delete</button>
                  </div>
                </div>
              );
            })}
          </div>
        </section>

        <aside className="settings-col">
          <section className="panel">
            <h2 className="panel-title">Gallery details</h2>
            <label className="field">
              Client / gallery name
              <input className="input" type="text" value={nameDraft} maxLength={200}
                onChange={(e) => setNameDraft(e.target.value)} onBlur={saveName}
                onKeyDown={(e) => e.key === 'Enter' && e.currentTarget.blur()} />
            </label>
            <label className="field">
              Photoshoot date
              <input className="input" type="date" value={gallery.shotDate}
                onChange={(e) => run(() => api.updateGallery(gallery.id, { shotDate: e.target.value }))} />
            </label>
          </section>

          <section className="panel">
            <h2 className="panel-title">Download period</h2>
            <div role="group" aria-label="How long the client can download" className="row wrap gap-8">
              {monthOptions.map((m) => (
                <button key={m} type="button" aria-pressed={gallery.months === m}
                  className={'btn btn-sm ' + (gallery.months === m ? 'btn-primary' : 'btn-outline')}
                  onClick={() => gallery.months !== m && run(() => api.updateGallery(gallery.id, { months: m }))}>
                  {monthsLabel(m)}
                </button>
              ))}
            </div>
            <div className={'expiry-box' + (gallery.downloadsOpen ? '' : ' closed')}>
              <span className="dot" />
              <span>{expiryText}</span>
            </div>
            <button type="button" className="text-btn"
              onClick={() => run(() => api.restartPeriod(gallery.id), 'Download period restarted from today')}>
              Restart the period from today
            </button>
          </section>

          <section className="panel">
            <h2 className="panel-title">Share with client</h2>
            <div className="field">
              <label htmlFor="share-url">Private gallery link</label>
              <div className="row gap-8">
                <input id="share-url" className="input input-readonly" type="text" readOnly value={shareUrl}
                  onFocus={(e) => e.currentTarget.select()} />
                <button type="button" className="btn btn-primary nowrap" onClick={copyLink}>
                  <LinkIcon /> {copied ? 'Copied' : 'Copy'}
                </button>
              </div>
            </div>
            <p className="hint">Anyone with this link sees only this gallery. No account needed.</p>
            <button type="button" className="text-btn" onClick={rotateLink}>
              Generate a new link (old one stops working)
            </button>
          </section>

          <section className="danger-zone">
            {confirmDelete ? (
              <div role="alert" className="confirm-box">
                <span>Delete “{gallery.name}” and all {plural(photos.length, 'photo')}? The client link stops working.</span>
                <div className="row gap-8 wrap">
                  <button type="button" className="btn btn-danger btn-sm" onClick={deleteGallery}>Delete gallery</button>
                  <button type="button" className="btn btn-outline btn-sm" onClick={() => setConfirmDelete(false)}>Cancel</button>
                </div>
              </div>
            ) : (
              <button type="button" className="text-btn danger" onClick={() => setConfirmDelete(true)}>
                <TrashIcon /> Delete this gallery
              </button>
            )}
          </section>
        </aside>
      </div>
    </main>
  );
}
