import { useEffect, useRef, useState } from 'react';
import { useParams } from 'react-router';
import { api, type ApiError, type ClientGallery as Gallery, type Photo } from '../api';
import { useStudioName, useToast } from '../app';
import { fmtDate, fmtLongDate, lastOpenDay } from '../format';
import { CloseIcon, DownloadIcon, NextIcon, PrevIcon } from '../icons';

// What a client sees at /g/{slug}: a cover, then the photo grid and a lightbox.
export default function ClientGallery() {
  const { slug = '' } = useParams();
  const studioName = useStudioName();
  const toast = useToast();
  const [gallery, setGallery] = useState<Gallery | null>(null);
  const [error, setError] = useState('');
  const [view, setView] = useState<'cover' | 'grid'>('cover');
  const [lightbox, setLightbox] = useState(-1);
  const maxCols = useMaxColumns();

  useEffect(() => {
    api.clientGallery(slug).then(
      (g) => {
        setGallery(g);
        document.title = g.name;
      },
      (e: ApiError) => setError(e.message),
    );
  }, [slug]);

  if (error) {
    return (
      <div className="center-page">
        <div className="notice-card">
          <div className="brand brand-md">{studioName}</div>
          <p>{error}</p>
        </div>
      </div>
    );
  }
  if (!gallery) return null;

  const photos = gallery.photos ?? [];
  const cover = photos.find((p) => p.id === gallery.coverPhotoId) ?? photos[0];
  const canDownload = gallery.downloadsOpen;
  // Never more columns than photos, so a small gallery stays centered.
  const cols = Math.max(1, Math.min(maxCols, photos.length));
  const columns = toColumns(photos, cols);

  if (view === 'cover') {
    return (
      <div className="client-cover">
        {cover && <img src={cover.webUrl} alt="" />}
        <div className="cover-shade" aria-hidden="true" />
        <div className="cover-content">
          <h1 className="cover-title">{gallery.name}</h1>
          {gallery.shotDate && <div className="cover-date">{fmtLongDate(gallery.shotDate)}</div>}
          <button type="button" className="cover-btn" onClick={() => setView('grid')} autoFocus>View gallery</button>
        </div>
        <div className="cover-studio">By {studioName}</div>
      </div>
    );
  }

  return (
    <>
      <header className="client-header">
        <button type="button" className="plain-btn" onClick={() => setView('cover')}>
          <span className="client-title">{gallery.name}</span>
          <span className="client-studio">{studioName}</span>
        </button>
        {canDownload && gallery.downloadAllUrl && photos.length > 0 ? (
          <a className="btn btn-primary btn-caps" href={gallery.downloadAllUrl}
            onClick={() => toast(`Preparing a ZIP of ${photos.length} photos… The download starts in a moment.`)}>
            <DownloadIcon /> Download all ({photos.length})
          </a>
        ) : (
          <button type="button" className="btn btn-disabled btn-caps" disabled>
            <DownloadIcon /> Download all ({photos.length})
          </button>
        )}
      </header>

      {!canDownload && gallery.expiresOn && (
        <div role="status" className="expired-banner">
          Downloads for this gallery closed on {fmtDate(gallery.expiresOn)}. You can still view the photos.
          Contact your photographer to reopen downloads.
        </div>
      )}
      {canDownload && gallery.expiresOn && (
        <p className="until-note">Downloads available until {lastOpenDay(gallery.expiresOn)}</p>
      )}
      {photos.length === 0 && <p className="coming-soon">Photos are coming soon.</p>}

      <main className="client-main">
        <div className="masonry" style={{ maxWidth: cols * 440 }}>
          {columns.map((col, c) => (
            <div key={c} className="masonry-col">
              {col.map(({ photo: p, index: i }) => (
                <figure key={p.id} className="tile">
                  <button type="button" className="tile-btn" aria-label={`Open ${p.name}`} onClick={() => setLightbox(i)}
                    style={{ aspectRatio: `${p.width} / ${p.height}` }}>
                    <img src={p.thumbUrl} alt={p.name} loading="lazy" />
                  </button>
                  {canDownload && p.downloadUrl && (
                    <a className="tile-dl" href={p.downloadUrl} aria-label={`Download ${p.name}`}>
                      <DownloadIcon />
                    </a>
                  )}
                </figure>
              ))}
            </div>
          ))}
        </div>
      </main>

      {lightbox >= 0 && lightbox < photos.length && (
        <Lightbox gallery={gallery} index={lightbox} onIndex={setLightbox} onClose={() => setLightbox(-1)} />
      )}
    </>
  );
}

// Columns by viewport width: 2 on phones, 3 on tablets, 4 on desktop.
function useMaxColumns(): number {
  const pick = () => (window.innerWidth < 640 ? 2 : window.innerWidth < 1024 ? 3 : 4);
  const [n, setN] = useState(pick);
  useEffect(() => {
    const onResize = () => setN(pick());
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);
  return n;
}

// Masonry: each photo goes into the currently shortest column, using its
// aspect ratio, so columns end at similar heights and none stays empty.
function toColumns(photos: Photo[], cols: number) {
  const columns: { photo: Photo; index: number }[][] = Array.from({ length: cols }, () => []);
  const heights = new Array<number>(cols).fill(0);
  photos.forEach((photo, index) => {
    const c = heights.indexOf(Math.min(...heights));
    columns[c].push({ photo, index });
    heights[c] += photo.width ? photo.height / photo.width : 1;
  });
  return columns;
}

function Lightbox({ gallery, index, onIndex, onClose }: {
  gallery: Gallery; index: number; onIndex: (i: number) => void; onClose: () => void;
}) {
  const photos = gallery.photos;
  const p = photos[index];
  const step = (d: number) => onIndex((index + d + photos.length) % photos.length);
  const closeRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    closeRef.current?.focus();
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = '';
      opener?.focus();
    };
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
      else if (e.key === 'ArrowLeft') step(-1);
      else if (e.key === 'ArrowRight') step(1);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });

  // Warm the cache for the neighbours so arrowing through feels instant.
  useEffect(() => {
    for (const d of [-1, 1]) {
      const n = photos[(index + d + photos.length) % photos.length];
      if (n) new Image().src = n.webUrl;
    }
  }, [index, photos]);

  return (
    <div role="dialog" aria-modal="true" aria-label="Photo viewer" className="lightbox">
      <div className="lb-top">
        <span className="lb-name">{p.name}</span>
        <div className="row gap-4">
          {gallery.downloadsOpen && p.downloadUrl && (
            <a href={p.downloadUrl} className="lb-icon" aria-label="Download this photo"><DownloadIcon size={20} /></a>
          )}
          <button ref={closeRef} type="button" className="lb-icon" aria-label="Close viewer" onClick={onClose}><CloseIcon /></button>
        </div>
      </div>
      <div className="lb-stage">
        <img key={p.id} src={p.webUrl} alt={p.name} width={p.width} height={p.height} />
      </div>
      <div className="lb-nav">
        <button type="button" className="lb-round" aria-label="Previous photo" onClick={() => step(-1)}><PrevIcon /></button>
        <span className="lb-counter">{index + 1} / {photos.length}</span>
        <button type="button" className="lb-round" aria-label="Next photo" onClick={() => step(1)}><NextIcon /></button>
      </div>
    </div>
  );
}
