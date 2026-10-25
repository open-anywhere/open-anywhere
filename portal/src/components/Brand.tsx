import { Link } from '@tanstack/react-router'
import logoDark from '../assets/logo/open-anywhere-logo-dark.svg'
import logoLight from '../assets/logo/open-anywhere-logo.svg'
import mascot from '../assets/logo/open-anywhere-mascot.png'
import { useThemeMode } from './ThemeProvider'

export function Brand({ to = '/' }: { to?: '/' | '/portal' | '/admin' }) {
  const { resolved } = useThemeMode()

  return (
    <Link to={to} className="brand" aria-label="Open Anywhere 首页">
      <img className="brand-mascot" src={mascot} alt="" />
      <img
        className="brand-logo"
        src={resolved === 'dark' ? logoDark : logoLight}
        alt="Open Anywhere"
      />
    </Link>
  )
}
