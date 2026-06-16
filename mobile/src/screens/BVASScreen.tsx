import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, RefreshControl, TouchableOpacity } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface BVASDevice {
  serial_number: string;
  firmware_version: string;
  assigned_pu: string;
  status: string;
  battery_level: number;
  last_sync: string;
  total_accreditations: number;
}

export default function BVASScreen() {
  const [devices, setDevices] = useState<BVASDevice[]>([]);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const data = await apiGet('/bvas/devices?election_id=1');
      setDevices(data.devices || []);
    } catch {}
  };

  useEffect(() => { load(); }, []);

  return (
    <FlatList
      data={devices}
      keyExtractor={(item) => item.serial_number}
      renderItem={({ item }) => (
        <View style={s.card}>
          <View style={s.row}>
            <Ionicons name="hardware-chip-outline" size={20} color="#2563eb" />
            <Text style={s.serial}>{item.serial_number}</Text>
            <View style={[s.badge, { backgroundColor: item.status === 'active' ? '#dcfce7' : '#fef3c7' }]}>
              <Text style={[s.badgeText, { color: item.status === 'active' ? '#15803d' : '#ca8a04' }]}>{item.status}</Text>
            </View>
          </View>
          <Text style={s.info}>PU: {item.assigned_pu} | FW: {item.firmware_version}</Text>
          <View style={s.statsRow}>
            <View style={s.stat}>
              <Ionicons name="battery-half" size={14} color={item.battery_level > 20 ? '#15803d' : '#dc2626'} />
              <Text style={s.statText}>{item.battery_level}%</Text>
            </View>
            <View style={s.stat}>
              <Ionicons name="people" size={14} color="#2563eb" />
              <Text style={s.statText}>{item.total_accreditations} accredited</Text>
            </View>
          </View>
        </View>
      )}
      contentContainerStyle={s.list}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}
      ListEmptyComponent={<Text style={s.empty}>No BVAS devices found</Text>}
    />
  );
}

const s = StyleSheet.create({
  list: { padding: 16, gap: 10 },
  card: { backgroundColor: '#fff', borderRadius: 12, padding: 14, elevation: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  serial: { flex: 1, fontSize: 14, fontWeight: '600', color: '#1e293b' },
  badge: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: 6 },
  badgeText: { fontSize: 10, fontWeight: '700', textTransform: 'uppercase' },
  info: { fontSize: 12, color: '#64748b', marginTop: 6 },
  statsRow: { flexDirection: 'row', gap: 16, marginTop: 8 },
  stat: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  statText: { fontSize: 12, color: '#475569' },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
