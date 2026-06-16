import React, { useEffect, useState } from 'react';
import { View, Text, ScrollView, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { API_URL as API } from '../src/lib/api';

export default function PredictiveAnalyticsScreen() {
  const [data, setData] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const res = await fetch(`${API}/analytics/predictions`);
      if (res.ok) setData(await res.json());
    } catch (e) { console.error('Predictions load:', e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  const predictions = data?.predictions || [];
  const turnoutPrediction = data?.turnout_prediction || {};
  const riskZones = data?.risk_zones || [];

  return (
    <ScrollView style={s.container} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}>
      <Text style={s.title}>Predictive Analytics</Text>

      <View style={s.card}>
        <Text style={s.cardTitle}>Turnout Prediction</Text>
        <Text style={s.bigNum}>{(turnoutPrediction.predicted_turnout_pct || 0).toFixed(1)}%</Text>
        <Text style={s.sub}>Predicted national turnout</Text>
        <Text style={s.sub}>Confidence: {(turnoutPrediction.confidence || 0).toFixed(1)}%</Text>
      </View>

      <Text style={s.sectionTitle}>Risk Zones ({riskZones.length})</Text>
      {riskZones.slice(0, 10).map((z: any, i: number) => (
        <View key={i} style={s.card}>
          <View style={s.row}>
            <Text style={s.zoneName}>{z.state_code || z.name}</Text>
            <View style={[s.riskBadge, { backgroundColor: z.risk_level === 'high' ? '#fee2e2' : z.risk_level === 'medium' ? '#fef3c7' : '#dcfce7' }]}>
              <Text style={{ color: z.risk_level === 'high' ? '#dc2626' : z.risk_level === 'medium' ? '#d97706' : '#16a34a', fontSize: 11, fontWeight: '600' }}>
                {z.risk_level}
              </Text>
            </View>
          </View>
          <Text style={s.sub}>Score: {(z.risk_score || 0).toFixed(2)}</Text>
          {z.factors && <Text style={s.sub}>Factors: {z.factors.join(', ')}</Text>}
        </View>
      ))}

      <Text style={s.sectionTitle}>Predictions ({predictions.length})</Text>
      {predictions.slice(0, 10).map((p: any, i: number) => (
        <View key={i} style={s.card}>
          <Text style={s.predLabel}>{p.label || p.metric}</Text>
          <Text style={s.predValue}>{p.predicted_value || p.value}</Text>
          <Text style={s.sub}>Confidence: {(p.confidence || 0).toFixed(1)}%</Text>
        </View>
      ))}
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc', padding: 16 },
  center: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 12 },
  sectionTitle: { fontSize: 17, fontWeight: '600', color: '#1e293b', marginTop: 16, marginBottom: 8 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 14, marginBottom: 10, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  cardTitle: { fontSize: 14, fontWeight: '600', color: '#64748b' },
  bigNum: { fontSize: 36, fontWeight: '700', color: '#16a34a', marginVertical: 4 },
  row: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  zoneName: { fontSize: 15, fontWeight: '600', color: '#1e293b' },
  riskBadge: { paddingHorizontal: 10, paddingVertical: 4, borderRadius: 10 },
  predLabel: { fontSize: 14, fontWeight: '600', color: '#1e293b' },
  predValue: { fontSize: 20, fontWeight: '700', color: '#3b82f6', marginVertical: 2 },
  sub: { fontSize: 12, color: '#64748b', marginTop: 2 },
});
