import React, { useEffect, useState } from 'react';
import { View, Text, ScrollView, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { API_URL as API } from '../src/lib/api';

interface Integration { name: string; status: string; type: string; last_sync: string; records_synced: number; }

export default function PortalIntegrationScreen() {
  const [integrations, setIntegrations] = useState<Integration[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try { const r = await fetch(`${API}/integrations`); if (r.ok) { const d = await r.json(); setIntegrations(Array.isArray(d) ? d : d.integrations || []); } } catch (e) { console.error(e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  const statusColors: Record<string, string> = { connected: '#16a34a', disconnected: '#dc2626', syncing: '#f59e0b', error: '#ef4444' };

  return (
    <ScrollView style={s.container} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}>
      <Text style={s.title}>Portal Integrations</Text>
      <Text style={s.subtitle}>External system connections and data sync status</Text>
      {integrations.map((item, i) => (
        <View key={i} style={s.card}>
          <View style={s.row}>
            <Text style={s.name}>{item.name}</Text>
            <View style={[s.dot, { backgroundColor: statusColors[item.status] || '#6b7280' }]} />
          </View>
          <Text style={s.sub}>Type: {item.type} · Status: {item.status}</Text>
          <Text style={s.sub}>Records synced: {(item.records_synced || 0).toLocaleString()}</Text>
          {item.last_sync && <Text style={s.sub}>Last sync: {item.last_sync}</Text>}
        </View>
      ))}
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc', padding: 16 },
  center: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 4 },
  subtitle: { fontSize: 14, color: '#64748b', marginBottom: 16 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 14, marginBottom: 10, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  row: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  name: { fontSize: 15, fontWeight: '600', color: '#1e293b' },
  dot: { width: 10, height: 10, borderRadius: 5 },
  sub: { fontSize: 13, color: '#64748b', marginTop: 4 },
});
