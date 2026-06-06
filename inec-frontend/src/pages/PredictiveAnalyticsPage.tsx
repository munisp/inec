import { useState, useEffect } from 'react';
import { api } from '../lib/api';


interface Prediction {
  election_id: number;
  election_type: string;
  turnout_prediction: number;
  margin_of_victory: number;
  confidence: number;
  risk_factors: string[];
  model_version: string;
}

export default function PredictiveAnalyticsPage() {
  const [data, setData] = useState<{ predictions: Prediction[]; model: string } | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.getPredictiveAnalytics()
      .then(setData)
      .catch(() => void 0)
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="p-6 max-w-5xl mx-auto">
        <h1 className="text-2xl font-bold dark:text-white mb-4">Predictive Analytics</h1>
        <div className="animate-pulse bg-gray-200 dark:bg-gray-700 h-64 rounded-lg" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-5xl mx-auto" role="main" aria-label="Predictive Analytics">
      <h1 className="text-2xl font-bold dark:text-white mb-2">Predictive Analytics</h1>
      <p className="text-gray-500 dark:text-gray-400 mb-6">
        ML-powered turnout prediction, margin analysis, and risk assessment
      </p>

      {data?.predictions && data.predictions.length > 0 ? (
        <div className="space-y-4">
          {data.predictions.map((p, i) => (
            <div key={i} className="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
              <div className="flex items-center justify-between mb-4">
                <h3 className="font-bold dark:text-white">Election #{p.election_id} — {p.election_type}</h3>
                <span className="text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 px-2 py-1 rounded-full">
                  {p.model_version}
                </span>
              </div>
              <div className="grid grid-cols-3 gap-4 mb-4">
                <div>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Turnout Prediction</p>
                  <p className="text-2xl font-bold text-blue-600">{(p.turnout_prediction * 100).toFixed(1)}%</p>
                </div>
                <div>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Victory Margin</p>
                  <p className="text-2xl font-bold text-green-600">{(p.margin_of_victory * 100).toFixed(1)}%</p>
                </div>
                <div>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Confidence</p>
                  <p className="text-2xl font-bold dark:text-white">{(p.confidence * 100).toFixed(0)}%</p>
                  <div className="w-full bg-gray-200 dark:bg-gray-600 rounded-full h-2 mt-1">
                    <div className="bg-green-500 h-2 rounded-full" style={{ width: `${p.confidence * 100}%` }} />
                  </div>
                </div>
              </div>
              {p.risk_factors && p.risk_factors.length > 0 && (
                <div>
                  <p className="text-sm font-medium text-gray-600 dark:text-gray-300 mb-1">Risk Factors</p>
                  <div className="flex flex-wrap gap-1">
                    {p.risk_factors.map((rf, j) => (
                      <span key={j} className="bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300 text-xs px-2 py-1 rounded-full">{rf}</span>
                    ))}
                  </div>
                </div>
              )}
            </div>
          ))}
        </div>
      ) : (
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-8 text-center">
          <p className="text-gray-500 dark:text-gray-400">No predictions available. Predictions are generated when election data is sufficient.</p>
        </div>
      )}

      <div className="mt-6 text-xs text-gray-400 dark:text-gray-500 text-center">
        Predictions generated using XGBoost ensemble model. Not official projections.
      </div>
    </div>
  );
}
