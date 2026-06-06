import { useState, useEffect } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Platform, ActivityIndicator } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';

interface AnomalyItem {
  id: number;
  polling_unit_code: string;
  anomaly_type: string;
  severity: string;
  score: number;
  description: string;
  status: string;
  created_at: string;
}

export default function AnomalyDetectionScreen() {
  const [anomalies, setAnomalies] = useState<AnomalyItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadAnomalies = async () => {
    setLoading(true);
    setError(null);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const data = await api<{ anomalies: AnomalyItem[] }>('/anomalies?limit=50');
      setAnomalies(data.anomalies || []);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { loadAnomalies(); }, []);

  const severityColor = (s: string) => {
    switch (s) {
      case 'critical': return '#dc2626';
      case 'high': return '#f59e0b';
      case 'medium': return '#3b82f6';
      default: return '#64748b';
    }
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}>
      <View style={styles.header}>
        <Ionicons name="warning" size={28} color="#f59e0b" />
        <Text style={styles.title}>Anomaly Detection</Text>
      </View>

      {loading && <ActivityIndicator size="large" color="#f59e0b" style={{ marginTop: 40 }} />}
      {error && (
        <View style={styles.errorCard}>
          <Text style={styles.errorText}>{error}</Text>
          <TouchableOpacity style={styles.retryBtn} onPress={loadAnomalies}>
            <Text style={styles.retryText}>Retry</Text>
          </TouchableOpacity>
        </View>
      )}

      <View style={styles.summaryRow}>
        <View style={[styles.summaryCard, { borderLeftColor: '#dc2626' }]}>
          <Text style={styles.summaryValue}>{anomalies.filter(a => a.severity === 'critical').length}</Text>
          <Text style={styles.summaryLabel}>Critical</Text>
        </View>
        <View style={[styles.summaryCard, { borderLeftColor: '#f59e0b' }]}>
          <Text style={styles.summaryValue}>{anomalies.filter(a => a.severity === 'high').length}</Text>
          <Text style={styles.summaryLabel}>High</Text>
        </View>
        <View style={[styles.summaryCard, { borderLeftColor: '#3b82f6' }]}>
          <Text style={styles.summaryValue}>{anomalies.filter(a => a.severity === 'medium').length}</Text>
          <Text style={styles.summaryLabel}>Medium</Text>
        </View>
      </View>

      {anomalies.map((a) => (
        <View key={a.id} style={[styles.anomalyCard, { borderLeftColor: severityColor(a.severity) }]}>
          <View style={styles.anomalyHeader}>
            <Ionicons name="alert-circle" size={18} color={severityColor(a.severity)} />
            <Text style={styles.anomalyType}>{a.anomaly_type}</Text>
            <View style={[styles.severityBadge, { backgroundColor: severityColor(a.severity) + '20' }]}>
              <Text style={[styles.severityText, { color: severityColor(a.severity) }]}>{a.severity}</Text>
            </View>
          </View>
          <Text style={styles.anomalyPU}>{a.polling_unit_code}</Text>
          <Text style={styles.anomalyDesc}>{a.description}</Text>
          <View style={styles.anomalyFooter}>
            <Text style={styles.anomalyScore}>Score: {(a.score * 100).toFixed(0)}%</Text>
            <Text style={styles.anomalyStatus}>{a.status}</Text>
          </View>
        </View>
      ))}

      {!loading && anomalies.length === 0 && !error && (
        <View style={styles.emptyCard}>
          <Ionicons name="checkmark-circle" size={48} color="#22c55e" />
          <Text style={styles.emptyTitle}>No Anomalies Detected</Text>
          <Text style={styles.emptySubtitle}>All results are within expected parameters</Text>
        </View>
      )}

      <TouchableOpacity style={styles.refreshBtn} onPress={loadAnomalies}>
        <Ionicons name="refresh" size={18} color="#fff" />
        <Text style={styles.refreshText}>Refresh</Text>
      </TouchableOpacity>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 16, paddingTop: Platform.OS === 'ios' ? 60 : 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b' },
  errorCard: { margin: 16, padding: 16, backgroundColor: '#fef2f2', borderRadius: 12, alignItems: 'center' },
  errorText: { color: '#dc2626', marginBottom: 8 },
  retryBtn: { paddingHorizontal: 16, paddingVertical: 8, backgroundColor: '#dc2626', borderRadius: 8 },
  retryText: { color: '#fff', fontWeight: '600' },
  summaryRow: { flexDirection: 'row', paddingHorizontal: 16, gap: 8, marginBottom: 8 },
  summaryCard: { flex: 1, padding: 14, backgroundColor: '#fff', borderRadius: 12, borderLeftWidth: 4, alignItems: 'center' },
  summaryValue: { fontSize: 24, fontWeight: '700', color: '#1e293b' },
  summaryLabel: { fontSize: 11, color: '#64748b', marginTop: 2 },
  anomalyCard: { marginHorizontal: 16, marginBottom: 10, padding: 14, backgroundColor: '#fff', borderRadius: 12, borderLeftWidth: 4 },
  anomalyHeader: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  anomalyType: { fontSize: 14, fontWeight: '600', color: '#1e293b', flex: 1 },
  severityBadge: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: 6 },
  severityText: { fontSize: 11, fontWeight: '600', textTransform: 'uppercase' },
  anomalyPU: { fontSize: 12, color: '#3b82f6', marginTop: 4, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  anomalyDesc: { fontSize: 13, color: '#64748b', marginTop: 4 },
  anomalyFooter: { flexDirection: 'row', justifyContent: 'space-between', marginTop: 8, paddingTop: 8, borderTopWidth: 1, borderTopColor: '#f1f5f9' },
  anomalyScore: { fontSize: 12, color: '#64748b' },
  anomalyStatus: { fontSize: 12, fontWeight: '600', color: '#166534' },
  emptyCard: { margin: 32, padding: 32, alignItems: 'center' },
  emptyTitle: { fontSize: 18, fontWeight: '600', color: '#1e293b', marginTop: 12 },
  emptySubtitle: { fontSize: 14, color: '#64748b', marginTop: 4 },
  refreshBtn: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 6, margin: 16, padding: 14, backgroundColor: '#f59e0b', borderRadius: 12 },
  refreshText: { color: '#fff', fontWeight: '600', fontSize: 15 },
});
