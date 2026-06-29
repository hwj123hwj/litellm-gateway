import { useEffect, useRef } from 'react'

/**
 * 数据轮询 hook。
 *
 * - mount 时立即执行一次 `fn`
 * - 之后每 `intervalMs` 执行一次
 * - unmount 时清理定时器
 *
 * 防抖保护：如果 `fn` 上一次的 Promise 尚未结束，下一次轮询会被跳过，
 * 避免慢网络下请求堆叠（RequestGuard 会 abort 旧请求，但防抖能在源头减少无意义的网络发起）。
 *
 * `fn` 必须自行处理竞态（通常通过 store 的 RequestGuard）。
 * 当 `fn` 引用变化时不会重置定时器（通过 ref 保持最新引用）。
 *
 * @param fn 要周期执行的异步函数（不接收参数）
 * @param intervalMs 轮询间隔（毫秒），默认 10s
 */
export function usePolling(fn: () => void | Promise<void>, intervalMs = 10000) {
  const savedFn = useRef(fn)
  const isRunning = useRef(false)

  // 始终保持 ref 指向最新的 fn，避免闭包陈旧，同时不重启定时器
  useEffect(() => {
    savedFn.current = fn
  }, [fn])

  useEffect(() => {
    const tick = async () => {
      // 防抖：上一次请求尚未返回时，跳过本次
      if (isRunning.current) return
      isRunning.current = true
      try {
        await savedFn.current()
      } finally {
        isRunning.current = false
      }
    }

    // 立即执行一次
    tick()

    const timer = setInterval(tick, intervalMs)

    return () => clearInterval(timer)
  }, [intervalMs])
}
