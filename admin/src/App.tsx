import React, { useEffect, useState } from 'react'
import { Chart as ChartJS, ArcElement, Tooltip, Legend } from 'chart.js'
import { Doughnut } from 'react-chartjs-2'

ChartJS.register(ArcElement, Tooltip, Legend)

const CLICKHOUSE_URL = import.meta.env.VITE_CLICKHOUSE_URL || 'http://localhost:8123'

interface AsnRow {
  asn: string
  block_count: number
}

interface SplitData {
  allow_count: number
  block_count: number
}

async function queryClickHouse<T>(sql: string): Promise<T[]> {
  const response = await fetch(CLICKHOUSE_URL, {
    method: 'POST',
    body: `${sql} FORMAT JSONEachRow`,
    headers: { 'Content-Type': 'text/plain' },
  })

  if (!response.ok) {
    throw new Error(`ClickHouse query failed: ${response.status}`)
  }

  const text = await response.text()
  if (!text.trim()) return []

  return text
    .trim()
    .split('\n')
    .map((line) => JSON.parse(line) as T)
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
    maxWidth: '1100px',
    margin: '0 auto',
    padding: '2rem',
    color: '#1f2937',
  },
  header: {
    marginBottom: '2rem',
    borderBottom: '1px solid #e5e7eb',
    paddingBottom: '1rem',
  },
  title: {
    fontSize: '1.75rem',
    fontWeight: 700,
    color: '#111827',
  },
  grid: {
    display: 'grid',
    gridTemplateColumns: '1fr 1fr',
    gap: '1.5rem',
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
    marginBottom: '0.5rem',
  },
  bigNumber: {
    fontSize: '2.5rem',
    fontWeight: 700,
    color: '#111827',
  },
  chartContainer: {
    maxWidth: '300px',
    margin: '0 auto',
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
  loading: {
    color: '#6b7280',
    fontStyle: 'italic',
  },
  error: {
    color: '#dc2626',
    fontSize: '0.875rem',
  },
}

function App() {
  const [visitsToday, setVisitsToday] = useState<number | null>(null)
  const [splitData, setSplitData] = useState<SplitData | null>(null)
  const [topAsns, setTopAsns] = useState<AsnRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    async function fetchData() {
      try {
        setLoading(true)
        setError(null)

        const [visitsResult, splitResult, asnResult] = await Promise.all([
          queryClickHouse<{ cnt: number }>(
            "SELECT count() as cnt FROM ghostroute.clicks WHERE toDate(timestamp) = today()"
          ),
          queryClickHouse<{ decision: string; cnt: number }>(
            "SELECT decision, count() as cnt FROM ghostroute.clicks WHERE toDate(timestamp) = today() GROUP BY decision"
          ),
          queryClickHouse<{ asn: string; block_count: number }>(
            "SELECT asn, count() as block_count FROM ghostroute.clicks WHERE toDate(timestamp) = today() AND decision = 'block' GROUP BY asn ORDER BY block_count DESC LIMIT 10"
          ),
        ])

        setVisitsToday(visitsResult.length > 0 ? visitsResult[0].cnt : 0)

        const allow = splitResult.find((r) => r.decision === 'allow')?.cnt || 0
        const block = splitResult.find((r) => r.decision === 'block')?.cnt || 0
        setSplitData({ allow_count: allow, block_count: block })

        setTopAsns(asnResult)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch data')
      } finally {
        setLoading(false)
      }
    }

    fetchData()
  }, [])

  const doughnutData = {
    labels: ['Allow (Black Page)', 'Block (White Page)'],
    datasets: [
      {
        data: [splitData?.allow_count || 0, splitData?.block_count || 0],
        backgroundColor: ['#10b981', '#ef4444'],
        borderColor: ['#059669', '#dc2626'],
        borderWidth: 1,
      },
    ],
  }

  const doughnutOptions = {
    responsive: true,
    plugins: {
      legend: {
        position: 'bottom' as const,
      },
    },
  }

  return (
    <div style={styles.container}>
      <header style={styles.header}>
        <h1 style={styles.title}>GhostRoute Admin</h1>
      </header>

      {error && <p style={styles.error}>Error: {error}</p>}

      <div style={styles.grid}>
        <div style={styles.card}>
          <div style={styles.cardTitle}>Visits Today</div>
          {loading ? (
            <p style={styles.loading}>Loading...</p>
          ) : (
            <div style={styles.bigNumber}>{visitsToday?.toLocaleString() ?? '0'}</div>
          )}
        </div>

        <div style={styles.card}>
          <div style={styles.cardTitle}>Black / White Split</div>
          {loading ? (
            <p style={styles.loading}>Loading...</p>
          ) : (
            <div style={styles.chartContainer}>
              <Doughnut data={doughnutData} options={doughnutOptions} />
            </div>
          )}
        </div>
      </div>

      <div style={styles.card}>
        <div style={styles.cardTitle}>Top Blocked ASNs</div>
        {loading ? (
          <p style={styles.loading}>Loading...</p>
        ) : topAsns.length === 0 ? (
          <p style={styles.loading}>No blocked ASN data available</p>
        ) : (
          <table style={styles.table}>
            <thead>
              <tr>
                <th style={styles.th}>#</th>
                <th style={styles.th}>ASN</th>
                <th style={styles.th}>Block Count</th>
              </tr>
            </thead>
            <tbody>
              {topAsns.map((row, index) => (
                <tr key={row.asn}>
                  <td style={styles.td}>{index + 1}</td>
                  <td style={styles.td}>{row.asn}</td>
                  <td style={styles.td}>{row.block_count.toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

export default App
