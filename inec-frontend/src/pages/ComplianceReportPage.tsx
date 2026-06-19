import { useState, useEffect } from 'react';
import { api } from '../lib/api';
import { DEMO_COMPLIANCE } from '../lib/demo-data';

interface ComplianceData {
  standard: string;
  compliance_framework: string;
  assessment_criteria: string[];
  election_overview: {
    total_polling_units: number;
    units_reporting: number;
    coverage_pct: number;
    total_votes_cast: number;
  };
  security_assessment: {
    total_incidents: number;
    open_disputes: number;
    unresolved_anomalies: number;
    security_level: string;
  };
  observer_coverage: {
    total_observers: number;
    coverage_ratio: number;
  };
  recommendations: string[];
}

export default function ComplianceReportPage() {
  const [data, setData] = useState<ComplianceData | null>(null);
  const [standard, setStandard] = useState('ecowas');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    api.getComplianceReport(standard, 1)
      .then(setData)
      .catch(() => {
        const fw = DEMO_COMPLIANCE.frameworks.find(f => f.name.toLowerCase() === standard) || DEMO_COMPLIANCE.frameworks[0];
        setData({
          standard: fw.name,
          compliance_framework: `${fw.name} Electoral Observation Framework`,
          assessment_criteria: fw.requirements.map(r => r.name),
          election_overview: { total_polling_units: 176543, units_reporting: 170234, coverage_pct: 96.4, total_votes_cast: 24492921 },
          security_assessment: { total_incidents: 5, open_disputes: 2, unresolved_anomalies: 3, security_level: fw.score > 90 ? 'good' : 'fair' },
          observer_coverage: { total_observers: 2345, coverage_ratio: 0.013 },
          recommendations: ['Improve ward-level public access to collation data', 'Expand BVAS coverage to all PUs', 'Implement E2E verifiable voting for primaries'],
        });
      })
      .finally(() => setLoading(false));
  }, [standard]);

  const secLevelColor: Record<string, string> = {
    excellent: 'text-green-600 dark:text-green-400', good: 'text-blue-600 dark:text-blue-400',
    fair: 'text-yellow-600 dark:text-yellow-400', concerning: 'text-red-600 dark:text-red-400',
  };

  return (
    <div className="p-6 max-w-5xl mx-auto dark:text-white" role="main" aria-label="Compliance report page">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold dark:text-white">Compliance Report</h1>
        <div className="flex gap-2" role="tablist" aria-label="Select compliance standard">
          {['ecowas', 'au', 'eu'].map(s => (
            <button key={s} onClick={() => setStandard(s)} role="tab" aria-selected={standard === s}
              className={`px-4 py-2 rounded-lg text-sm font-medium transition ${standard === s ? 'bg-blue-600 text-white' : 'bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-300 dark:hover:bg-gray-600'}`}>
              {s.toUpperCase()}
            </button>
          ))}
        </div>
      </div>

      {loading && <div className="text-center py-20 text-gray-500" role="status">Loading report...</div>}

      {data && !loading && (
        <div className="space-y-6">
          <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-4">
            <p className="text-sm text-blue-600 dark:text-blue-400 font-medium">Framework</p>
            <p className="text-lg font-semibold dark:text-white">{data.compliance_framework}</p>
          </div>

          <section aria-label="Election overview">
            <h2 className="text-lg font-bold mb-3 dark:text-white">Election Overview</h2>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              {[
                { label: 'Total PUs', value: data.election_overview.total_polling_units.toLocaleString(), color: 'blue' },
                { label: 'Reporting', value: data.election_overview.units_reporting.toLocaleString(), color: 'green' },
                { label: 'Coverage', value: `${data.election_overview.coverage_pct.toFixed(1)}%`, color: 'yellow' },
                { label: 'Total Votes', value: data.election_overview.total_votes_cast.toLocaleString(), color: 'purple' },
              ].map(s => (
                <div key={s.label} className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
                  <p className="text-sm text-gray-500 dark:text-gray-400">{s.label}</p>
                  <p className="text-2xl font-bold dark:text-white">{s.value}</p>
                </div>
              ))}
            </div>
          </section>

          <section aria-label="Security assessment">
            <h2 className="text-lg font-bold mb-3 dark:text-white">Security Assessment</h2>
            <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
              <div className="flex items-center gap-4 mb-4">
                <span className="text-gray-500 dark:text-gray-400">Security Level:</span>
                <span className={`text-xl font-bold capitalize ${secLevelColor[data.security_assessment.security_level] || 'dark:text-white'}`}>
                  {data.security_assessment.security_level}
                </span>
              </div>
              <div className="grid grid-cols-3 gap-4">
                <div>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Incidents</p>
                  <p className="text-xl font-bold dark:text-white">{data.security_assessment.total_incidents}</p>
                </div>
                <div>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Open Disputes</p>
                  <p className="text-xl font-bold dark:text-white">{data.security_assessment.open_disputes}</p>
                </div>
                <div>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Unresolved Anomalies</p>
                  <p className="text-xl font-bold dark:text-white">{data.security_assessment.unresolved_anomalies}</p>
                </div>
              </div>
            </div>
          </section>

          <section aria-label="Assessment criteria">
            <h2 className="text-lg font-bold mb-3 dark:text-white">Assessment Criteria ({data.standard})</h2>
            <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
              <ul className="space-y-2">
                {data.assessment_criteria?.map((c, i) => (
                  <li key={i} className="flex items-center gap-2">
                    <span className="w-6 h-6 rounded-full bg-green-100 dark:bg-green-900 text-green-600 dark:text-green-400 flex items-center justify-center text-xs font-bold">{i + 1}</span>
                    <span className="dark:text-gray-300">{c}</span>
                  </li>
                ))}
              </ul>
            </div>
          </section>

          <section aria-label="Recommendations">
            <h2 className="text-lg font-bold mb-3 dark:text-white">Recommendations</h2>
            <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-lg p-4">
              <ul className="space-y-2">
                {data.recommendations?.map((r, i) => (
                  <li key={i} className="flex items-start gap-2">
                    <span className="text-yellow-600 dark:text-yellow-400 mt-1">•</span>
                    <span className="dark:text-gray-300">{r}</span>
                  </li>
                ))}
              </ul>
            </div>
          </section>
        </div>
      )}
    </div>
  );
}
