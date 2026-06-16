import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { API_URL as API } from '../src/lib/api';

interface BlockRecord { tx_hash: string; block_number: number; action: string; entity_type: string; verified: boolean; timestamp: string; }

export default function BlockchainScreen() {
  const [records, setRecords] = useState<BlockRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const res = await fetch(`${API}/blockchain/audit`);
      if (res.ok) { const d = await res.json(); setRecords(Array.isArray(d) ? d : d.records || []); }
    } catch (e) { console.error('Blockchain load:', e); }
    setLoading(false); setRefreshing(false);
  };

  useEffect(() => { load(); }, []);
  if (loading) return <View style={s.center}><ActivityIndicator size="large" color="#16a34a" /></View>;

  return (
    <View style={s.container}>
      <Text style={s.title}>Blockchain Audit Trail</Text>
      <Text style={s.count}>{records.length} records</Text>
      <FlatList data={records} keyExtractor={(_, i) => String(i)}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => { setRefreshing(true); load(); }} />}
        renderItem={({ item }) => (
          <View style={s.card}>
            <View style={s.row}>
              <Text style={s.action}>{item.action}</Text>
              <Text style={[s.badge, item.verified ? s.verified : s.unverified]}>{item.verified ? 'Verified' : 'Pending'}</Text>
            </View>
            <Text style={s.sub}>Block #{item.block_number} · {item.entity_type}</Text>
            <Text style={s.hash} numberOfLines={1}>{item.tx_hash}</Text>
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
  action: { fontSize: 15, fontWeight: '600', color: '#1e293b' },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 10, fontSize: 11, overflow: 'hidden' },
  verified: { backgroundColor: '#dcfce7', color: '#16a34a' },
  unverified: { backgroundColor: '#fef3c7', color: '#d97706' },
  sub: { fontSize: 13, color: '#64748b', marginTop: 4 },
  hash: { fontSize: 11, color: '#94a3b8', marginTop: 4, fontFamily: 'monospace' },
});
