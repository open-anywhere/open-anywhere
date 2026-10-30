import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach, beforeEach } from 'vitest'

// jsdom implements none of the browser APIs antd and the voice pages rely on.
// They are installed here once so every test file can assume them.

let systemPrefersDark = false
const mediaListeners = new Set<(event: MediaQueryListEvent) => void>()

export function setSystemPrefersDark(value: boolean) {
  systemPrefersDark = value
  const event = { matches: value, media: '(prefers-color-scheme: dark)' } as MediaQueryListEvent
  for (const listener of mediaListeners) listener(event)
}

function installMatchMedia() {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    configurable: true,
    value: (query: string): MediaQueryList => {
      return {
        // Read the current preference on every access, so a listener that
        // consults `matches` sees the value it was just notified about.
        get matches() {
          return query.includes('prefers-color-scheme: dark') ? systemPrefersDark : false
        },
        media: query,
        onchange: null,
        addEventListener: (_type: string, listener: (event: MediaQueryListEvent) => void) => {
          mediaListeners.add(listener)
        },
        removeEventListener: (_type: string, listener: (event: MediaQueryListEvent) => void) => {
          mediaListeners.delete(listener)
        },
        addListener: (listener: (event: MediaQueryListEvent) => void) => {
          mediaListeners.add(listener)
        },
        removeListener: (listener: (event: MediaQueryListEvent) => void) => {
          mediaListeners.delete(listener)
        },
        dispatchEvent: () => false,
      } as unknown as MediaQueryList
    },
  })
}

function installObservers() {
  class ResizeObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  class IntersectionObserverStub {
    root = null
    rootMargin = ''
    thresholds: number[] = []
    observe() {}
    unobserve() {}
    disconnect() {}
    takeRecords(): IntersectionObserverEntry[] {
      return []
    }
  }
  Object.defineProperty(window, 'ResizeObserver', { writable: true, value: ResizeObserverStub })
  Object.defineProperty(window, 'IntersectionObserver', {
    writable: true,
    value: IntersectionObserverStub,
  })
  Object.defineProperty(window.HTMLElement.prototype, 'scrollIntoView', {
    writable: true,
    value: () => undefined,
  })
  Object.defineProperty(window, 'scrollTo', { writable: true, value: () => undefined })
  Object.defineProperty(Element.prototype, 'scrollTo', { writable: true, value: () => undefined })

  // jsdom throws on the pseudo-element argument antd uses to measure a
  // scrollbar; the element's own style is enough for these tests.
  const getComputedStyle = window.getComputedStyle.bind(window)
  window.getComputedStyle = ((element: Element) =>
    getComputedStyle(element)) as typeof window.getComputedStyle
}

installMatchMedia()
installObservers()

beforeEach(() => {
  systemPrefersDark = false
  mediaListeners.clear()
  installMatchMedia()
  window.localStorage.clear()
  document.documentElement.removeAttribute('data-theme')
  document.documentElement.style.colorScheme = ''
  document.title = ''
})

afterEach(() => {
  cleanup()
})
