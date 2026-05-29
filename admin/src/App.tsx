import React, { useEffect, useState } from 'react'
import { Chart as ChartJS, ArcElement, Tooltip, Legend } from 'chart.js'
import { Doughnut } from 'react-chartjs-2'
import MLTuning from './components/MLTuning'
import SubnetBans from './components/SubnetBans'
import APIKeys from './components/APIKeys'
import LiveTraffic from './components/LiveTraffic'

ChartJS.register(ArcElement, Tooltip, Legend)

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080'

interface AsnRow {
  asn: string
  block_count: number
}

interface SplitData {
  allow_count: number
  block_count: number
}

async function queryClickHouse<T>(sql: string): Promise<T[]> {
  const response = await fetch(`${API_URL}/api/query`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query: sql }),
  })

  if (!response.ok) {
    const err = await response.text()
    throw new Error(`Query failed: ${response.status} ${err}`)
  }

  return response.json()
}

type TabName = 'dashboard' | 'ml-tuning' | 'subnet-bans' | 'api-keys' | 'live-traffic'

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
    marginBottom: '1rem',
  },
  tabBar: {
    display: 'flex',
    gap: '0',
    borderBottom: '2px solid #e5e7eb',
    marginBottom: '1.5rem',
  },
  tab: {
    padding: '0.75rem 1.25rem',
    fontSize: '0.875rem',
    fontWeight: 500,
    color: '#6b7280',
    background: 'transparent',
    border: 'none',
    borderBottom: '2px solid transparent',
    marginBottom: '-2px',
    cursor: 'pointer',
  },
  tabActive: {
    padding: '0.75rem 1.25rem',
    fontSize: '0.875rem',
    fontWeight: 600,
    color: '#3b82f6',
    background: 'transparent',
    border: 'none',
    borderBottom: '2px solid #3b82f6',
    marginBottom: '-2px',
    cursor: 'pointer',
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

const tabs: { id: TabName; label: string }[] = [
  { id: 'dashboard', label: 'Dashboard' },
  { id: 'ml-tuning', label: 'ML Tuning' },
  { id: 'subnet-bans', label: 'Subnet Bans' },
  { id: 'api-keys', label: 'API Keys' },
  { id: 'live-traffic', label: 'Live Traffic' },
]

function Dashboard() {
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
            "SELECT count() as cnt FROM ghostroute.visits WHERE toDate(event_time) = today()"
          ),
          queryClickHouse<{ decision: string; cnt: number }>(
            "SELECT decision, count() as cnt FROM ghostroute.visits WHERE toDate(event_time) = today() GROUP BY decision"
          ),
          queryClickHouse<{ asn: string; block_count: number }>(
            "SELECT asn_name as asn, count() as block_count FROM ghostroute.visits WHERE toDate(event_time) = today() AND decision = 'block' GROUP BY asn_name ORDER BY block_count DESC LIMIT 10"
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
    <>
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
    </>
  )
}

function App() {
  const [activeTab, setActiveTab] = useState<TabName>('dashboard')

  function renderContent() {
    switch (activeTab) {
      case 'dashboard':
        return <Dashboard />
      case 'ml-tuning':
        return <MLTuning />
      case 'subnet-bans':
        return <SubnetBans />
      case 'api-keys':
        return <APIKeys />
      case 'live-traffic':
        return <LiveTraffic />
      default:
        return <Dashboard />
    }
  }

  return (
    <div style={styles.container}>
      <header style={styles.header}>
        <h1 style={styles.title}>GhostRoute Admin</h1>
      </header>

      <div style={styles.tabBar}>
        {tabs.map((tab) => (
          <button
            key={tab.id}
            style={activeTab === tab.id ? styles.tabActive : styles.tab}
            onClick={() => setActiveTab(tab.id)}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {renderContent()}
    </div>
  )
}

export default App
