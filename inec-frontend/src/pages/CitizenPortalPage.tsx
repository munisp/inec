import { useState } from 'react';
import { api } from '../lib/api';


interface PartyScore {
  party_code: string;
  votes: number;
}

interface ResultEntry {
  id: number;
  polling_unit_code: string;
  pu_name?: string;
  total_votes: number;
  accredited_voters?: number;
  status: string;
  party_scores?: PartyScore[];
  cryptographically_verified?: boolean;
}

export default function CitizenPortalPage() {
  const [searchType, setSearchType] = useState<'pu_code' | 'state' | 'lga'>('pu_code');
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<ResultEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);

  const handleSearch = async () => {
    if (!query.trim()) return;
    setLoading(true);
    setSearched(true);
    try {
      const params: Record<string, string> = {};
      params[searchType] = query.trim();
      const res = await api.citizenVerify(params);
      setResults(res.results || []);
    } catch (e) {
      void 0;
      setResults([]);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="p-6 max-w-5xl mx-auto" role="main" aria-label="Citizen Result Verification Portal">
      <div className="text-center mb-8">
        <h1 className="text-3xl font-bold dark:text-white">Citizen Result Verification</h1>
        <p className="text-gray-500 dark:text-gray-400 mt-2">
          Verify election results for any polling unit in Nigeria — no login required
        </p>
      </div>

      {/* Search Form */}
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 mb-6">
        <div className="flex gap-4 mb-4">
          {(['pu_code', 'state', 'lga'] as const).map((t) => (
            <button key={t} onClick={() => setSearchType(t)}
              className={`px-4 py-2 rounded-lg font-medium text-sm ${searchType === t ? 'bg-green-600 text-white' : 'bg-gray-100 dark:bg-gray-700 dark:text-gray-300'}`}>
              {t === 'pu_code' ? 'Polling Unit Code' : t === 'state' ? 'State' : 'LGA'}
            </button>
          ))}
        </div>
        <div className="flex gap-2">
          <input type="text" value={query} onChange={(e) => setQuery(e.target.value)}
            placeholder={searchType === 'pu_code' ? 'e.g., PU-FCT-001' : searchType === 'state' ? 'e.g., FCT' : 'e.g., AMAC'}
            className="flex-1 border rounded-lg px-4 py-2 dark:bg-gray-700 dark:border-gray-600 dark:text-white"
            onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
            aria-label={`Search by ${searchType}`} />
          <button onClick={handleSearch} disabled={loading}
            className="bg-green-600 text-white px-6 py-2 rounded-lg font-medium hover:bg-green-700 disabled:opacity-50">
            {loading ? 'Searching...' : 'Verify'}
          </button>
        </div>
      </div>

      {/* Results */}
      {searched && (
        <div className="space-y-4">
          {results.length === 0 ? (
            <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-8 text-center">
              <p className="text-gray-500 dark:text-gray-400">No results found for this search</p>
            </div>
          ) : (
            results.map((r) => (
              <div key={r.id} className="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
                <div className="flex items-center justify-between mb-4">
                  <div>
                    <h3 className="font-bold dark:text-white">{r.pu_name || r.polling_unit_code}</h3>
                    <p className="text-sm text-gray-500 dark:text-gray-400">Code: {r.polling_unit_code}</p>
                  </div>
                  <div className="flex items-center gap-2">
                    {r.cryptographically_verified && (
                      <span className="bg-green-100 text-green-800 text-xs px-2 py-1 rounded-full font-medium">Cryptographically Verified</span>
                    )}
                    <span className={`text-xs px-2 py-1 rounded-full font-medium ${r.status === 'finalized' ? 'bg-green-100 text-green-800' : 'bg-yellow-100 text-yellow-800'}`}>
                      {r.status}
                    </span>
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-4 mb-4">
                  <div>
                    <p className="text-sm text-gray-500 dark:text-gray-400">Total Votes</p>
                    <p className="text-xl font-bold dark:text-white">{Number(r.total_votes).toLocaleString()}</p>
                  </div>
                  {r.accredited_voters && (
                    <div>
                      <p className="text-sm text-gray-500 dark:text-gray-400">Accredited Voters</p>
                      <p className="text-xl font-bold dark:text-white">{Number(r.accredited_voters).toLocaleString()}</p>
                    </div>
                  )}
                </div>
                {r.party_scores && r.party_scores.length > 0 && (
                  <div>
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-300 mb-2">Party Scores</p>
                    <div className="space-y-1">
                      {r.party_scores.map((ps) => {
                        const maxVotes = Math.max(...r.party_scores!.map((p) => Number(p.votes)));
                        const pct = maxVotes > 0 ? (Number(ps.votes) / maxVotes) * 100 : 0;
                        return (
                          <div key={ps.party_code} className="flex items-center gap-2">
                            <span className="w-12 text-xs font-medium dark:text-gray-300">{ps.party_code}</span>
                            <div className="flex-1 bg-gray-200 dark:bg-gray-600 rounded-full h-4">
                              <div className="bg-green-500 h-4 rounded-full" style={{ width: `${pct}%` }} />
                            </div>
                            <span className="w-16 text-right text-sm font-medium dark:text-gray-300">{Number(ps.votes).toLocaleString()}</span>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                )}
              </div>
            ))
          )}
        </div>
      )}

      <div className="mt-6 text-center text-xs text-gray-400 dark:text-gray-500">
        Results as submitted by INEC officials. For official certified results, visit{' '}
        <a href="https://www.inecnigeria.org" className="underline" target="_blank" rel="noreferrer">www.inecnigeria.org</a>
      </div>
    </div>
  );
}
