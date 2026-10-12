import { useCallback, useEffect, useRef, useState } from 'react'
import { api, wsUrl } from './api'

/**
 * Loads a poll over REST, then keeps its results live over a WebSocket.
 *
 * Every "results" message carries the FULL counts, so the UI is always replaced with the truth
 * instead of patched incrementally: a missed or duplicated message can never corrupt the numbers.
 * Votes only ever increase, so a message with a lower total than we already show is stale and ignored.
 */
export function useLivePoll(slug) {
  const [poll, setPoll] = useState(null)
  const [loadError, setLoadError] = useState(null)
  const [counts, setCounts] = useState({})
  const [total, setTotal] = useState(0)
  const [viewers, setViewers] = useState(0)
  const [closed, setClosed] = useState(false)
  const [deleted, setDeleted] = useState(false)
  const [connection, setConnection] = useState('connecting') // connecting | live | reconnecting
  const [lastBumped, setLastBumped] = useState(null) // option id that just gained a vote
  const totalRef = useRef(0)
  const countsRef = useRef({})

  const applyResults = useCallback((nextCounts, nextTotal) => {
    if (nextTotal < totalRef.current) return
    // Work out which option(s) grew, so the UI can flash them.
    const grew = Object.keys(nextCounts).find((id) => nextCounts[id] > (countsRef.current[id] ?? 0))
    if (grew && totalRef.current > 0) setLastBumped({ id: grew, at: Date.now() })
    totalRef.current = nextTotal
    countsRef.current = nextCounts
    setCounts(nextCounts)
    setTotal(nextTotal)
  }, [])

  // 1) initial load
  useEffect(() => {
    let alive = true
    totalRef.current = 0
    countsRef.current = {}
    setPoll(null)
    setLoadError(null)
    api
      .getPoll(slug)
      .then((p) => {
        if (!alive) return
        setPoll(p)
        setClosed(p.closed)
        applyResults(p.counts, p.total)
      })
      .catch((e) => alive && setLoadError(e))
    return () => {
      alive = false
    }
  }, [slug, applyResults])

  // 2) live connection (only once we know the poll exists)
  const exists = poll !== null
  useEffect(() => {
    if (!exists) return
    let ws
    let timer
    let attempt = 0
    let stopped = false

    const connect = () => {
      ws = new WebSocket(wsUrl(`/ws/polls/${slug}`))
      ws.onopen = () => {
        attempt = 0
        setConnection('live')
      }
      ws.onmessage = (e) => {
        let msg
        try {
          msg = JSON.parse(e.data)
        } catch {
          return
        }
        if (msg.type === 'results') applyResults(msg.counts, msg.total)
        else if (msg.type === 'viewers') setViewers(msg.count)
        else if (msg.type === 'status') setClosed(msg.closed)
        else if (msg.type === 'deleted') setDeleted(true)
      }
      ws.onclose = () => {
        if (stopped) return
        setConnection('reconnecting')
        // Exponential backoff with jitter so a server restart doesn't get a thundering herd.
        const delay = Math.min(1000 * 2 ** attempt++, 15000) + Math.random() * 500
        timer = setTimeout(connect, delay)
      }
    }

    // Phones often freeze background tabs; reconnect right away when the tab comes back.
    const onVisible = () => {
      if (document.visibilityState === 'visible' && ws && ws.readyState > WebSocket.OPEN && !stopped) {
        clearTimeout(timer)
        connect()
      }
    }
    document.addEventListener('visibilitychange', onVisible)
    connect()

    return () => {
      stopped = true
      clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisible)
      if (ws) ws.close()
    }
  }, [exists, slug, applyResults])

  return {
    poll, setPoll, loadError, counts, total, viewers, closed, setClosed, deleted, connection,
    lastBumped, applyResults,
  }
}
