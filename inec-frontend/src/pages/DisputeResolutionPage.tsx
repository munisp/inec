import React, { useState, useEffect, useCallback } from 'react';

interface Dispute {
  id: number;
  election_id: number;
  polling_unit_code: string;
  filed_by: string;
  party: string;
  category: string;
  description: string;
  evidence: string[];
  status: string;
  assigned_to: string;
  resolution: string;
  resolved_by: string;
  filed_at: string;
  resolved_at: string;
  priority: string;
}

interface DisputeStats {
  total: number;
  by_status: Record<string, number>;
  by_priority: Record<string, number>;
  categories: string[];
}

const statusColors: Record<string, string> = {
  filed: '#ef4444',
  under_review: '#f59e0b',
  escalated: '#8b5cf6',
  resolved: '#22c55e',
  dismissed: '#6b7280',
};

const priorityColors: Record<string, string> = {
  high: '#ef4444',
  medium: '#f59e0b',
  low: '#22c55e',
};

const API_BASE = '';

export default function DisputeResolutionPage() {
  const [disputes, setDisputes] = useState<Dispute[]>([]);
  const [stats, setStats] = useState<DisputeStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filterStatus, setFilterStatus] = useState('');
  const [filterPriority, setFilterPriority] = useState('');
  const [showFileForm, setShowFileForm] = useState(false);

  // Form state
  const [formElectionId, setFormElectionId] = useState('');
  const [formPuCode, setFormPuCode] = useState('');
  const [formParty, setFormParty] = useState('');
  const [formCategory, setFormCategory] = useState('overvoting');
  const [formDescription, setFormDescription] = useState('');
  const [submitLoading, setSubmitLoading] = useState(false);



  const fetchDisputes = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const params = new URLSearchParams();
      if (filterStatus) params.set('status', filterStatus);
      if (filterPriority) params.set('priority', filterPriority);

      const res = await fetch(`${API_BASE}/disputes?${params}`, {
        credentials: 'include',
      });
      if (!res.ok) throw new Error(`Failed to fetch disputes: ${res.status}`);
      const data = await res.json();
      setDisputes(data.disputes || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load disputes');
    } finally {
      setLoading(false);
    }
  }, [filterStatus, filterPriority]);

  const fetchStats = useCallback(async () => {
    try {
      const res = await fetch(`${API_BASE}/disputes/stats`, {
        credentials: 'include',
      });
      if (res.ok) {
        const data = await res.json();
        setStats(data);
      }
    } catch {
      // Stats are non-critical
    }
  }, []);

  useEffect(() => {
    fetchDisputes();
    fetchStats();
  }, [fetchDisputes, fetchStats]);

  const handleFileDispute = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitLoading(true);
    try {
      const res = await fetch(`${API_BASE}/disputes`, {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          election_id: parseInt(formElectionId),
          polling_unit_code: formPuCode,
          party: formParty,
          category: formCategory,
          description: formDescription,
          evidence: [],
        }),
      });
      if (!res.ok) {
        const data = await res.json();
        throw new Error(data.detail || 'Failed to file dispute');
      }
      setShowFileForm(false);
      setFormDescription('');
      fetchDisputes();
      fetchStats();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to file dispute');
    } finally {
      setSubmitLoading(false);
    }
  };

  const handleResolve = async (disputeId: number, action: string) => {
    const resolution = action === 'resolve' || action === 'dismiss'
      ? prompt(`Enter resolution for ${action}:`)
      : undefined;
    if ((action === 'resolve' || action === 'dismiss') && !resolution) return;

    try {
      const res = await fetch(`${API_BASE}/disputes/${disputeId}/resolve`, {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ action, resolution }),
      });
      if (!res.ok) {
        const data = await res.json();
        throw new Error(data.detail || `Failed to ${action} dispute`);
      }
      fetchDisputes();
      fetchStats();
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${action} dispute`);
    }
  };

  return (
    <div style={{ padding: '24px', maxWidth: '1200px', margin: '0 auto' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '24px' }}>
        <h1 style={{ fontSize: '24px', fontWeight: 'bold' }}>Dispute Resolution</h1>
        <button
          onClick={() => setShowFileForm(!showFileForm)}
          style={{
            padding: '8px 16px', backgroundColor: '#1d4ed8', color: 'white',
            border: 'none', borderRadius: '6px', cursor: 'pointer',
          }}
          aria-label="File new dispute"
        >
          + File Dispute
        </button>
      </div>

      {/* Stats Summary */}
      {stats && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: '16px', marginBottom: '24px' }}>
          <div style={{ padding: '16px', backgroundColor: '#f8fafc', borderRadius: '8px', border: '1px solid #e2e8f0' }}>
            <div style={{ fontSize: '28px', fontWeight: 'bold' }}>{stats.total}</div>
            <div style={{ color: '#64748b', fontSize: '14px' }}>Total Disputes</div>
          </div>
          {Object.entries(stats.by_status).map(([status, count]) => (
            <div key={status} style={{
              padding: '16px', backgroundColor: '#f8fafc', borderRadius: '8px',
              border: `2px solid ${statusColors[status] || '#e2e8f0'}`,
            }}>
              <div style={{ fontSize: '28px', fontWeight: 'bold' }}>{count}</div>
              <div style={{ color: '#64748b', fontSize: '14px', textTransform: 'capitalize' }}>
                {status.replace('_', ' ')}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Filters */}
      <div style={{ display: 'flex', gap: '12px', marginBottom: '16px' }}>
        <select
          value={filterStatus}
          onChange={(e) => setFilterStatus(e.target.value)}
          style={{ padding: '8px', borderRadius: '6px', border: '1px solid #d1d5db' }}
          aria-label="Filter by status"
        >
          <option value="">All Statuses</option>
          <option value="filed">Filed</option>
          <option value="under_review">Under Review</option>
          <option value="escalated">Escalated</option>
          <option value="resolved">Resolved</option>
          <option value="dismissed">Dismissed</option>
        </select>
        <select
          value={filterPriority}
          onChange={(e) => setFilterPriority(e.target.value)}
          style={{ padding: '8px', borderRadius: '6px', border: '1px solid #d1d5db' }}
          aria-label="Filter by priority"
        >
          <option value="">All Priorities</option>
          <option value="high">High</option>
          <option value="medium">Medium</option>
          <option value="low">Low</option>
        </select>
      </div>

      {/* File Dispute Form */}
      {showFileForm && (
        <form onSubmit={handleFileDispute} style={{
          padding: '20px', backgroundColor: '#f8fafc', borderRadius: '8px',
          border: '1px solid #e2e8f0', marginBottom: '24px',
        }}>
          <h2 style={{ fontSize: '18px', marginBottom: '16px' }}>File New Dispute</h2>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px', marginBottom: '12px' }}>
            <input
              type="number" placeholder="Election ID" required
              value={formElectionId} onChange={(e) => setFormElectionId(e.target.value)}
              style={{ padding: '8px', borderRadius: '6px', border: '1px solid #d1d5db' }}
              aria-label="Election ID"
            />
            <input
              type="text" placeholder="Polling Unit Code"
              value={formPuCode} onChange={(e) => setFormPuCode(e.target.value)}
              style={{ padding: '8px', borderRadius: '6px', border: '1px solid #d1d5db' }}
              aria-label="Polling unit code"
            />
            <input
              type="text" placeholder="Party (e.g., APC)"
              value={formParty} onChange={(e) => setFormParty(e.target.value)}
              style={{ padding: '8px', borderRadius: '6px', border: '1px solid #d1d5db' }}
              aria-label="Party"
            />
            <select
              value={formCategory} onChange={(e) => setFormCategory(e.target.value)}
              style={{ padding: '8px', borderRadius: '6px', border: '1px solid #d1d5db' }}
              aria-label="Dispute category"
            >
              <option value="overvoting">Overvoting</option>
              <option value="ballot_stuffing">Ballot Stuffing</option>
              <option value="voter_intimidation">Voter Intimidation</option>
              <option value="result_falsification">Result Falsification</option>
              <option value="unauthorized_persons">Unauthorized Persons</option>
              <option value="voting_machine_tampering">Voting Machine Tampering</option>
              <option value="multiple_voting">Multiple Voting</option>
              <option value="missing_results">Missing Results</option>
              <option value="procedural_violation">Procedural Violation</option>
              <option value="other">Other</option>
            </select>
          </div>
          <textarea
            placeholder="Describe the dispute in detail..."
            required rows={4}
            value={formDescription} onChange={(e) => setFormDescription(e.target.value)}
            style={{ width: '100%', padding: '8px', borderRadius: '6px', border: '1px solid #d1d5db', marginBottom: '12px' }}
            aria-label="Dispute description"
          />
          <div style={{ display: 'flex', gap: '8px' }}>
            <button
              type="submit" disabled={submitLoading}
              style={{
                padding: '8px 16px', backgroundColor: '#dc2626', color: 'white',
                border: 'none', borderRadius: '6px', cursor: 'pointer', opacity: submitLoading ? 0.5 : 1,
              }}
            >
              {submitLoading ? 'Filing...' : 'File Dispute'}
            </button>
            <button
              type="button" onClick={() => setShowFileForm(false)}
              style={{ padding: '8px 16px', backgroundColor: '#6b7280', color: 'white', border: 'none', borderRadius: '6px', cursor: 'pointer' }}
            >
              Cancel
            </button>
          </div>
        </form>
      )}

      {/* Error */}
      {error && (
        <div role="alert" style={{
          padding: '12px', backgroundColor: '#fef2f2', color: '#dc2626',
          borderRadius: '6px', marginBottom: '16px', border: '1px solid #fecaca',
        }}>
          {error}
        </div>
      )}

      {/* Loading */}
      {loading && <p style={{ textAlign: 'center', color: '#64748b' }}>Loading disputes...</p>}

      {/* Disputes Table */}
      {!loading && (
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ backgroundColor: '#f1f5f9', textAlign: 'left' }}>
              <th style={{ padding: '12px', borderBottom: '2px solid #e2e8f0' }}>ID</th>
              <th style={{ padding: '12px', borderBottom: '2px solid #e2e8f0' }}>Category</th>
              <th style={{ padding: '12px', borderBottom: '2px solid #e2e8f0' }}>Priority</th>
              <th style={{ padding: '12px', borderBottom: '2px solid #e2e8f0' }}>Status</th>
              <th style={{ padding: '12px', borderBottom: '2px solid #e2e8f0' }}>Filed By</th>
              <th style={{ padding: '12px', borderBottom: '2px solid #e2e8f0' }}>Filed At</th>
              <th style={{ padding: '12px', borderBottom: '2px solid #e2e8f0' }}>Actions</th>
            </tr>
          </thead>
          <tbody>
            {disputes.length === 0 && (
              <tr><td colSpan={7} style={{ padding: '24px', textAlign: 'center', color: '#94a3b8' }}>No disputes found</td></tr>
            )}
            {disputes.map((d) => (
              <tr key={d.id} style={{ borderBottom: '1px solid #e2e8f0' }}>
                <td style={{ padding: '12px' }}>#{d.id}</td>
                <td style={{ padding: '12px', textTransform: 'capitalize' }}>{d.category.replace('_', ' ')}</td>
                <td style={{ padding: '12px' }}>
                  <span style={{
                    padding: '2px 8px', borderRadius: '12px', fontSize: '12px',
                    backgroundColor: `${priorityColors[d.priority]}20`, color: priorityColors[d.priority],
                  }}>
                    {d.priority}
                  </span>
                </td>
                <td style={{ padding: '12px' }}>
                  <span style={{
                    padding: '2px 8px', borderRadius: '12px', fontSize: '12px',
                    backgroundColor: `${statusColors[d.status]}20`, color: statusColors[d.status],
                  }}>
                    {d.status.replace('_', ' ')}
                  </span>
                </td>
                <td style={{ padding: '12px' }}>{d.filed_by}</td>
                <td style={{ padding: '12px', fontSize: '13px', color: '#64748b' }}>{d.filed_at}</td>
                <td style={{ padding: '12px' }}>
                  <div style={{ display: 'flex', gap: '4px', flexWrap: 'wrap' }}>
                    {d.status === 'filed' && (
                      <button
                        onClick={() => handleResolve(d.id, 'review')}
                        style={{ padding: '4px 8px', fontSize: '12px', backgroundColor: '#3b82f6', color: 'white', border: 'none', borderRadius: '4px', cursor: 'pointer' }}
                      >
                        Review
                      </button>
                    )}
                    {(d.status === 'filed' || d.status === 'under_review') && (
                      <button
                        onClick={() => handleResolve(d.id, 'escalate')}
                        style={{ padding: '4px 8px', fontSize: '12px', backgroundColor: '#8b5cf6', color: 'white', border: 'none', borderRadius: '4px', cursor: 'pointer' }}
                      >
                        Escalate
                      </button>
                    )}
                    {d.status !== 'resolved' && d.status !== 'dismissed' && (
                      <>
                        <button
                          onClick={() => handleResolve(d.id, 'resolve')}
                          style={{ padding: '4px 8px', fontSize: '12px', backgroundColor: '#22c55e', color: 'white', border: 'none', borderRadius: '4px', cursor: 'pointer' }}
                        >
                          Resolve
                        </button>
                        <button
                          onClick={() => handleResolve(d.id, 'dismiss')}
                          style={{ padding: '4px 8px', fontSize: '12px', backgroundColor: '#6b7280', color: 'white', border: 'none', borderRadius: '4px', cursor: 'pointer' }}
                        >
                          Dismiss
                        </button>
                      </>
                    )}
                    {d.resolution && (
                      <span style={{ fontSize: '12px', color: '#64748b', fontStyle: 'italic' }}>
                        {d.resolution.substring(0, 50)}...
                      </span>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
