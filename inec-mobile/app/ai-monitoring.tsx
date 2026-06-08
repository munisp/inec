import React, { useEffect, useState } from 'react';
import { View, Text, ScrollView, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { API_URL as API } from '../src/lib/api';

export default function AIMonitoringScreen() {
  const [data, setData] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const res = await fetch(`${API}/ai/models/info`);
      if (res.ok) setData(await res.json());
    } catch (e) { console.error('AI monitoring load:', e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  const models = data?.models || [];

  return (
    <ScrollView style={s.container} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}>
      <Text style={s.title}>AI/ML Monitoring</Text>
      <Text style={s.subtitle}>Model performance, drift detection, inference stats</Text>

      {models.map((m: any, i: number) => (
        <View key={i} style={s.card}>
          <Text style={s.modelName}>{m.name || m.model_name}</Text>
          <View style={s.row}>
            <View style={s.metric}><Text style={s.metricLabel}>Type</Text><Text style={s.metricVal}>{m.type || m.model_type}</Text></View>
            <View style={s.metric}><Text style={s.metricLabel}>Status</Text><Text style={[s.metricVal, { color: m.status === 'active' ? '#16a34a' : '#f59e0b' }]}>{m.status || 'active'}</Text></View>
            <View style={s.metric}><Text style={s.metricLabel}>Version</Text><Text style={s.metricVal}>{m.version || '1.0'}</Text></View>
          </View>
          {m.accuracy && <Text style={s.sub}>Accuracy: {(m.accuracy * 100).toFixed(1)}%</Text>}
          {m.f1_score && <Text style={s.sub}>F1 Score: {(m.f1_score * 100).toFixed(1)}%</Text>}
          {m.last_trained && <Text style={s.sub}>Last trained: {m.last_trained}</Text>}
        </View>
      ))}

      <View style={s.card}>
        <Text style={s.cardTitle}>Inference Stats</Text>
        <Text style={s.sub}>Total predictions: {data?.total_predictions || 0}</Text>
        <Text style={s.sub}>Avg latency: {data?.avg_latency_ms || 0}ms</Text>
        <Text style={s.sub}>Anomalies detected: {data?.anomalies_detected || 0}</Text>
      </View>
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc', padding: 16 },
  center: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 4 },
  subtitle: { fontSize: 14, color: '#64748b', marginBottom: 16 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 14, marginBottom: 10, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  cardTitle: { fontSize: 16, fontWeight: '600', color: '#1e293b', marginBottom: 8 },
  modelName: { fontSize: 16, fontWeight: '700', color: '#1e293b', marginBottom: 8 },
  row: { flexDirection: 'row', gap: 16, marginBottom: 8 },
  metric: { flex: 1 },
  metricLabel: { fontSize: 11, color: '#94a3b8' },
  metricVal: { fontSize: 14, fontWeight: '600', color: '#1e293b' },
  sub: { fontSize: 13, color: '#64748b', marginTop: 2 },
});
