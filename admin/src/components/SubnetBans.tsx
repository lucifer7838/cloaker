import React, { useEffect, useState, useRef } from 'react'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080'

interface BannedSubnet {
  subnet: string
  banned_at: string
  expires_at: string
  hit_count: number
}

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
  card: {
    background: '#fff',
    borderRadius: '8px',
    padding: '1.5rem',
    boxShadow: '0 1px 3px rgba(0,0,0,0.1)',
    border: '1px solid #e5e7eb',
  },
  cardTitle: {
    fontSize: '0.875rem',
    fontWeight: 600,
    color: '#6b7280',
    textTransform: 'uppercase' as const,
    letterSpacing: '0.05em',
    marginBottom: '1rem',
  },
  table: {
    width: '100%',
    borderCollapse: 'collapse' as const,
  },
  th: {
    textAlign: 'left' as const,
    padding: '0.75rem',
    borderBottom: '2px solid #e5e7eb',
    fontSize: '0.8rem',
    fontWeight: 600,
    color: '#6b7280',
    textTransform: 'uppercase' as const,
  },
  td: {
    padding: '0.75rem',
    borderBottom: '1px solid #f3f4f6',
    fontSize: '0.9rem',
  },
  unbanButton: {
    padding: '0.25rem 0.75rem',
    background: '#ef4444',
    color: '#fff',
    border: 'none',
    borderRadius: '4px',
    fontSize: '0.8rem',
    cursor: 'pointer',
  },
  refreshNote: {
    fontSize: '0.75rem',
    color: '#9ca3af',
    marginTop: '0.75rem',
  },
  empty: {
    color: '#6b7280',
    fontStyle: 'italic',
    padding: '1rem 0',
  },
  error: {
    color: '#dc2626',
    fontSize: '0.875rem',
  },
  loading: {
    color: '#6b7280',
    fontStyle: 'italic',
  },
}

function SubnetBans() {
  const [subnets, setSubnets] = useState<BannedSubnet[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const intervalRef = useRef<number | null>(null)

  useEffect(() => {
    fetchSubnets()
    intervalRef.current = window.setInterval(fetchSubnets, 10000)
    return () => {
      if (intervalRef.current !== null) {
        window.clearInterval(intervalRef.current)
      }
    }
  }, [])

  async function fetchSubnets() {
    try {
      setError(null)
      const res = await fetch(`${API_URL}/api/admin/subnets`)
      if (!res.ok) throw new Error(`Failed to fetch subnets: ${res.status}`)
      const data = await res.json()
      setSubnets(data || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch subnets')
    } finally {
      setLoading(false)
    }
  }

  async function handleUnban(subnet: string) {
    try {
      setError(null)
      const res = await fetch(`${API_URL}/api/admin/subnets/unban`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ subnet }),
      })
      if (!res.ok) throw new Error(`Failed to unban subnet: ${res.status}`)
      await fetchSubnets()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to unban subnet')
    }
  }

  if (loading) return <div style={styles.container}><p style={styles.loading}>Loading subnet bans...</p></div>

  return (
    <div style={styles.container}>
      <h2 style={styles.title}>Subnet Bans</h2>

      {error && <p style={styles.error}>Error: {error}</p>}

      <div style={styles.card}>
        <div style={styles.cardTitle}>Banned /24 Subnets</div>

        {subnets.length === 0 ? (
          <p style={styles.empty}>No subnets currently banned</p>
        ) : (
          <table style={styles.table}>
            <thead>
              <tr>
                <th style={styles.th}>Subnet</th>
                <th style={styles.th}>Banned At</th>
                <th style={styles.th}>Expires At</th>
                <th style={styles.th}>Hit Count</th>
                <th style={styles.th}>Action</th>
              </tr>
            </thead>
            <tbody>
              {subnets.map((s) => (
                <tr key={s.subnet}>
                  <td style={styles.td}>{s.subnet}</td>
                  <td style={styles.td}>{s.banned_at}</td>
                  <td style={styles.td}>{s.expires_at}</td>
                  <td style={styles.td}>{s.hit_count}</td>
                  <td style={styles.td}>
                    <button
                      style={styles.unbanButton}
                      onClick={() => handleUnban(s.subnet)}
                    >
                      Unban
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        <p style={styles.refreshNote}>Auto-refreshes every 10 seconds</p>
      </div>
    </div>
  )
}

export default SubnetBans
