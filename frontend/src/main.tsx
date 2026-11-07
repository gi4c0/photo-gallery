import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router';
import { AuthProvider, SettingsProvider, ToastProvider } from './app';
import Login from './pages/Login';
import StudioLayout from './pages/StudioLayout';
import GalleryList from './pages/GalleryList';
import GalleryEditor from './pages/GalleryEditor';
import Account from './pages/Account';
import ClientGallery from './pages/ClientGallery';
// Self-hosted so visitors' browsers make no requests to third-party font servers.
import '@fontsource/lato/300.css';
import '@fontsource/lato/400.css';
import '@fontsource/lato/700.css';
import '@fontsource/lato/900.css';
import './styles.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <SettingsProvider>
        <ToastProvider>
          <Routes>
            {/* Client link: no account, the slug is the credential. */}
            <Route path="/g/:slug" element={<ClientGallery />} />
            <Route
              path="*"
              element={
                <AuthProvider>
                  <Routes>
                    <Route path="/login" element={<Login />} />
                    <Route element={<StudioLayout />}>
                      <Route index element={<GalleryList />} />
                      <Route path="galleries/:id" element={<GalleryEditor />} />
                      <Route path="account" element={<Account />} />
                    </Route>
                    <Route path="*" element={<Navigate to="/" replace />} />
                  </Routes>
                </AuthProvider>
              }
            />
          </Routes>
        </ToastProvider>
      </SettingsProvider>
    </BrowserRouter>
  </StrictMode>,
);
