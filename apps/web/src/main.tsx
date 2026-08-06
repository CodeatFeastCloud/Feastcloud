import React from 'react';
import ReactDOM from 'react-dom/client';
import '@fontsource/poppins/300.css';
import '@fontsource/poppins/400.css';
import '@fontsource/poppins/500.css';
import '@fontsource/poppins/600.css';
import '@fontsource/poppins/700.css';
import App from './App';
import { loadConfiguredLanguagePacks } from './i18n';
import { registerServiceWorker } from './registerServiceWorker';
import './styles.css';
import './components/feastcloud-theme.css';

function application() {
  return (
    <React.StrictMode>
      <App />
    </React.StrictMode>
  );
}

function start() {
  const root = ReactDOM.createRoot(document.getElementById('root')!);

  // Bundled languages are sufficient for the first paint. Restoring or refreshing
  // optional packs must never put a network dependency on opening the POS/KDS.
  root.render(application());
  registerServiceWorker();

  void loadConfiguredLanguagePacks()
    .then(() => {
      // Re-render without remounting so newly installed language options become
      // visible while preserving the active kitchen session.
      root.render(application());
    })
    .catch((error) => {
      console.warn('Additional language packs could not be installed', error);
    });
}

start();
