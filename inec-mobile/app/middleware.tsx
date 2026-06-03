import { useState, useEffect, useCallback } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, RefreshControl } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { middlewareApi } from '../src/lib/api';

interface MiddlewareItem {
  name: string;
  connected: boolean;
  mode: string;
}

export default function MiddlewareScreen() {
  const [middleware, setMiddleware] = useState<MiddlewareItem[]>([]);
  const [health, setHealth] = useState<Record<string, unknown> | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const loadData = useCallback(async () => {
    try {
      const [statusRes, healthRes] = await Promise.all([
        middlewareApi.status().catch(() => ({ middleware: [] })),
        middlewareApi.health().catch(() => null),
      ]);
      setMiddleware(statusRes?.middleware || []);
      setHealth(healthRes);
    } catch {}
    setLoading(false);
    setRefreshing(false);
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  const onRefresh = useCallback(() => {
    setRefreshing(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    loadData();
  }, [loadData]);

  const connectedCount = middleware.filter(m => m.connected).length;
  const totalCount = middleware.length || 13;

  const modeIcon = (mode: string): keyof typeof Ionicons.glyphMap => {
    if (mode === 'external') return 'cloud-outline';
    if (mode === 'embedded') return 'cube-outline';
    return 'cog-outline';
  };

  if (loading) {
    return <View style={styles.center}><Text style={styles.muted}>Loading middleware status...</Text></View>;
  }

  return (
    <ScrollView style={styles.container} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#166534" />} contentContainerStyle={{ paddingBottom: 40 }}>
      {/* Summary */}
      <View style={styles.summaryRow}>
        <View style={styles.summaryCard}>
          <Text style={styles.summaryNumber}>{totalCount}</Text>
          <Text style={styles.summaryLabel}>Total</Text>
        </View>
        <View style={styles.summaryCard}>
          <Text style={[styles.summaryNumber, { color: '#166534' }]}>{connectedCount}</Text>
          <Text style={styles.summaryLabel}>Connected</Text>
        </View>
        <View style={styles.summaryCard}>
          <Text style={[styles.summaryNumber, { color: '#2563eb' }]}>{totalCount - connectedCount}</Text>
          <Text style={styles.summaryLabel}>Embedded</Text>
        </View>
      </View>

      {health && (
        <View style={[styles.healthBanner, { backgroundColor: health.all_connected ? '#dcfce7' : '#fef3c7' }]}>
          <Ionicons name={health.all_connected ? 'checkmark-circle' : 'warning'} size={20} color={health.all_connected ? '#166534' : '#92400e'} />
          <Text style={{ fontSize: 14, fontWeight: '600', color: health.all_connected ? '#166534' : '#92400e', marginLeft: 8 }}>
            {health.all_connected ? 'All systems healthy' : 'Some services using fallback mode'}
          </Text>
        </View>
      )}

      {/* Middleware List */}
      {middleware.map((mw) => (
        <View key={mw.name} style={styles.mwCard}>
          <View style={[styles.statusDot, { backgroundColor: mw.connected ? '#22c55e' : '#3b82f6' }]} />
          <Ionicons name={modeIcon(mw.mode)} size={20} color="#6b7280" style={{ marginRight: 10 }} />
          <View style={{ flex: 1 }}>
            <Text style={styles.mwName}>{mw.name}</Text>
            <Text style={styles.muted}>{mw.mode}</Text>
          </View>
          <View style={[styles.statusBadge, { backgroundColor: mw.connected ? '#dcfce7' : '#dbeafe' }]}>
            <Text style={{ fontSize: 11, fontWeight: '600', color: mw.connected ? '#166534' : '#2563eb' }}>
              {mw.connected ? 'CONNECTED' : 'EMBEDDED'}
            </Text>
          </View>
        </View>
      ))}

      {middleware.length === 0 && (
        <View style={styles.center}>
          <Ionicons name="layers-outline" size={48} color="#d1d5db" />
          <Text style={[styles.muted, { marginTop: 12 }]}>No middleware data available</Text>
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  center: { flex: 1, justifyContent: 'center', alignItems: 'center', paddingVertical: 60 },
  summaryRow: { flexDirection: 'row', gap: 12, marginBottom: 16 },
  summaryCard: { flex: 1, backgroundColor: '#fff', borderRadius: 12, padding: 14, alignItems: 'center', shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  summaryNumber: { fontSize: 24, fontWeight: '700', color: '#111827' },
  summaryLabel: { fontSize: 12, color: '#6b7280', marginTop: 2 },
  healthBanner: { flexDirection: 'row', alignItems: 'center', padding: 12, borderRadius: 12, marginBottom: 16 },
  mwCard: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#fff', borderRadius: 12, padding: 14, marginBottom: 8, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.03, shadowRadius: 2, elevation: 1 },
  statusDot: { width: 8, height: 8, borderRadius: 4, marginRight: 10 },
  mwName: { fontSize: 15, fontWeight: '600', color: '#111827' },
  muted: { fontSize: 12, color: '#9ca3af' },
  statusBadge: { paddingHorizontal: 8, paddingVertical: 4, borderRadius: 6 },
});
