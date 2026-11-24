import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'
import App from './app/App'
import { ConnectionStrip } from './components/Connection'
import { MusicBarHost } from './music/MusicBarHost'
import '@fontsource-variable/figtree'
import './styles/tokens.css'
import './styles/app.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <App />
      <MusicBarHost />
      <ConnectionStrip />
    </BrowserRouter>
  </StrictMode>,
)
