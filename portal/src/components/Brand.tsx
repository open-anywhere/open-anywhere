import { Link } from '@tanstack/react-router'

export function Brand({
  subtitle,
  to = '/',
}: {
  subtitle?: string
  to?: '/' | '/portal' | '/admin'
}) {
  return (
    <Link to={to} className="brand" aria-label="Open Anywhere 首页">
      <span className="brand-mark" aria-hidden="true" />
      <span className="brand-name">
        Open Anywhere
        {subtitle ? <span className="brand-sub"> · {subtitle}</span> : null}
      </span>
    </Link>
  )
}
