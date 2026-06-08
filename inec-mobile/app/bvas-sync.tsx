import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, ActivityIndicator, RefreshControl, TouchableOpacity, Alert } from 'react-native';
import { API_URL as API } from '../src/lib/api';

interface SyncRecord { device_id: string; status: string; last_sync: string; records_pending: number; records_synced: number; battery: number; }

export default function BVASSyncScreen() {
  const [records, setRecords] = useState<SyncRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try { const r = await fetch(`${API}/bvas/sync-status`); if (r.ok) { const d = await r.json(); setRecords(Array.isArray(d) ? d : d.devices || []); } } catch (e) { console.error(e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  const triggerSync = async (deviceId: string) => {
    try { await fetch(`${API}/bvas/sync`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ device_id: deviceId }) }); Alert.alert('Sync', 'Sync triggered'); load(); } catch (e) { Alert.alert('Error', 'Sync failed'); }
  };

  return (
    <View style={s.container}>
      <Text style={s.title}>BVAS Data Sync</Text>
      <Text style={s.count}>{records.length} devices · {records.reduce((a, r) => a + (r.records_pending || 0), 0)} pending</Text>
      <FlatList data={records} keyExtractor={r => r.device_id}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}
        renderItem={({ item }) => (
          <View style={s.card}>
            <View style={s.row}>
              <Text style={s.deviceId}>{item.device_id}</Text>
              <View style={[s.badge, { backgroundColor: item.status === 'synced' ? '#dcfce7' : item.status === 'syncing' ? '#dbeafe' : '#fef3c7' }]}>
                <Text style={{ fontSize: 11, fontWeight: '600', color: item.status === 'synced' ? '#16a34a' : item.status === 'syncing' ? '#3b82f6' : '#d97706' }}>{item.status}</Text>
              </View>
            </View>
            <View style={s.stats}>
              <Text style={s.sub}>Synced: {item.records_synced}</Text>
              <Text style={s.sub}>Pending: {item.records_pending}</Text>
              <Text style={s.sub}>Battery: {item.battery}%</Text>
            </View>
            {item.records_pending > 0 && (
              <TouchableOpacity style={s.syncBtn} onPress={() => triggerSync(item.device_id)}>
                <Text style={s.syncBtnText}>Sync Now</Text>
              </TouchableOpacity>
            )}
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
  row: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  deviceId: { fontSize: 15, fontWeight: '600', color: '#1e293b' },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 10 },
  stats: { flexDirection: 'row', gap: 16, marginTop: 8 },
  sub: { fontSize: 12, color: '#64748b' },
  syncBtn: { backgroundColor: '#3b82f6', paddingVertical: 8, borderRadius: 8, marginTop: 10, alignItems: 'center' },
  syncBtnText: { color: '#fff', fontSize: 13, fontWeight: '600' },
});
