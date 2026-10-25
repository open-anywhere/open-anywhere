import { App as AntApp, ConfigProvider, theme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import {
  applyDocumentTheme,
  readThemeMode,
  resolveTheme,
  systemTheme,
  writeThemeMode,
  type ResolvedTheme,
  type ThemeMode,
} from '../lib/theme'

interface ThemeContextValue {
  mode: ThemeMode
  resolved: ResolvedTheme
  setMode: (mode: ThemeMode) => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

export function useThemeMode(): ThemeContextValue {
  const value = useContext(ThemeContext)
  if (!value) {
    throw new Error('useThemeMode must be used inside ThemeProvider')
  }
  return value
}

const fontFamily =
  '"Inter", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", system-ui, sans-serif'

function themeConfig(resolved: ResolvedTheme) {
  const dark = resolved === 'dark'
  return {
    algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    cssVar: {},
    token: {
      colorPrimary: dark ? '#7c85e8' : '#5e6ad2',
      colorInfo: dark ? '#7c85e8' : '#5e6ad2',
      colorBgBase: dark ? '#0e0f11' : '#ffffff',
      colorBgLayout: dark ? '#0e0f11' : '#f7f7f8',
      colorBgContainer: dark ? '#161719' : '#ffffff',
      colorBgElevated: dark ? '#1c1d20' : '#ffffff',
      colorTextBase: dark ? '#f2f3f5' : '#1c1c1f',
      colorBorder: dark ? '#34353a' : '#d4d4d8',
      colorBorderSecondary: dark ? '#26272b' : '#e4e4e7',
      borderRadius: 8,
      borderRadiusSM: 6,
      controlHeight: 34,
      fontSize: 14,
      fontFamily,
    },
    components: {
      Button: {
        primaryShadow: 'none',
        defaultShadow: 'none',
        dangerShadow: 'none',
      },
      Table: {
        headerBg: dark ? '#1a1b1e' : '#fafafa',
        headerColor: dark ? '#a0a2ab' : '#62626b',
        rowHoverBg: dark ? '#1b1c1f' : '#fafafa',
      },
    },
  }
}

function TokenBridge({ resolved }: { resolved: ResolvedTheme }) {
  const { token } = theme.useToken()

  useEffect(() => {
    const root = document.documentElement
    const variables: Record<string, string> = {
      '--oa-bg': token.colorBgLayout,
      '--oa-surface': token.colorBgContainer,
      '--oa-surface-elevated': token.colorBgElevated,
      '--oa-sidebar': resolved === 'dark' ? '#101113' : '#f2f2f3',
      '--oa-border': token.colorBorderSecondary,
      '--oa-border-strong': token.colorBorder,
      '--oa-text': token.colorText,
      '--oa-text-secondary': token.colorTextSecondary,
      '--oa-text-tertiary': token.colorTextTertiary,
      '--oa-primary': token.colorPrimary,
      '--oa-primary-hover': token.colorPrimaryHover,
      '--oa-success': token.colorSuccess,
      '--oa-warning': token.colorWarning,
      '--oa-error': token.colorError,
      '--oa-success-bg': token.colorSuccessBg,
      '--oa-warning-bg': token.colorWarningBg,
      '--oa-error-bg': token.colorErrorBg,
      '--oa-info-bg': token.colorInfoBg,
      '--oa-fill': token.colorFillQuaternary,
      '--oa-radius': `${token.borderRadius}px`,
      '--oa-radius-sm': `${token.borderRadiusSM}px`,
      '--oa-shadow': token.boxShadowTertiary,
    }
    for (const [name, value] of Object.entries(variables)) {
      root.style.setProperty(name, value)
    }
  }, [token, resolved])

  return null
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(readThemeMode)
  const [system, setSystem] = useState<ResolvedTheme>(systemTheme)
  const resolved = resolveTheme(mode, system)

  useEffect(() => {
    const query = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => setSystem(query.matches ? 'dark' : 'light')
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])

  useEffect(() => {
    applyDocumentTheme(resolved)
  }, [resolved])

  const setMode = useCallback((next: ThemeMode) => {
    writeThemeMode(next)
    setModeState(next)
  }, [])

  const value = useMemo(() => ({ mode, resolved, setMode }), [mode, resolved, setMode])
  const config = useMemo(() => themeConfig(resolved), [resolved])

  return (
    <ThemeContext.Provider value={value}>
      <ConfigProvider locale={zhCN} theme={config}>
        <AntApp>
          <TokenBridge resolved={resolved} />
          {children}
        </AntApp>
      </ConfigProvider>
    </ThemeContext.Provider>
  )
}
