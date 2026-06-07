import { useState, useEffect, useCallback } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Platform, ActivityIndicator, RefreshControl } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';

interface TierStats { bronze: number; silver: number; gold: number }

export default function MLDashboardScreen() {
  const [tiers, setTiers] = useState<TierStats>({ bronze: 0, silver: 0, gold: 0 });
  const [trainingStatus, setTrainingStatus] = useState<Record<string, unknown> | null>(null);
  const [models, setModels] = useState<Record<string, unknown>>({});
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const loadData = useCallback(async () => {
    setLoading(true);
    if (Platform.OS !== 'web') Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const [lh, ts, mr] = await Promise.all([
        api<{ tiers: TierStats }>('/ai/lakehouse/status').catch(() => ({ tiers: { bronze: 0, silver: 0, gold: 0 } })),
        api<Record<string, unknown>>('/ai/training/status').catch(() => null),
        api<{ models: Record<string, unknown>; production: Record<string, string> }>('/ai/registry/models').catch(() => ({ models: {}, production: {} })),
      ]);
      setTiers(lh.tiers);
      setTrainingStatus(ts);
      setModels((mr as { models: Record<string, unknown> }).models || {});
    } catch { /* handled */ }
    setLoading(false);
    setRefreshing(false);
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  const onRefresh = () => { setRefreshing(true); loadData(); };

  const totalRows = tiers.bronze + tiers.silver + tiers.gold;

  return (
    <ScrollView style={styles.container} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />}>
      <View style={styles.header}>
        <Text style={styles.title}>ML/AI Infrastructure</Text>
        <TouchableOpacity onPress={loadData}>
          <Ionicons name="refresh" size={24} color="#166534" />
        </TouchableOpacity>
      </View>

      {loading && <ActivityIndicator size="large" color="#166534" style={{ marginVertical: 20 }} />}

      {/* Summary Cards */}
      <View style={styles.cardRow}>
        <View style={[styles.card, { flex: 1 }]}>
          <Ionicons name="server-outline" size={24} color="#ea580c" />
          <Text style={styles.cardValue}>{totalRows.toLocaleString()}</Text>
          <Text style={styles.cardLabel}>Lakehouse Rows</Text>
        </View>
        <View style={[styles.card, { flex: 1 }]}>
          <Ionicons name="cube-outline" size={24} color="#7c3aed" />
          <Text style={styles.cardValue}>{Object.keys(models).length}</Text>
          <Text style={styles.cardLabel}>ML Models</Text>
        </View>
      </View>

      {/* Lakehouse Tiers */}
      <Text style={styles.sectionTitle}>Lakehouse Tiers</Text>
      {(['bronze', 'silver', 'gold'] as const).map(tier => (
        <View key={tier} style={styles.tierRow}>
          <View style={[styles.tierBadge, tier === 'bronze' ? styles.bronze : tier === 'silver' ? styles.silver : styles.gold]}>
            <Text style={styles.tierBadgeText}>{tier.toUpperCase()}</Text>
          </View>
          <View style={styles.tierBar}>
            <View style={[styles.tierFill, { width: `${totalRows > 0 ? (tiers[tier] / totalRows) * 100 : 0}%` }]} />
          </View>
          <Text style={styles.tierCount}>{tiers[tier].toLocaleString()}</Text>
        </View>
      ))}

      {/* Training Status */}
      <Text style={styles.sectionTitle}>Continuous Training</Text>
      <View style={styles.card}>
        <View style={styles.statusRow}>
          <Ionicons
            name={trainingStatus && (trainingStatus as Record<string, Record<string, boolean>>).drift_status?.drift_detected ? 'warning' : 'checkmark-circle'}
            size={20}
            color={trainingStatus && (trainingStatus as Record<string, Record<string, boolean>>).drift_status?.drift_detected ? '#ef4444' : '#22c55e'}
          />
          <Text style={styles.statusText}>
            {trainingStatus && (trainingStatus as Record<string, Record<string, boolean>>).drift_status?.drift_detected ? 'Drift Detected' : 'Model Stable'}
          </Text>
        </View>
        <Text style={styles.smallText}>
          Predictions: {String((trainingStatus as Record<string, unknown>)?.prediction_count || 0)}
        </Text>
      </View>

      {/* Models */}
      <Text style={styles.sectionTitle}>Trained Models</Text>
      {Object.keys(models).length === 0 && (
        <Text style={styles.emptyText}>No models registered yet</Text>
      )}
      {Object.entries(models).map(([id, model]) => {
        const m = model as Record<string, unknown>;
        const metrics = m.metrics as Record<string, number> | undefined;
        return (
          <View key={id} style={styles.card}>
            <Text style={styles.modelName}>{String(m.name || id)}</Text>
            {metrics && (
              <View style={styles.metricsRow}>
                {Object.entries(metrics).map(([k, v]) => (
                  <View key={k} style={styles.metricBadge}>
                    <Text style={styles.metricText}>{k}: {typeof v === 'number' ? v.toFixed(4) : String(v)}</Text>
                  </View>
                ))}
              </View>
            )}
          </View>
        );
      })}

      {/* Architecture */}
      <Text style={styles.sectionTitle}>Architecture</Text>
      <View style={styles.card}>
        <Text style={styles.archText}>Go Backend → Python ML Server</Text>
        <Text style={styles.archText}>XGBoost | GAT GNN | CDCN | PaddleOCR</Text>
        <Text style={styles.archText}>Lakehouse: Bronze→Silver→Gold</Text>
        <Text style={styles.archText}>Ray: Distributed Training & Inference</Text>
      </View>

      <View style={{ height: 40 }} />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#166534' },
  cardRow: { flexDirection: 'row', gap: 12, marginBottom: 16 },
  card: { backgroundColor: '#fff', borderRadius: 12, padding: 16, marginBottom: 8, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 3, elevation: 2 },
  cardValue: { fontSize: 28, fontWeight: '700', color: '#111', marginTop: 4 },
  cardLabel: { fontSize: 12, color: '#6b7280', marginTop: 2 },
  sectionTitle: { fontSize: 16, fontWeight: '600', color: '#374151', marginTop: 16, marginBottom: 8 },
  tierRow: { flexDirection: 'row', alignItems: 'center', marginBottom: 8, gap: 8 },
  tierBadge: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: 4, width: 70, alignItems: 'center' },
  tierBadgeText: { fontSize: 11, fontWeight: '600' },
  bronze: { backgroundColor: '#fed7aa' },
  silver: { backgroundColor: '#e5e7eb' },
  gold: { backgroundColor: '#fef08a' },
  tierBar: { flex: 1, height: 8, backgroundColor: '#e5e7eb', borderRadius: 4, overflow: 'hidden' },
  tierFill: { height: '100%', backgroundColor: '#166534', borderRadius: 4 },
  tierCount: { fontSize: 13, fontWeight: '600', width: 60, textAlign: 'right' },
  statusRow: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  statusText: { fontSize: 15, fontWeight: '600' },
  smallText: { fontSize: 12, color: '#6b7280', marginTop: 4 },
  emptyText: { fontSize: 13, color: '#9ca3af', fontStyle: 'italic', marginBottom: 8 },
  modelName: { fontSize: 15, fontWeight: '600', marginBottom: 4 },
  metricsRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 4 },
  metricBadge: { backgroundColor: '#f3f4f6', paddingHorizontal: 6, paddingVertical: 2, borderRadius: 4 },
  metricText: { fontSize: 11, color: '#374151' },
  archText: { fontSize: 13, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace', color: '#4b5563', marginBottom: 2 },
});
