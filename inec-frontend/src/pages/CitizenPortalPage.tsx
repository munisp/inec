import { useState } from 'react';
import { api } from '../lib/api';
import { useI18n } from '../lib/i18n';


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

// Public incident types must mirror the backend whitelist in
// inec-go-backend/public_incidents.go (publicIncidentTypes).
const INCIDENT_TYPES = [
  'violence',
  'ballot_box_snatching',
  'vote_buying',
  'voter_intimidation',
  'missing_materials',
  'late_opening',
  'overvoting',
  'result_falsification',
  'accessibility_barrier',
  'other',
] as const;

const API_URL = import.meta.env.VITE_API_URL ?? '';

// PublicComplaintForm is the no-auth-wall voter complaint channel (R5-073).
// It posts directly to /public/incidents (rate-limited, captcha-gated when
// the backend is configured for it). Reporter contact is optional.
function PublicComplaintForm() {
  const { t } = useI18n();
  const [incidentType, setIncidentType] = useState('');
  const [description, setDescription] = useState('');
  const [puCode, setPuCode] = useState('');
  const [contact, setContact] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [reference, setReference] = useState('');
  const [error, setError] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setReference('');
    setSubmitting(true);
    try {
      const body: Record<string, string> = {
        incident_type: incidentType,
        description: description.trim(),
      };
      if (puCode.trim()) body.polling_unit_code = puCode.trim();
      if (contact.trim()) {
        // Reporter contact is optional; route to the right field by shape.
        if (contact.includes('@')) body.reporter_email = contact.trim();
        else body.reporter_phone = contact.trim();
      }
      const res = await fetch(`${API_URL}/public/incidents`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        throw new Error((data as { detail?: string; error?: string }).detail || (data as { error?: string }).error || `HTTP ${res.status}`);
      }
      setReference((data as { reference?: string }).reference || '');
      setIncidentType('');
      setDescription('');
      setPuCode('');
      setContact('');
    } catch {
      setError(t('incident_error'));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 mb-6" aria-labelledby="public-incident-heading">
      <h2 id="public-incident-heading" className="text-xl font-bold dark:text-white mb-1">{t('incident_title')}</h2>
      <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">{t('incident_subtitle')}</p>
      <form onSubmit={handleSubmit} className="space-y-4">
        {error && (
          <div role="alert" className="p-3 text-sm text-red-700 bg-red-50 rounded-lg border border-red-200 dark:bg-red-900/30 dark:text-red-300 dark:border-red-800">{error}</div>
        )}
        {reference && (
          <div role="status" className="p-3 text-sm text-green-700 bg-green-50 rounded-lg border border-green-200 dark:bg-green-900/30 dark:text-green-300 dark:border-green-800">
            {t('incident_success')} <strong>{reference}</strong>
          </div>
        )}
        <div>
          <label htmlFor="incident-type" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">{t('incident_type_label')}</label>
          <select id="incident-type" required value={incidentType} onChange={(e) => setIncidentType(e.target.value)}
            className="w-full border rounded-lg px-3 py-2 dark:bg-gray-700 dark:border-gray-600 dark:text-white">
            <option value="" disabled>{t('incident_type_placeholder')}</option>
            {INCIDENT_TYPES.map((it) => (
              <option key={it} value={it}>{t(`itype_${it}`)}</option>
            ))}
          </select>
        </div>
        <div>
          <label htmlFor="incident-desc" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">{t('incident_desc_label')}</label>
          <textarea id="incident-desc" required minLength={10} maxLength={2000} rows={3} value={description}
            onChange={(e) => setDescription(e.target.value)} placeholder={t('incident_desc_placeholder')}
            className="w-full border rounded-lg px-3 py-2 dark:bg-gray-700 dark:border-gray-600 dark:text-white" />
        </div>
        <div className="grid md:grid-cols-2 gap-4">
          <div>
            <label htmlFor="incident-pu" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">{t('incident_pu_label')}</label>
            <input id="incident-pu" type="text" value={puCode} onChange={(e) => setPuCode(e.target.value)}
              placeholder="e.g., PU-FCT-001"
              className="w-full border rounded-lg px-3 py-2 dark:bg-gray-700 dark:border-gray-600 dark:text-white" />
          </div>
          <div>
            <label htmlFor="incident-contact" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">{t('incident_contact_label')}</label>
            <input id="incident-contact" type="text" value={contact} onChange={(e) => setContact(e.target.value)}
              className="w-full border rounded-lg px-3 py-2 dark:bg-gray-700 dark:border-gray-600 dark:text-white" />
          </div>
        </div>
        <button type="submit" disabled={submitting}
          className="bg-red-600 text-white px-6 py-2 rounded-lg font-medium hover:bg-red-700 disabled:opacity-50">
          {submitting ? t('incident_submitting') : t('incident_submit')}
        </button>
      </form>
    </div>
  );
}

