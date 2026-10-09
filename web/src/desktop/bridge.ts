export interface Profile { id: string; name: string; url: string; hasToken: boolean }
export interface ConnectionState { activeId: string; profiles: Profile[] }
export interface ConnectionInput { id: string; name: string; url: string; token: string }

declare global {
  interface Window { mygo?: { call<T>(method: string, ...args: unknown[]): Promise<T> } }
}

export const isDesktop = () => typeof window.mygo?.call === 'function'
export function nativeCall<T>(method: string, ...args: unknown[]): Promise<T> {
  if (!window.mygo) return Promise.reject(new Error('桌面连接服务不可用'))
  return window.mygo.call<T>(`Gateway.${method}`, ...args)
}

// WebView2 maps custom schemes to http://<scheme>.localhost on Windows.
export function desktopAPIBase(): string {
  return location.origin === 'http://mygo.localhost'
    ? 'http://gateway.localhost/admin' : 'gateway://localhost/admin'
}
export function manageConnections() { location.hash = '/connections' }
