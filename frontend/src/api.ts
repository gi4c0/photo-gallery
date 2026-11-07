// Thin client for the Go backend. See backend/README.md for the contract.

export interface User {
  id: string;
  email: string;
  isAdmin: boolean;
}

export interface Settings {
  studioName: string;
  accentColor: string;
}

export interface Photo {
  id: string;
  name: string;
  width: number;
  height: number;
  thumbUrl: string;
  webUrl: string;
  originalUrl?: string;
  downloadUrl?: string;
  downloads?: number; // studio only: single-photo downloads by clients
  lastDownloadedAt?: string; // UTC RFC 3339
}

export interface Gallery {
  id: string;
  name: string;
  shotDate: string;
  sharedOn: string;
  months: number;
  expiresOn?: string;
  downloadsOpen: boolean;
  slug: string;
  coverPhotoId?: string;
  coverThumbUrl?: string;
  photoCount: number;
  zipDownloads?: number; // gallery editor only: "download all" ZIPs started by clients
  zipLastDownloadedAt?: string; // UTC RFC 3339
  photos?: Photo[];
}

export interface ClientGallery {
  name: string;
  shotDate: string;
  coverPhotoId?: string;
  downloadsOpen: boolean;
  expiresOn?: string;
  downloadAllUrl?: string;
  photos: Photo[];
}

export interface UploadResult {
  photos: Photo[];
  rejected: { name: string; error: string }[];
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

// Fired when a signed-in request gets 401, so the app can return to login.
export const UNAUTHORIZED_EVENT = 'api:unauthorized';

function errorMessage(status: number, body: unknown): string {
  if (body && typeof body === 'object' && 'error' in body && typeof body.error === 'string') {
    return body.error;
  }
  if (status === 429) return 'Too many attempts. Wait a minute and try again.';
  if (status === 413) return 'That upload is too large.';
  return 'Something went wrong. Please try again.';
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(0, 'Cannot reach the server. Check your connection.');
  }
  if (res.status === 204) return undefined as T;
  const data: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const authPath = path === '/api/login' || path === '/api/me' || path === '/api/password';
    if (res.status === 401 && !authPath) window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
    throw new ApiError(res.status, errorMessage(res.status, data));
  }
  return data as T;
}

const g = (id: string) => `/api/galleries/${encodeURIComponent(id)}`;

export const api = {
  settings: () => request<Settings>('GET', '/api/settings'),
  updateSettings: (s: Partial<Settings>) => request<Settings>('PATCH', '/api/settings', s),

  registrationOpen: () => request<{ open: boolean }>('GET', '/api/register'),
  register: (email: string, password: string, studioName: string) =>
    request<User>('POST', '/api/register', { email, password, studioName }),
  login: (email: string, password: string) => request<User>('POST', '/api/login', { email, password }),
  logout: () => request<void>('POST', '/api/logout'),
  me: () => request<User>('GET', '/api/me'),
  changePassword: (currentPassword: string, newPassword: string) =>
    request<void>('POST', '/api/password', { currentPassword, newPassword }),

  users: () => request<User[]>('GET', '/api/users'),
  createUser: (email: string, password: string, isAdmin: boolean) =>
    request<User>('POST', '/api/users', { email, password, isAdmin }),
  deleteUser: (id: string) => request<void>('DELETE', `/api/users/${encodeURIComponent(id)}`),

  galleries: () => request<Gallery[]>('GET', '/api/galleries'),
  createGallery: (name: string, shotDate: string, months: number) =>
    request<Gallery>('POST', '/api/galleries', { name, shotDate, months }),
  gallery: (id: string) => request<Gallery>('GET', g(id)),
  updateGallery: (id: string, patch: Partial<Pick<Gallery, 'name' | 'shotDate' | 'months'>>) =>
    request<Gallery>('PATCH', g(id), patch),
  deleteGallery: (id: string) => request<void>('DELETE', g(id)),
  setCover: (id: string, photoId: string) => request<Gallery>('PATCH', `${g(id)}/cover`, { photoId }),
  restartPeriod: (id: string) => request<Gallery>('POST', `${g(id)}/restart-download-period`),
  rotateLink: (id: string) => request<Gallery>('POST', `${g(id)}/rotate-link`),
  reorderPhotos: (id: string, photoIds: string[]) =>
    request<Gallery>('PATCH', `${g(id)}/photos/order`, { photoIds }),
  deletePhotos: (id: string, photoIds: string[]) =>
    request<Gallery>('DELETE', `${g(id)}/photos`, { photoIds }),

  clientGallery: (slug: string) => request<ClientGallery>('GET', `/api/g/${encodeURIComponent(slug)}`),
};

// Uploads one file. XHR, not fetch, because fetch has no upload progress events.
export function uploadPhoto(
  galleryId: string,
  file: File,
  onProgress: (fraction: number) => void,
): Promise<UploadResult> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `${g(galleryId)}/photos`);
    xhr.responseType = 'json';
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(e.loaded / e.total);
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(xhr.response as UploadResult);
        return;
      }
      if (xhr.status === 401) window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
      reject(new ApiError(xhr.status, errorMessage(xhr.status, xhr.response)));
    };
    xhr.onerror = () => reject(new ApiError(0, 'Upload interrupted. Check your connection.'));
    const form = new FormData();
    form.append('files', file, file.name);
    xhr.send(form);
  });
}
