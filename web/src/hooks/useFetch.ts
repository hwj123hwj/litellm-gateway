import { useState, useEffect, useCallback } from 'react'

interface UseFetchOptions {
  /** 自动刷新间隔(ms)，0 表示不自动刷新 */
  refreshInterval?: number
}

export function useFetch<T>(
  fetcher: () => Promise<T>,
  options: UseFetchOptions = {},
) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      const result = await fetcher()
      setData(result)
      setError(null)
    } catch (e: any) {
      setError(e.message || '请求失败')
    } finally {
      setLoading(false)
    }
  }, [fetcher])

  useEffect(() => {
    load()
    if (options.refreshInterval && options.refreshInterval > 0) {
      const timer = setInterval(load, options.refreshInterval)
      return () => clearInterval(timer)
    }
  }, [load, options.refreshInterval])

  return { data, error, loading, refresh: load }
}
