import React, { useEffect, useState } from 'react';
import { View, Text, ScrollView, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { API_URL as API } from '../src/lib/api';

export default function ProductionScreen() {
  const [health, setHealth] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const [h, m, s] = await Promise.all([
        fetch(`${API}/scale/health`).then(r => r.ok ? r.json() : null),
        fetch(`${API}/middleware/modes`).then(r => r.ok ? r.json() : null),
        fetch(`${API}/scale/pool-stats`).then(r => r.ok ? r.json() : null),
      ]);
      setHealth({ health: h, middleware: m, pool: s });
    } catch (e) { console.error(e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  const modes = health?.middleware?.modes || [];
  const realCount = modes.filter((m: any) => m.IsReal).length;

  return (
    <ScrollView style={s.container} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}>
      <Text style={s.title}>Production Status</Text>

      <View style={s.card}>
        <Text style={s.cardTitle}>System Health</Text>
        <View style={[s.statusBig, { backgroundColor: health?.health?.status === 'healthy' ? '#dcfce7' : '#fee2e2' }]}>
          <Text style={{ color: health?.health?.status === 'healthy' ? '#16a34a' : '#dc2626', fontSize: 18, fontWeight: '700' }}>
            {health?.health?.status || 'Unknown'}
          </Text>
        </View>
      </View>

      <View style={s.card}>
        <Text style={s.cardTitle}>Middleware ({realCount}/{modes.length} real)</Text>
        {modes.map((m: any, i: number) => (
          <View key={i} style={s.mwRow}>
            <View style={[s.dot, { backgroundColor: m.IsReal ? '#16a34a' : '#f59e0b' }]} />
            <Text style={s.mwName}>{m.Name}</Text>
            <Text style={s.mwMode}>{m.Connection}</Text>
          </View>
        ))}
      </View>

      <View style={s.card}>
        <Text style={s.cardTitle}>Connection Pool</Text>
        <Text style={s.sub}>Open: {health?.pool?.open_connections || 0}</Text>
        <Text style={s.sub}>In Use: {health?.pool?.in_use || 0}</Text>
        <Text style={s.sub}>Idle: {health?.pool?.idle || 0}</Text>
      </View>
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc', padding: 16 },
  center: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 12 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 14, marginBottom: 10, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  cardTitle: { fontSize: 16, fontWeight: '600', color: '#1e293b', marginBottom: 8 },
  statusBig: { alignSelf: 'flex-start', paddingHorizontal: 16, paddingVertical: 8, borderRadius: 10 },
  mwRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 6 },
  dot: { width: 8, height: 8, borderRadius: 4, marginRight: 8 },
  mwName: { fontSize: 13, fontWeight: '600', color: '#1e293b', flex: 1 },
  mwMode: { fontSize: 12, color: '#64748b' },
  sub: { fontSize: 13, color: '#64748b', marginTop: 2 },
});
