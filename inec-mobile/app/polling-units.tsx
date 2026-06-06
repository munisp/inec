import { useState, useEffect } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, Platform, ActivityIndicator } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';

interface PollingUnit {
  code: string;
  name: string;
  ward_code: string;
  registered_voters: number;
  latitude: string;
  longitude: string;
}

export default function PollingUnitsScreen() {
  const [units, setUnits] = useState<PollingUnit[]>([]);
  const [loading, setLoading] = useState(false);
  const [search, setSearch] = useState('');
  const [total, setTotal] = useState(0);

  const loadPUs = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const data = await api<{ polling_units: PollingUnit[]; total: number }>('/polling-units?limit=50');
      setUnits(data.polling_units || []);
      setTotal(data.total || 0);
    } catch { /* ignore */ }
    setLoading(false);
  };

  useEffect(() => { loadPUs(); }, []);

  const filtered = search
    ? units.filter(u => u.code.toLowerCase().includes(search.toLowerCase()) || u.name.toLowerCase().includes(search.toLowerCase()))
    : units;

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}>
      <View style={styles.header}>
        <Ionicons name="location" size={28} color="#166534" />
        <Text style={styles.title}>Polling Units</Text>
      </View>

      <View style={styles.searchBox}>
        <Ionicons name="search" size={18} color="#94a3b8" />
        <TextInput style={styles.searchInput} placeholder="Search by code or name..." value={search} onChangeText={setSearch} />
      </View>

      <Text style={styles.countText}>{total.toLocaleString()} total polling units</Text>

      {loading && <ActivityIndicator size="large" color="#166534" style={{ marginTop: 24 }} />}

      {filtered.map(pu => (
        <View key={pu.code} style={styles.puCard}>
          <View style={styles.puHeader}>
            <Ionicons name="pin" size={16} color="#166534" />
            <Text style={styles.puCode}>{pu.code}</Text>
          </View>
          <Text style={styles.puName}>{pu.name}</Text>
          <View style={styles.puMeta}>
            <Text style={styles.puMetaItem}>Ward: {pu.ward_code}</Text>
            <Text style={styles.puMetaItem}>Reg: {pu.registered_voters}</Text>
          </View>
        </View>
      ))}

      <TouchableOpacity style={styles.refreshBtn} onPress={loadPUs}>
        <Ionicons name="refresh" size={18} color="#fff" />
        <Text style={styles.refreshText}>Refresh</Text>
      </TouchableOpacity>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 16, paddingTop: Platform.OS === 'ios' ? 60 : 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b' },
  searchBox: { flexDirection: 'row', alignItems: 'center', marginHorizontal: 16, padding: 10, backgroundColor: '#fff', borderRadius: 10, borderWidth: 1, borderColor: '#e2e8f0', gap: 8 },
  searchInput: { flex: 1, fontSize: 14 },
  countText: { fontSize: 13, color: '#64748b', marginHorizontal: 16, marginVertical: 8 },
  puCard: { marginHorizontal: 16, marginBottom: 8, padding: 14, backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#e2e8f0' },
  puHeader: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  puCode: { fontSize: 13, fontWeight: '700', color: '#166534', fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  puName: { fontSize: 15, fontWeight: '600', color: '#1e293b', marginTop: 4 },
  puMeta: { flexDirection: 'row', gap: 16, marginTop: 6 },
  puMetaItem: { fontSize: 12, color: '#64748b' },
  refreshBtn: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 6, margin: 16, padding: 14, backgroundColor: '#166534', borderRadius: 12 },
  refreshText: { color: '#fff', fontWeight: '600', fontSize: 15 },
});
