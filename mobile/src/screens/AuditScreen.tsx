import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, RefreshControl } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface AuditEntry {
  id: number;
  action: string;
  entity_type: string;
  entity_id: string;
  user_id: string;
  ip_address: string;
  timestamp: string;
  details: string;
}

export default function AuditScreen() {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const data = await apiGet('/audit?election_id=1&page=1&limit=50');
      setEntries(data.entries || data.audit_entries || []);
    } catch {}
  };

  useEffect(() => { load(); }, []);

  return (
    <FlatList
      data={entries}
      keyExtractor={(item) => item.id?.toString() || Math.random().toString()}
      renderItem={({ item }) => (
        <View style={s.card}>
          <View style={s.row}>
            <Ionicons name="document-text" size={16} color="#2563eb" />
            <Text style={s.action}>{item.action}</Text>
            <Text style={s.time}>{new Date(item.timestamp).toLocaleTimeString()}</Text>
          </View>
          <Text style={s.detail}>{item.entity_type} / {item.entity_id}</Text>
          {item.details && <Text style={s.meta}>{item.details}</Text>}
        </View>
      )}
      contentContainerStyle={s.list}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}
      ListEmptyComponent={<Text style={s.empty}>No audit entries</Text>}
    />
  );
}

const s = StyleSheet.create({
  list: { padding: 16, gap: 8 },
  card: { backgroundColor: '#fff', borderRadius: 12, padding: 12, elevation: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  action: { flex: 1, fontSize: 14, fontWeight: '600', color: '#1e293b' },
  time: { fontSize: 11, color: '#94a3b8' },
  detail: { fontSize: 12, color: '#64748b', marginTop: 4 },
  meta: { fontSize: 11, color: '#94a3b8', marginTop: 2 },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
