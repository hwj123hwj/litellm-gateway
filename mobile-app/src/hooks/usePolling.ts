import { useEffect, useRef } from 'react'

/**
 * 数据轮询 hook。
 *
 * - mount 时立即执行一次 `fn`
 * - 之后每 `intervalMs` 执行一次
 * - unmount 时清理定时器
 *
 * `fn` 必须自行处理竞态（通常通过 store 的 RequestGuard）。
 * 当 `fn` 引用变化时，定时器会重置。
 *
 * @param fn 要周期执行的异步函数（不接收参数）
 * @param intervalMs 轮询间隔（毫秒），默认 10s
 */
export function usePolling(fn: () => void | Promise<void>, intervalMs = 10000) {
  const savedFn = useRef(fn)

  // 始终保持 ref 指向最新的 fn，避免闭包陈旧，同时不重启定时器
  useEffect(() => {
    savedFn.current = fn
  }, [fn])

  useEffect(() => {
    // 立即执行一次
    savedFn.current()

    const timer = setInterval(() => {
      savedFn.current()
    }, intervalMs)

    return () => clearInterval(timer)
  }, [intervalMs])
}
