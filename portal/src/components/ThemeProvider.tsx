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
      colorPrimary: dark ? '#00C2A8' : '#007664',
      colorPrimaryHover: dark ? '#35D8C0' : '#006653',
      colorInfo: dark ? '#00C2A8' : '#007664',
      colorBgBase: dark ? '#07090C' : '#FFFFFF',
      colorBgLayout: dark ? '#07090C' : '#EFEAE1',
      colorBgContainer: dark ? '#171E26' : '#FFFFFF',
      colorBgElevated: dark ? '#1F2730' : '#FFFFFF',
      colorTextBase: dark ? '#F2F4F2' : '#0E1116',
      colorTextSecondary: dark ? '#A8AEAD' : '#5E5A52',
      colorTextTertiary: dark ? '#80888D' : '#6B665C',
      colorBorder: dark ? '#39434E' : '#CFC7BA',
      colorBorderSecondary: dark ? '#2A323B' : '#E2DCD2',
      colorSuccess: dark ? '#4EC38A' : '#177A4F',
      colorWarning: dark ? '#D6A94F' : '#9A6212',
      colorError: dark ? '#E57368' : '#C0362C',
      colorSuccessBg: dark ? '#16261E' : '#E7F5ED',
      colorWarningBg: dark ? '#2A2418' : '#FBF3E4',
      colorErrorBg: dark ? '#2C1D1C' : '#FBE9E7',
      colorInfoBg: dark ? '#0F2A26' : '#E0F2ED',
      colorFillQuaternary: dark ? '#1E262E' : '#EAE4DA',
      borderRadius: 8,
      borderRadiusSM: 6,
      controlHeight: 34,
      fontSize: 14,
      fontFamily,
    },
    components: {
      Button: {
        // Bright teal carries ink text in dark mode; white would fail contrast.
        primaryColor: dark ? '#08110F' : '#FFFFFF',
        primaryShadow: 'none',
        defaultShadow: 'none',
        dangerShadow: 'none',
      },
      Table: {
        headerBg: dark ? '#1B232C' : '#F4F0E8',
        headerColor: dark ? '#A8AEAD' : '#5E5A52',
        rowHoverBg: dark ? '#1B232C' : '#F8F5EF',
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
      '--oa-sidebar': resolved === 'dark' ? '#07090C' : '#EFEAE1',
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
