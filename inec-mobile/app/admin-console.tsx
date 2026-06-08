import React, { useEffect, useState } from 'react';
import { View, Text, ScrollView, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { API_URL as API } from '../src/lib/api';

export default function AdminConsoleScreen() {
  const [stats, setStats] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const res = await fetch(`${API}/admin/stats`);
      if (res.ok) setStats(await res.json());
    } catch (e) { console.error('Admin stats load:', e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  const cards = [
    { label: 'Total Users', value: stats?.total_users || 0, color: '#3b82f6' },
    { label: 'Active Elections', value: stats?.active_elections || 0, color: '#16a34a' },
    { label: 'Results Submitted', value: stats?.total_results || 0, color: '#8b5cf6' },
    { label: 'BVAS Devices', value: stats?.bvas_count || 0, color: '#f59e0b' },
    { label: 'Open Incidents', value: stats?.open_incidents || 0, color: '#ef4444' },
    { label: 'Active Observers', value: stats?.active_observers || 0, color: '#06b6d4' },
    { label: 'Pending Disputes', value: stats?.pending_disputes || 0, color: '#d946ef' },
    { label: 'System Uptime', value: stats?.uptime || '99.9%', color: '#10b981' },
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
