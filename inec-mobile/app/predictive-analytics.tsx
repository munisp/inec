import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';
import { useResolvedElection } from '../src/lib/election';

// R4-52: /ai/predict-turnout never existed; the real route is
// GET /predictive/analytics (per-state completion + turnout estimate).
interface StatePrediction {
  state_code: string;
  total_pus: number;
  reported_pus: number;
  completion_pct: number;
}

interface PredictiveAnalytics {
  completion_pct: number;
  predicted_turnout: number;
  total_pus: number;
  reported_pus: number;
  confidence: number;
  eta_complete: string;
  state_predictions: StatePrediction[];
}

// Real /ai/benford response shape (no p_value — chi-square threshold test).
interface BenfordResult {
  status: 'pass' | 'warning' | 'fail';
  conclusion: string;
  chi_squared: number;
  sample_size?: number;
  error?: string;
}

export default function PredictiveAnalyticsScreen() {
  const [predictions, setPredictions] = useState<StatePrediction[]>([]);
  const [overview, setOverview] = useState<PredictiveAnalytics | null>(null);
  const [benford, setBenford] = useState<BenfordResult | null>(null);
  const [loading, setLoading] = useState(false);
  const { electionId, loading: electionLoading } = useResolvedElection();

  const runPrediction = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      const res = await apiCall<PredictiveAnalytics>(`/predictive/analytics${electionId ? `?election_id=${electionId}` : ''}`);
      setPredictions(res.state_predictions || []);
      setOverview(res);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Prediction failed');
    }
    setLoading(false);
  };

  const runBenford = async () => {
    if (!electionId) { Alert.alert('Please wait', 'Election is still resolving.'); return; }
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      const res = await apiCall<BenfordResult>(`/ai/benford?election_id=${electionId}`);
      setBenford(res);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Benford analysis failed');
    }
    setLoading(false);
  };

  // Risk band derived from reporting completion (no server risk score exists).
  const riskOf = (completionPct: number) =>
    completionPct >= 75 ? 'low' : completionPct >= 40 ? 'medium' : 'high';
  const riskColor = (level: string) => {
    if (level === 'high') return '#dc2626';
    if (level === 'medium') return '#f59e0b';
    return '#166534';
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="trending-up-outline" size={24} color="#7c3aed" />
          <Text style={styles.cardTitle}>Turnout Predictions</Text>
        </View>
        <Text style={styles.muted}>AI-powered voter turnout prediction and risk assessment per state.</Text>
        <TouchableOpacity style={[styles.button, { backgroundColor: '#7c3aed' }]} onPress={runPrediction} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>{loading ? 'Analyzing...' : 'Run Prediction Model'}</Text>
        </TouchableOpacity>
        {overview && (
          <Text style={styles.muted}>
            Overall turnout estimate: {overview.predicted_turnout.toFixed(1)}% · Completion: {overview.completion_pct.toFixed(1)}% · Confidence: {(overview.confidence * 100).toFixed(0)}%
          </Text>
        )}
        {predictions.map((p) => {
          const risk = riskOf(p.completion_pct);
          return (
          <View key={p.state_code} style={styles.predRow}>
            <View style={{ flex: 1 }}>
              <Text style={{ fontSize: 14, fontWeight: '600', color: '#111827' }}>{p.state_code}</Text>
              <Text style={styles.muted}>Reported: {p.reported_pus}/{p.total_pus} PUs ({p.completion_pct.toFixed(1)}%)</Text>
            </View>
            <View style={[styles.riskBadge, { backgroundColor: riskColor(risk) + '20' }]}>
              <Text style={{ fontSize: 11, fontWeight: '700', color: riskColor(risk) }}>{risk.toUpperCase()}</Text>
            </View>
          </View>
          );
        })}
      </View>

      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="stats-chart-outline" size={24} color="#166534" />
          <Text style={styles.cardTitle}>Benford's Law Analysis</Text>
        </View>
        <Text style={styles.muted}>Statistical fraud detection by analyzing first-digit distribution of vote counts.</Text>
        <TouchableOpacity style={styles.button} onPress={runBenford} disabled={loading || electionLoading || !electionId} activeOpacity={0.8}>
          <Text style={styles.buttonText}>{loading ? 'Analyzing...' : 'Run Benford Analysis'}</Text>
        </TouchableOpacity>
        {benford && (
          <View style={[styles.resultBanner, { backgroundColor: benford.status === 'pass' ? '#dcfce7' : '#fef2f2' }]}>
            <Ionicons name={benford.status === 'pass' ? 'checkmark-circle' : 'warning'} size={28} color={benford.status === 'pass' ? '#166534' : '#dc2626'} />
            <View style={{ flex: 1, marginLeft: 12 }}>
              <Text style={{ fontSize: 16, fontWeight: '700', color: benford.status === 'pass' ? '#166534' : '#dc2626' }}>
                {benford.error ? benford.error : benford.status === 'pass' ? 'Distribution Normal' : benford.status === 'warning' ? 'Marginal Deviation' : 'Anomaly Detected'}
              </Text>
              {benford.chi_squared !== undefined && (
                <Text style={styles.muted}>Chi-squared: {benford.chi_squared.toFixed(2)}{benford.conclusion ? ` — ${benford.conclusion}` : ''}</Text>
              )}
            </View>
          </View>
        )}
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  card: { backgroundColor: '#fff', borderRadius: 16, padding: 16, marginBottom: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  cardHeader: { flexDirection: 'row', alignItems: 'center', gap: 10, marginBottom: 12 },
  cardTitle: { fontSize: 16, fontWeight: '700', color: '#111827' },
  muted: { fontSize: 13, color: '#9ca3af', marginBottom: 8 },
  button: { backgroundColor: '#166534', borderRadius: 12, padding: 14, alignItems: 'center', marginTop: 8 },
  buttonText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  predRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
  riskBadge: { paddingHorizontal: 8, paddingVertical: 4, borderRadius: 6 },
  resultBanner: { flexDirection: 'row', alignItems: 'center', borderRadius: 12, padding: 14, marginTop: 12 },
});
