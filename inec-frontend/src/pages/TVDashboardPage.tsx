import { useState, useEffect } from 'react';
import { api } from '../lib/api';

interface PartyTotal {
  party: string;
  votes: number;
}

interface TVData {
  election_id: number;
  total_pus: number;
  reported_pus: number;
  completion_pct: number;
  total_votes: number;
  party_totals: PartyTotal[];
  state_results: Record<string, PartyTotal[]>;
  last_updated: string;
}

const partyColors: Record<string, string> = {
  APC: '#2563eb', PDP: '#dc2626', LP: '#16a34a', NNPP: '#d97706',
  APGA: '#7c3aed', SDP: '#0891b2', ADC: '#db2777', YPP: '#65a30d',
};

export default function TVDashboardPage() {
  const [data, setData] = useState<TVData | null>(null);
  const [cycleIdx, setCycleIdx] = useState(0);

  useEffect(() => {
    const load = () => api.getTVDashboard(1).then(setData).catch(err => console.error("API error:", err));
    load();
    const interval = setInterval(load, 10000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    const interval = setInterval(() => setCycleIdx(i => i + 1), 8000);
    return () => clearInterval(interval);
  }, []);

  if (!data) return <div className="flex items-center justify-center h-screen bg-gray-950 text-white text-4xl" role="status" aria-label="Loading dashboard">Loading Election Dashboard...</div>;

  const states = Object.keys(data.state_results || {});
  const currentState = states.length > 0 ? states[cycleIdx % states.length] : '';
  const maxVotes = data.party_totals.length > 0 ? data.party_totals[0].votes : 1;

  return (
    <div className="min-h-screen bg-gray-950 text-white p-6" role="main" aria-label="Public election TV dashboard">
      <header className="text-center mb-8" role="banner">
        <h1 className="text-5xl font-bold tracking-tight">INEC Election Results</h1>
        <p className="text-gray-400 mt-2 text-lg">Live Results Dashboard — Last updated: {new Date(data.last_updated).toLocaleTimeString()}</p>
      </header>

      <div className="grid grid-cols-3 gap-4 mb-8" role="region" aria-label="Summary statistics">
        <div className="bg-gray-800 rounded-xl p-6 text-center">
          <p className="text-gray-400 text-sm">Total Polling Units</p>
          <p className="text-4xl font-bold text-blue-400">{data.total_pus.toLocaleString()}</p>
        </div>
        <div className="bg-gray-800 rounded-xl p-6 text-center">
          <p className="text-gray-400 text-sm">Units Reporting</p>
          <p className="text-4xl font-bold text-green-400">{data.reported_pus.toLocaleString()}</p>
        </div>
        <div className="bg-gray-800 rounded-xl p-6 text-center">
          <p className="text-gray-400 text-sm">Completion</p>
          <p className="text-4xl font-bold text-yellow-400">{data.completion_pct.toFixed(1)}%</p>
        </div>
      </div>

      <div className="w-full bg-gray-800 rounded-full h-4 mb-8 overflow-hidden" role="progressbar" aria-valuenow={data.completion_pct} aria-valuemin={0} aria-valuemax={100} aria-label="Result reporting progress">
        <div className="h-full rounded-full bg-gradient-to-r from-green-500 to-green-300 transition-all duration-1000" style={{ width: `${data.completion_pct}%` }} />
      </div>

      <div className="grid grid-cols-2 gap-8">
        <section aria-label="National party totals">
          <h2 className="text-2xl font-bold mb-4">National Results</h2>
          <div className="space-y-3">
            {data.party_totals.map(p => (
              <div key={p.party} className="flex items-center gap-3">
                <span className="w-16 text-right font-bold text-lg" style={{ color: partyColors[p.party] || '#9ca3af' }}>{p.party}</span>
                <div className="flex-1 bg-gray-800 rounded-full h-8 overflow-hidden">
                  <div className="h-full rounded-full transition-all duration-1000 flex items-center px-3 text-sm font-bold"
                    style={{ width: `${(p.votes / maxVotes) * 100}%`, backgroundColor: partyColors[p.party] || '#6b7280' }}>
                    {p.votes.toLocaleString()}
                  </div>
                </div>
              </div>
            ))}
          </div>
          <p className="text-gray-400 mt-4 text-lg">Total Votes: <span className="text-white font-bold">{data.total_votes.toLocaleString()}</span></p>
        </section>

        <section aria-label="State-level results">
          <h2 className="text-2xl font-bold mb-4">State: {currentState || 'N/A'}</h2>
          {currentState && data.state_results[currentState] && (
            <div className="space-y-2">
              {data.state_results[currentState].slice(0, 6).map(p => {
                const stateMax = data.state_results[currentState][0]?.votes || 1;
                return (
                  <div key={p.party} className="flex items-center gap-3">
                    <span className="w-16 text-right font-semibold" style={{ color: partyColors[p.party] || '#9ca3af' }}>{p.party}</span>
                    <div className="flex-1 bg-gray-800 rounded-full h-6 overflow-hidden">
                      <div className="h-full rounded-full transition-all duration-1000 flex items-center px-2 text-xs font-bold"
                        style={{ width: `${(p.votes / stateMax) * 100}%`, backgroundColor: partyColors[p.party] || '#6b7280' }}>
                        {p.votes.toLocaleString()}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
          <div className="flex gap-2 mt-4 flex-wrap">
            {states.slice(0, 12).map((s, i) => (
              <button key={s} onClick={() => setCycleIdx(i)}
                className={`px-3 py-1 rounded text-sm ${i === cycleIdx % states.length ? 'bg-blue-600 text-white' : 'bg-gray-800 text-gray-400 hover:bg-gray-700'}`}
                aria-label={`Show results for ${s}`}>{s}</button>
            ))}
          </div>
        </section>
      </div>
    </div>
  );
}
