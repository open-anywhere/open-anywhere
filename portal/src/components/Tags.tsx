import type { ReactNode } from 'react'
import type { AppState, DeviceState } from '../api'
import { appStateLabels, deviceStateLabels, voiceStateLabels } from '../lib/format'

type Tone = 'neutral' | 'primary' | 'success' | 'warning' | 'danger' | 'info'

function Pill({ tone, children }: { tone: Tone; children: ReactNode }) {
  return <span className={`pill pill-${tone}`}>{children}</span>
}

const appTones: Record<AppState, Tone> = {
  building: 'warning',
  idle: 'neutral',
  running: 'success',
  error: 'danger',
}

export function AppStateTag({ state }: { state: AppState }) {
  return <Pill tone={appTones[state] ?? 'neutral'}>{appStateLabels[state] ?? state}</Pill>
}

export function VoiceTag({ state, connected }: { state: string; connected: boolean }) {
  const tone: Tone = connected
    ? 'success'
    : state === 'error'
      ? 'danger'
      : state === 'offline' || state === 'disconnected'
        ? 'neutral'
        : state === 'reconnecting'
          ? 'warning'
          : 'primary'
  return <Pill tone={tone}>{voiceStateLabels[state] ?? state}</Pill>
}

export function DeviceStateTag({ state }: { state: DeviceState }) {
  const tone: Tone =
    state === 'registered'
      ? 'success'
      : state === 'provisioning'
        ? 'warning'
        : state === 'error'
          ? 'danger'
          : 'neutral'
  return <Pill tone={tone}>{deviceStateLabels[state] ?? state}</Pill>
}
