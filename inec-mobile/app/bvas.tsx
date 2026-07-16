import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, ActivityIndicator, RefreshControl, TouchableOpacity } from 'react-native';
import { API_URL as API } from '../src/lib/api';

interface BVASDevice {
  id: number;
  device_id: string;
  polling_unit_code: string;
  status: string;
  battery_level: number;
  firmware_version: string;
  last_sync: string;
}

export default function BVASScreen() {
  const [devices, setDevices] = useState<BVASDevice[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [filter, setFilter] = useState<string>('all');

  const load = async () => {
    try {
      const res = await fetch(`${API}/bvas/devices`);
      if (res.ok) {
        const data = await res.json();
        setDevices(Array.isArray(data) ? data : data.devices || []);
      }
    } catch (e) { console.error('BVAS load error:', e); }
    setLoading(false);
    setRefreshing(false);
  };

  useEffect(() => { load(); }, []);

  const filtered = filter === 'all' ? devices : devices.filter(d => d.status === filter);
  const statusColors: Record<string, string> = { active: '#16a34a', offline: '#dc2626', syncing: '#f59e0b', maintenance: '#6b7280' };

  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  return (
    <View style={s.container}>
      <Text style={s.title}>BVAS Devices</Text>
      <View style={s.filters}>
        {['all', 'active', 'offline', 'syncing'].map(f => (
          <TouchableOpacity key={f} style={[s.chip, filter === f && s.chipActive]} onPress={() => setFilter(f)}>
            <Text style={[s.chipText, filter === f && s.chipTextActive]}>{f}</Text>
          </TouchableOpacity>
        ))}
      </View>
      <Text style={s.count}>{filtered.length} devices</Text>
      <FlatList
        data={filtered}
        keyExtractor={d => String(d.id)}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}
        renderItem={({ item }) => (
          <View style={s.card}>
            <View style={s.row}>
              <Text style={s.deviceId}>{item.device_id}</Text>
              <View style={[s.badge, { backgroundColor: statusColors[item.status] || '#6b7280' }]}>
                <Text style={s.badgeText}>{item.status}</Text>
              </View>
            </View>
            <Text style={s.sub}>PU: {item.polling_unit_code}</Text>
            <View style={s.row}>
              <Text style={s.sub}>Battery: {item.battery_level}%</Text>
              <Text style={s.sub}>FW: {item.firmware_version}</Text>
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
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 12 },
  count: { fontSize: 13, color: '#64748b', marginBottom: 8 },
  filters: { flexDirection: 'row', gap: 8, marginBottom: 12 },
  chip: { paddingHorizontal: 12, paddingVertical: 6, borderRadius: 16, backgroundColor: '#e2e8f0' },
  chipActive: { backgroundColor: '#16a34a' },
  chipText: { fontSize: 13, color: '#475569' },
  chipTextActive: { color: '#fff' },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 14, marginBottom: 10, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  row: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  deviceId: { fontSize: 15, fontWeight: '600', color: '#1e293b' },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 10 },
  badgeText: { fontSize: 11, color: '#fff', fontWeight: '600' },
  sub: { fontSize: 13, color: '#64748b', marginTop: 4 },
});
