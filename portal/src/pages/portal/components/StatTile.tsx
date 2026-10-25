import type { ReactNode } from 'react'

export function StatTile({
  icon,
  label,
  value,
  note,
  tone = 'neutral',
}: {
  icon?: ReactNode
  label: string
  value: ReactNode
  note?: ReactNode
  tone?: 'neutral' | 'primary' | 'success' | 'warning' | 'danger'
}) {
  return (
    <div className="stat-tile">
      <div className="stat-tile-top">
        <span className="stat-label">{label}</span>
        {icon ? <span className={`stat-icon stat-icon-${tone}`}>{icon}</span> : null}
      </div>
      <div className="stat-value">{value}</div>
      {note ? <div className="stat-note">{note}</div> : null}
    </div>
  )
}
