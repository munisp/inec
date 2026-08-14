import React, { useEffect, useState } from 'react';
import { View, Text, ScrollView, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { api } from '../src/lib/api';

export default function AdminConsoleScreen() {
  const [stats, setStats] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      // R4-52: /admin/stats never existed; real route is GET /ems/dashboard.
      setStats(await api<Record<string, any>>('/ems/dashboard'));
    } catch (e) { console.error('Admin stats load:', e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  const cards = [
    { label: 'Registered Voters', value: stats?.voter_registration?.total_voters ?? 0, color: '#3b82f6' },
    { label: 'PVC Collected', value: stats?.voter_registration?.pvc_collected ?? 0, color: '#16a34a' },
    { label: 'BVAS Sync Queue', value: stats?.bvas_sync?.total ?? 0, color: '#8b5cf6' },
    { label: 'Sync Conflicts', value: stats?.bvas_sync?.conflicts ?? 0, color: '#ef4444' },
    { label: 'Active Portals', value: stats?.portal_hub?.active ?? 0, color: '#f59e0b' },
    { label: 'Validation Pass %', value: `${(stats?.validation?.pass_rate ?? 0).toFixed(1)}%`, color: '#06b6d4' },
    { label: 'Materials Delivered', value: stats?.materials?.delivered ?? 0, color: '#d946ef' },
    { label: 'Staff Deployed', value: stats?.staff_deployed ?? 0, color: '#10b981' },
  ];

  return (
    <ScrollView style={s.container} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}>
      <Text style={s.title}>Admin Console</Text>
      <Text style={s.subtitle}>System overview and administration</Text>
      <View style={s.grid}>
        {cards.map((c, i) => (
          <View key={i} style={s.card}>
            <Text style={s.cardLabel}>{c.label}</Text>
            <Text style={[s.cardValue, { color: c.color }]}>{c.value}</Text>
          </View>
        ))}
      </View>
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc', padding: 16 },
  center: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 4 },
  subtitle: { fontSize: 14, color: '#64748b', marginBottom: 16 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 10 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 14, width: '48%', shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  cardLabel: { fontSize: 12, color: '#64748b', marginBottom: 4 },
  cardValue: { fontSize: 24, fontWeight: '700' },
});