export default function CitizenPortalPage() {
  const { t } = useI18n();
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
    } catch {
      void 0;
      setResults([]);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="p-6 max-w-5xl mx-auto" role="main" aria-label="Citizen Result Verification Portal">
      <div className="text-center mb-8">
        <h1 className="text-3xl font-bold dark:text-white">{t('citizen_title')}</h1>
        <p className="text-gray-500 dark:text-gray-400 mt-2">
          {t('citizen_subtitle')}
        </p>
      </div>

      {/* Search Form */}
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 mb-6">
        <div className="flex gap-4 mb-4">
          {(['pu_code', 'state', 'lga'] as const).map((tp) => (
            <button key={tp} onClick={() => setSearchType(tp)}
              className={`px-4 py-2 rounded-lg font-medium text-sm ${searchType === tp ? 'bg-green-600 text-white' : 'bg-gray-100 dark:bg-gray-700 dark:text-gray-300'}`}>
              {tp === 'pu_code' ? t('polling_unit') + ' ' + t('citizen_code_label').replace(':', '') : tp === 'state' ? t('state') : t('lga')}
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
            {loading ? t('citizen_searching') : t('citizen_verify_action')}
          </button>
        </div>
      </div>

      {/* Results */}
      {searched && (
        <div className="space-y-4" aria-live="polite">
          {results.length === 0 ? (
            <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-8 text-center">
              <p className="text-gray-500 dark:text-gray-400">{t('citizen_no_results')}</p>
            </div>
          ) : (
            results.map((r) => (
              <div key={r.id} className="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
                <div className="flex items-center justify-between mb-4">
                  <div>
                    <h3 className="font-bold dark:text-white">{r.pu_name || r.polling_unit_code}</h3>
                    <p className="text-sm text-gray-500 dark:text-gray-400">{t('citizen_code_label')} {r.polling_unit_code}</p>
                  </div>
                  <div className="flex items-center gap-2">
                    {r.cryptographically_verified && (
                      <span className="bg-green-100 text-green-800 text-xs px-2 py-1 rounded-full font-medium">{t('citizen_verified_badge')}</span>
                    )}
                    <span className={`text-xs px-2 py-1 rounded-full font-medium ${r.status === 'finalized' ? 'bg-green-100 text-green-800' : 'bg-yellow-100 text-yellow-800'}`}>
                      {r.status}
                    </span>
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-4 mb-4">
                  <div>
                    <p className="text-sm text-gray-500 dark:text-gray-400">{t('citizen_total_votes')}</p>
                    <p className="text-xl font-bold dark:text-white">{Number(r.total_votes).toLocaleString()}</p>
                  </div>
                  {r.accredited_voters && (
                    <div>
                      <p className="text-sm text-gray-500 dark:text-gray-400">{t('citizen_accredited')}</p>
                      <p className="text-xl font-bold dark:text-white">{Number(r.accredited_voters).toLocaleString()}</p>
                    </div>
                  )}
                </div>
                {r.party_scores && r.party_scores.length > 0 && (
                  <div>
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-300 mb-2">{t('citizen_party_scores')}</p>
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

      {/* Public complaint channel (R5-073) */}
      <div className="mt-8">
        <PublicComplaintForm />
      </div>

      <div className="mt-6 text-center text-xs text-gray-400 dark:text-gray-500">
        {t('citizen_disclaimer')}{' '}
        <a href="https://www.inecnigeria.org" className="underline" target="_blank" rel="noreferrer">www.inecnigeria.org</a>
      </div>
    </div>
  );
}
