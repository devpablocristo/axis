import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ConsoleRoot } from './ConsoleRoot'
import './styles.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ConsoleRoot />
  </StrictMode>,
)
