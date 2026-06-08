import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { API_URL as API } from '../src/lib/api';

interface Observer { id: number; name: string; organization: string; accreditation_id: string; status: string; assigned_pu: string; check_in_time: string; }

export default function ObserverMonitoringScreen() {
  const [observers, setObservers] = useState<Observer[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const res = await fetch(`${API}/observers`);
      if (res.ok) { const d = await res.json(); setObservers(Array.isArray(d) ? d : d.observers || []); }
    } catch (e) { console.error('Observer load:', e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  return (
    <View style={s.container}>
      <Text style={s.title}>Observer Monitoring</Text>
      <Text style={s.count}>{observers.length} observers deployed</Text>
      <FlatList data={observers} keyExtractor={o => String(o.id)}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}
        renderItem={({ item }) => (
          <View style={s.card}>
            <Text style={s.name}>{item.name}</Text>
            <Text style={s.sub}>{item.organization} · {item.accreditation_id}</Text>
            <Text style={s.sub}>PU: {item.assigned_pu}</Text>
            <View style={[s.statusBadge, { backgroundColor: item.status === 'active' ? '#dcfce7' : '#fee2e2' }]}>
              <Text style={{ color: item.status === 'active' ? '#16a34a' : '#dc2626', fontSize: 12, fontWeight: '600' }}>{item.status}</Text>
            </View>
          </View>
        )}
      />
    </View>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc', padding: 16 },
  center: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 4 },
  count: { fontSize: 13, color: '#64748b', marginBottom: 12 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 14, marginBottom: 10, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  name: { fontSize: 15, fontWeight: '600', color: '#1e293b' },
  sub: { fontSize: 13, color: '#64748b', marginTop: 4 },
  statusBadge: { alignSelf: 'flex-start', paddingHorizontal: 10, paddingVertical: 4, borderRadius: 12, marginTop: 8 },
});
