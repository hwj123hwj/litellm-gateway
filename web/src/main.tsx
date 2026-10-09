import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { HashRouter } from 'react-router-dom'
import App from './App'
import DesktopRoot from './desktop/DesktopRoot'
import { isDesktop } from './desktop/bridge'
import './styles/app.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <HashRouter>
      {isDesktop() ? <DesktopRoot /> : <App />}
    </HashRouter>
  </StrictMode>,
)
