import { useState, useEffect } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, Platform, ActivityIndicator } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';

interface AuditEntry {
  id: number;
  action: string;
  entity_type: string;
  entity_id: string;
  user_id: number;
  details: Record<string, string>;
  block_hash: string;
  created_at: string;
}

export default function AuditScreen() {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [search, setSearch] = useState('');
  const [filterAction, setFilterAction] = useState('all');

  const loadAudit = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const data = await api<{ entries: AuditEntry[] }>('/audit/log?limit=50');
      setEntries(data.entries || []);
    } catch { /* ignore */ }
    setLoading(false);
  };

  useEffect(() => { loadAudit(); }, []);

  const actionIcon = (a: string): keyof typeof Ionicons.glyphMap => {
    if (a.includes('SUBMIT')) return 'cloud-upload';
    if (a.includes('LOGIN')) return 'log-in';
    if (a.includes('VALIDATE')) return 'checkmark-circle';
    if (a.includes('FINALIZE')) return 'shield-checkmark';
    if (a.includes('DELETE')) return 'trash';
    if (a.includes('CREATE')) return 'add-circle';
    if (a.includes('UPDATE')) return 'create';
    return 'document-text';
  };

  const actionTypes = ['all', ...new Set(entries.map(e => e.action.split('_')[0]))];

  const filtered = entries.filter(e => {
    if (filterAction !== 'all' && !e.action.startsWith(filterAction)) return false;
    if (search) {
      const q = search.toLowerCase();
      return e.action?.toLowerCase().includes(q) || e.entity_type?.toLowerCase().includes(q) || e.entity_id?.toLowerCase().includes(q);
    }
    return true;
  });

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}>
      <View style={styles.header}>
        <Ionicons name="list" size={28} color="#166534" />
        <Text style={styles.title}>Audit Trail</Text>
        <TouchableOpacity onPress={loadAudit}>
          <Ionicons name="refresh" size={22} color="#64748b" />
        </TouchableOpacity>
      </View>

      <View style={styles.searchBox}>
        <Ionicons name="search" size={18} color="#94a3b8" />
        <TextInput style={styles.searchInput} placeholder="Search audit log..." value={search} onChangeText={setSearch} />
      </View>

      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ paddingHorizontal: 16, marginBottom: 12 }}>
        {actionTypes.slice(0, 8).map(a => (
          <TouchableOpacity key={a} style={[styles.filterChip, filterAction === a && styles.filterChipActive]} onPress={() => setFilterAction(a)}>
            <Text style={[styles.filterChipText, filterAction === a && styles.filterChipTextActive]}>{a === 'all' ? 'All' : a}</Text>
          </TouchableOpacity>
        ))}
      </ScrollView>

      <Text style={styles.countText}>{filtered.length} of {entries.length} entries</Text>

      {loading && <ActivityIndicator size="large" color="#166534" style={{ marginTop: 40 }} />}

      {filtered.map(e => (
        <View key={e.id} style={styles.entryCard}>
          <View style={styles.iconCol}>
            <Ionicons name={actionIcon(e.action)} size={20} color="#166534" />
          </View>
          <View style={{ flex: 1 }}>
            <Text style={styles.action}>{e.action.replace(/_/g, ' ')}</Text>
            <Text style={styles.entityInfo}>{e.entity_type} #{e.entity_id} — User {e.user_id}</Text>
            {e.block_hash && <Text style={styles.hash}>{e.block_hash.substring(0, 16)}...</Text>}
            <Text style={styles.time}>{new Date(e.created_at).toLocaleString()}</Text>
          </View>
        </View>
      ))}

      {!loading && filtered.length === 0 && (
        <View style={{ alignItems: 'center', marginTop: 40 }}>
          <Ionicons name="document-text-outline" size={48} color="#94a3b8" />
          <Text style={{ color: '#64748b', marginTop: 8 }}>No audit entries match your search</Text>
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 16, paddingTop: Platform.OS === 'ios' ? 60 : 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', flex: 1 },
  searchBox: { flexDirection: 'row', alignItems: 'center', marginHorizontal: 16, marginBottom: 10, padding: 10, backgroundColor: '#fff', borderRadius: 10, borderWidth: 1, borderColor: '#e2e8f0', gap: 8 },
  searchInput: { flex: 1, fontSize: 14 },
  filterChip: { paddingHorizontal: 12, paddingVertical: 6, borderRadius: 16, backgroundColor: '#f1f5f9', marginRight: 8 },
  filterChipActive: { backgroundColor: '#166534' },
  filterChipText: { fontSize: 12, fontWeight: '600', color: '#64748b', textTransform: 'uppercase' },
  filterChipTextActive: { color: '#fff' },
  countText: { fontSize: 13, color: '#64748b', marginHorizontal: 16, marginBottom: 8 },
  entryCard: { flexDirection: 'row', marginHorizontal: 16, marginBottom: 8, padding: 14, backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#e2e8f0' },
  iconCol: { width: 36, height: 36, borderRadius: 18, backgroundColor: '#dcfce7', alignItems: 'center', justifyContent: 'center', marginRight: 12 },
  action: { fontSize: 14, fontWeight: '600', color: '#1e293b', textTransform: 'capitalize' },
  entityInfo: { fontSize: 12, color: '#64748b', marginTop: 2 },
  hash: { fontSize: 11, color: '#3b82f6', marginTop: 2, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  time: { fontSize: 11, color: '#94a3b8', marginTop: 4 },
});
