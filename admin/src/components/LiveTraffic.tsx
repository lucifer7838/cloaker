import React, { useEffect, useState, useRef, useCallback } from 'react'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080'

interface TrafficEvent {
  id: string
  timestamp: string
  ip: string
  country: string
  user_agent: string
  decision: string
  reason: string
  score: number
  campaign: string
}

const MAX_ROWS = 100

const styles: Record<string, React.CSSProperties> = {
  container: {
    padding: '1.5rem',
  },
  title: {
    fontSize: '1.25rem',
    fontWeight: 700,
    color: '#111827',
    marginBottom: '1.5rem',
  },
  controls: {
    display: 'flex',
    gap: '0.75rem',
    marginBottom: '1rem',
    alignItems: 'center',
  },
  pauseButton: {
    padding: '0.5rem 1rem',
    background: '#f59e0b',
    color: '#fff',
    border: 'none',
    borderRadius: '6px',
    fontSize: '0.875rem',
    fontWeight: 500,
    cursor: 'pointer',
  },
  resumeButton: {
    padding: '0.5rem 1rem',
    background: '#10b981',
    color: '#fff',
    border: 'none',
    borderRadius: '6px',
    fontSize: '0.875rem',
    fontWeight: 500,
    cursor: 'pointer',
  },
  statusConnected: {
    fontSize: '0.8rem',
    color: '#059669',
    fontWeight: 500,
  },
  statusDisconnected: {
    fontSize: '0.8rem',
    color: '#dc2626',
    fontWeight: 500,
  },
  count: {
    fontSize: '0.8rem',
    color: '#6b7280',
    marginLeft: 'auto',
  },
  card: {
    background: '#fff',
    borderRadius: '8px',
    padding: '1.5rem',
    boxShadow: '0 1px 3px rgba(0,0,0,0.1)',
    border: '1px solid #e5e7eb',
  },
  tableWrapper: {
    maxHeight: '500px',
    overflowY: 'auto' as const,
  },
  table: {
    width: '100%',
    borderCollapse: 'collapse' as const,
    fontSize: '0.825rem',
  },
  th: {
    textAlign: 'left' as const,
    padding: '0.5rem 0.75rem',
    borderBottom: '2px solid #e5e7eb',
    fontSize: '0.75rem',
    fontWeight: 600,
    color: '#6b7280',
    textTransform: 'uppercase' as const,
    position: 'sticky' as const,
    top: 0,
    background: '#fff',
  },
  td: {
    padding: '0.4rem 0.75rem',
    borderBottom: '1px solid #f3f4f6',
  },
  decisionAllow: {
    color: '#059669',
    fontWeight: 600,
  },
  decisionBlock: {
    color: '#dc2626',
    fontWeight: 600,
  },
  empty: {
    color: '#6b7280',
    fontStyle: 'italic',
    padding: '2rem 0',
    textAlign: 'center' as const,
  },
}

function LiveTraffic() {
  const [events, setEvents] = useState<TrafficEvent[]>([])
  const [paused, setPaused] = useState(false)
  const [connected, setConnected] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const pausedRef = useRef(false)
  const tableRef = useRef<HTMLDivElement>(null)

  pausedRef.current = paused

  const connectWebSocket = useCallback(() => {
    const wsURL = API_URL.replace(/^http/, 'ws') + '/ws/traffic'
    const ws = new WebSocket(wsURL)

    ws.onopen = () => {
      setConnected(true)
    }

    ws.onclose = () => {
      setConnected(false)
      // Reconnect after 3 seconds
      setTimeout(() => {
        if (wsRef.current === ws) {
          connectWebSocket()
        }
      }, 3000)
    }

    ws.onerror = () => {
      setConnected(false)
    }

    ws.onmessage = (event) => {
      if (pausedRef.current) return

      try {
        const data: TrafficEvent = JSON.parse(event.data)
        setEvents((prev) => {
          const updated = [data, ...prev]
          return updated.slice(0, MAX_ROWS)
        })
      } catch {
        // Ignore malformed messages
      }
    }

    wsRef.current = ws
  }, [])

  useEffect(() => {
    connectWebSocket()
    return () => {
      if (wsRef.current) {
        wsRef.current.close()
        wsRef.current = null
      }
    }
  }, [connectWebSocket])

  function truncateUA(ua: string): string {
    if (ua.length <= 40) return ua
    return ua.substring(0, 40) + '...'
  }

  return (
    <div style={styles.container}>
      <h2 style={styles.title}>Live Traffic</h2>

      <div style={styles.controls}>
        {paused ? (
          <button style={styles.resumeButton} onClick={() => setPaused(false)}>
            Resume
          </button>
        ) : (
          <button style={styles.pauseButton} onClick={() => setPaused(true)}>
            Pause
          </button>
        )}
        <span style={connected ? styles.statusConnected : styles.statusDisconnected}>
          {connected ? 'Connected' : 'Disconnected'}
        </span>
        <span style={styles.count}>{events.length} events</span>
      </div>

      <div style={styles.card}>
        <div style={styles.tableWrapper} ref={tableRef}>
          {events.length === 0 ? (
            <p style={styles.empty}>Waiting for traffic events...</p>
          ) : (
            <table style={styles.table}>
              <thead>
                <tr>
                  <th style={styles.th}>Time</th>
                  <th style={styles.th}>IP</th>
                  <th style={styles.th}>Country</th>
                  <th style={styles.th}>User Agent</th>
                  <th style={styles.th}>Decision</th>
                  <th style={styles.th}>Reason</th>
                  <th style={styles.th}>Score</th>
                  <th style={styles.th}>Campaign</th>
                </tr>
              </thead>
              <tbody>
                {events.map((evt) => (
                  <tr key={evt.id}>
                    <td style={styles.td}>{evt.timestamp}</td>
                    <td style={styles.td}>{evt.ip}</td>
                    <td style={styles.td}>{evt.country}</td>
                    <td style={styles.td}>{truncateUA(evt.user_agent)}</td>
                    <td style={styles.td}>
                      <span style={evt.decision === 'allow' ? styles.decisionAllow : styles.decisionBlock}>
                        {evt.decision}
                      </span>
                    </td>
                    <td style={styles.td}>{evt.reason}</td>
                    <td style={styles.td}>{evt.score.toFixed(2)}</td>
                    <td style={styles.td}>{evt.campaign}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  )
}

export default LiveTraffic
