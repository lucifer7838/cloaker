import React, { useEffect, useState } from 'react'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080'

interface APIKey {
  id: string
  key_prefix: string
  name: string
  created_at: string
  last_used_at: string
  rate_limit: number
  active: boolean
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
    marginBottom: '1.5rem',
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
  form: {
    display: 'flex',
    gap: '0.75rem',
    alignItems: 'flex-end',
  },
  formGroup: {
    display: 'flex',
    flexDirection: 'column' as const,
    gap: '0.25rem',
  },
  label: {
    fontSize: '0.8rem',
    fontWeight: 500,
    color: '#6b7280',
  },
  input: {
    padding: '0.5rem 0.75rem',
    border: '1px solid #d1d5db',
    borderRadius: '6px',
    fontSize: '0.875rem',
    outline: 'none',
  },
  createButton: {
    padding: '0.5rem 1rem',
    background: '#10b981',
    color: '#fff',
    border: 'none',
    borderRadius: '6px',
    fontSize: '0.875rem',
    fontWeight: 500,
    cursor: 'pointer',
  },
  revokeButton: {
    padding: '0.25rem 0.75rem',
    background: '#ef4444',
    color: '#fff',
    border: 'none',
    borderRadius: '4px',
    fontSize: '0.8rem',
    cursor: 'pointer',
  },
  activeStatus: {
    color: '#059669',
    fontWeight: 600,
  },
  inactiveStatus: {
    color: '#dc2626',
    fontWeight: 600,
  },
  keyDisplay: {
    background: '#f3f4f6',
    padding: '1rem',
    borderRadius: '6px',
    fontFamily: 'monospace',
    fontSize: '0.875rem',
    marginTop: '1rem',
    wordBreak: 'break-all' as const,
    border: '1px solid #d1d5db',
  },
  keyWarning: {
    color: '#d97706',
    fontSize: '0.8rem',
    marginTop: '0.5rem',
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

function APIKeys() {
  const [keys, setKeys] = useState<APIKey[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [newKeyName, setNewKeyName] = useState('')
  const [newKeyRateLimit, setNewKeyRateLimit] = useState('1000')
  const [createdKey, setCreatedKey] = useState<string | null>(null)

  useEffect(() => {
    fetchKeys()
  }, [])

  async function fetchKeys() {
    try {
      setError(null)
      const res = await fetch(`${API_URL}/api/admin/keys`)
      if (!res.ok) throw new Error(`Failed to fetch keys: ${res.status}`)
      const data = await res.json()
      setKeys(data || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch keys')
    } finally {
      setLoading(false)
    }
  }

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    if (!newKeyName.trim()) return

    try {
      setError(null)
      setCreatedKey(null)
      const res = await fetch(`${API_URL}/api/admin/keys`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: newKeyName.trim(),
          rate_limit: parseInt(newKeyRateLimit) || 1000,
        }),
      })
      if (!res.ok) throw new Error(`Failed to create key: ${res.status}`)
      const data = await res.json()
      setCreatedKey(data.key)
      setNewKeyName('')
      setNewKeyRateLimit('1000')
      await fetchKeys()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create key')
    }
  }

  async function handleRevoke(id: string) {
    if (!confirm('Are you sure you want to revoke this API key?')) return

    try {
      setError(null)
      const res = await fetch(`${API_URL}/api/admin/keys/${id}`, {
        method: 'DELETE',
      })
      if (!res.ok) throw new Error(`Failed to revoke key: ${res.status}`)
      await fetchKeys()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to revoke key')
    }
  }

  if (loading) return <div style={styles.container}><p style={styles.loading}>Loading API keys...</p></div>

  return (
    <div style={styles.container}>
      <h2 style={styles.title}>API Keys</h2>

      {error && <p style={styles.error}>Error: {error}</p>}

      <div style={styles.card}>
        <div style={styles.cardTitle}>Create New Key</div>
        <form style={styles.form} onSubmit={handleCreate}>
          <div style={styles.formGroup}>
            <label style={styles.label}>Name</label>
            <input
              style={styles.input}
              type="text"
              value={newKeyName}
              onChange={(e) => setNewKeyName(e.target.value)}
              placeholder="Key name"
            />
          </div>
          <div style={styles.formGroup}>
            <label style={styles.label}>Rate Limit (req/hr)</label>
            <input
              style={styles.input}
              type="number"
              value={newKeyRateLimit}
              onChange={(e) => setNewKeyRateLimit(e.target.value)}
              placeholder="1000"
            />
          </div>
          <button style={styles.createButton} type="submit">
            Create Key
          </button>
        </form>

        {createdKey && (
          <div>
            <div style={styles.keyDisplay}>{createdKey}</div>
            <p style={styles.keyWarning}>
              Copy this key now. It will not be shown again.
            </p>
          </div>
        )}
      </div>

      <div style={styles.card}>
        <div style={styles.cardTitle}>Existing Keys</div>

        {keys.length === 0 ? (
          <p style={styles.empty}>No API keys created yet</p>
        ) : (
          <table style={styles.table}>
            <thead>
              <tr>
                <th style={styles.th}>Prefix</th>
                <th style={styles.th}>Name</th>
                <th style={styles.th}>Created</th>
                <th style={styles.th}>Last Used</th>
                <th style={styles.th}>Rate Limit</th>
                <th style={styles.th}>Status</th>
                <th style={styles.th}>Action</th>
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id}>
                  <td style={styles.td}><code>{k.key_prefix}...</code></td>
                  <td style={styles.td}>{k.name}</td>
                  <td style={styles.td}>{k.created_at}</td>
                  <td style={styles.td}>{k.last_used_at || 'Never'}</td>
                  <td style={styles.td}>{k.rate_limit}/hr</td>
                  <td style={styles.td}>
                    <span style={k.active ? styles.activeStatus : styles.inactiveStatus}>
                      {k.active ? 'Active' : 'Revoked'}
                    </span>
                  </td>
                  <td style={styles.td}>
                    {k.active && (
                      <button
                        style={styles.revokeButton}
                        onClick={() => handleRevoke(k.id)}
                      >
                        Revoke
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

export default APIKeys
