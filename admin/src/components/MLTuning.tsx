import React, { useEffect, useState } from 'react'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080'

interface ModelInfo {
  version: string
  auc_roc: number
  auc_pr: number
  precision: number
  recall: number
  trained_at: string
}

interface ModelConfig {
  vector_similarity_weight: number
  xgboost_weight: number
  bot_threshold: number
  ab_test_enabled: boolean
  ab_test_split: number
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
    marginBottom: '1rem',
  },
  metricRow: {
    display: 'flex',
    justifyContent: 'space-between',
    padding: '0.5rem 0',
    borderBottom: '1px solid #f3f4f6',
  },
  metricLabel: {
    fontSize: '0.9rem',
    color: '#374151',
  },
  metricValue: {
    fontSize: '0.9rem',
    fontWeight: 600,
    color: '#111827',
  },
  sliderGroup: {
    marginBottom: '1.25rem',
  },
  sliderLabel: {
    display: 'flex',
    justifyContent: 'space-between',
    fontSize: '0.875rem',
    color: '#374151',
    marginBottom: '0.5rem',
  },
  slider: {
    width: '100%',
    cursor: 'pointer',
  },
  button: {
    padding: '0.5rem 1rem',
    background: '#3b82f6',
    color: '#fff',
    border: 'none',
    borderRadius: '6px',
    fontSize: '0.875rem',
    fontWeight: 500,
    cursor: 'pointer',
    marginTop: '1rem',
  },
  checkbox: {
    marginRight: '0.5rem',
  },
  checkboxLabel: {
    fontSize: '0.875rem',
    color: '#374151',
  },
  status: {
    fontSize: '0.8rem',
    marginTop: '0.75rem',
    color: '#059669',
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

function MLTuning() {
  const [modelInfo, setModelInfo] = useState<ModelInfo | null>(null)
  const [config, setConfig] = useState<ModelConfig>({
    vector_similarity_weight: 0.6,
    xgboost_weight: 0.4,
    bot_threshold: 0.7,
    ab_test_enabled: false,
    ab_test_split: 50,
  })
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saveStatus, setSaveStatus] = useState<string | null>(null)

  useEffect(() => {
    fetchModelInfo()
  }, [])

  async function fetchModelInfo() {
    try {
      setLoading(true)
      setError(null)
      const res = await fetch(`${API_URL}/api/admin/model/info`)
      if (!res.ok) throw new Error(`Failed to fetch model info: ${res.status}`)
      const data = await res.json()
      setModelInfo(data)
      if (data.config) {
        setConfig(data.config)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch model info')
    } finally {
      setLoading(false)
    }
  }

  function handleVectorWeight(value: number) {
    setConfig({
      ...config,
      vector_similarity_weight: value,
      xgboost_weight: Math.round((1 - value) * 100) / 100,
    })
  }

  async function saveConfig() {
    try {
      setSaveStatus(null)
      setError(null)
      const res = await fetch(`${API_URL}/api/admin/model/config`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(config),
      })
      if (!res.ok) throw new Error(`Failed to save config: ${res.status}`)
      setSaveStatus('Configuration saved successfully')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save config')
    }
  }

  if (loading) return <div style={styles.container}><p style={styles.loading}>Loading model info...</p></div>

  return (
    <div style={styles.container}>
      <h2 style={styles.title}>ML Model Tuning</h2>

      {error && <p style={styles.error}>Error: {error}</p>}

      <div style={styles.grid}>
        <div style={styles.card}>
          <div style={styles.cardTitle}>Model Information</div>
          {modelInfo ? (
            <>
              <div style={styles.metricRow}>
                <span style={styles.metricLabel}>Version</span>
                <span style={styles.metricValue}>{modelInfo.version}</span>
              </div>
              <div style={styles.metricRow}>
                <span style={styles.metricLabel}>AUC-ROC</span>
                <span style={styles.metricValue}>{modelInfo.auc_roc.toFixed(4)}</span>
              </div>
              <div style={styles.metricRow}>
                <span style={styles.metricLabel}>AUC-PR</span>
                <span style={styles.metricValue}>{modelInfo.auc_pr.toFixed(4)}</span>
              </div>
              <div style={styles.metricRow}>
                <span style={styles.metricLabel}>Precision</span>
                <span style={styles.metricValue}>{modelInfo.precision.toFixed(4)}</span>
              </div>
              <div style={styles.metricRow}>
                <span style={styles.metricLabel}>Recall</span>
                <span style={styles.metricValue}>{modelInfo.recall.toFixed(4)}</span>
              </div>
              <div style={styles.metricRow}>
                <span style={styles.metricLabel}>Trained At</span>
                <span style={styles.metricValue}>{modelInfo.trained_at}</span>
              </div>
            </>
          ) : (
            <p style={styles.loading}>No model info available</p>
          )}
        </div>

        <div style={styles.card}>
          <div style={styles.cardTitle}>Feature Weights</div>

          <div style={styles.sliderGroup}>
            <div style={styles.sliderLabel}>
              <span>Vector Similarity</span>
              <span>{config.vector_similarity_weight.toFixed(2)}</span>
            </div>
            <input
              type="range"
              min="0"
              max="1"
              step="0.05"
              value={config.vector_similarity_weight}
              onChange={(e) => handleVectorWeight(parseFloat(e.target.value))}
              style={styles.slider}
            />
          </div>

          <div style={styles.sliderGroup}>
            <div style={styles.sliderLabel}>
              <span>XGBoost</span>
              <span>{config.xgboost_weight.toFixed(2)}</span>
            </div>
            <input
              type="range"
              min="0"
              max="1"
              step="0.05"
              value={config.xgboost_weight}
              disabled
              style={styles.slider}
            />
          </div>

          <div style={styles.sliderGroup}>
            <div style={styles.sliderLabel}>
              <span>Bot Threshold</span>
              <span>{config.bot_threshold.toFixed(2)}</span>
            </div>
            <input
              type="range"
              min="0"
              max="1"
              step="0.05"
              value={config.bot_threshold}
              onChange={(e) => setConfig({ ...config, bot_threshold: parseFloat(e.target.value) })}
              style={styles.slider}
            />
          </div>
        </div>
      </div>

      <div style={styles.card}>
        <div style={styles.cardTitle}>A/B Test Configuration</div>
        <label style={styles.checkboxLabel}>
          <input
            type="checkbox"
            checked={config.ab_test_enabled}
            onChange={(e) => setConfig({ ...config, ab_test_enabled: e.target.checked })}
            style={styles.checkbox}
          />
          Enable A/B Testing
        </label>

        {config.ab_test_enabled && (
          <div style={{ ...styles.sliderGroup, marginTop: '1rem' }}>
            <div style={styles.sliderLabel}>
              <span>Traffic Split (Model A)</span>
              <span>{config.ab_test_split}%</span>
            </div>
            <input
              type="range"
              min="10"
              max="90"
              step="5"
              value={config.ab_test_split}
              onChange={(e) => setConfig({ ...config, ab_test_split: parseInt(e.target.value) })}
              style={styles.slider}
            />
          </div>
        )}

        <button style={styles.button} onClick={saveConfig}>
          Save Configuration
        </button>
        {saveStatus && <p style={styles.status}>{saveStatus}</p>}
      </div>
    </div>
  )
}

export default MLTuning
