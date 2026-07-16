// GOTV Contacts — manage voter contacts with demographic data and pledge status.

import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity,
  RefreshControl, TextInput, Platform,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect } from 'expo-router';
import { EmptyState } from '../src/components/EmptyState';
import { CardSkeleton } from '../src/components/SkeletonLoader';
import { gotvFetch } from '../lib/gotv-auth';

interface Contact {
  contact_id: string;
  full_name: string;
  state_code: string;
  lga_code: string;
  ward_code: string;
  polling_unit_code: string;
  status: string;
  pledge_status: string;
  age_group: string;
  gender: string;
  tags: string[];
}

const STATUS_COLORS: Record<string, string> = {
  active: '#22c55e',
  contacted: '#3b82f6',
  pledged: '#8b5cf6',
  unresponsive: '#6b7280',
};

export default function GOTVContactsScreen() {
  const [contacts, setContacts] = useState<Contact[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState<string>('all');

  const load = useCallback(async () => {
    try {
      const data = await gotvFetch<{ contacts: Contact[] }>('/gotv/contacts');
      setContacts(data.contacts || []);
    } catch { /* empty */ }
    setLoading(false);
  }, []);

  useFocusEffect(useCallback(() => { load(); }, [load]));

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    if (Platform.OS !== 'web') Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await load();
    setRefreshing(false);
  }, [load]);

  const filtered = contacts.filter(c => {
    if (filter !== 'all' && c.status !== filter) return false;
    if (search && !c.full_name.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  if (loading) return <CardSkeleton />;

  return (
    <ScrollView
      style={styles.container}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#006b3f" />}
    >
      <View style={styles.header}>
        <Text style={styles.title}>Contacts</Text>
        <Text style={styles.count}>{contacts.length} total</Text>
      </View>

      <View style={styles.searchRow}>
        <Ionicons name="search" size={18} color="#9ca3af" style={styles.searchIcon} />
        <TextInput
          style={styles.searchInput}
          placeholder="Search contacts..."
          value={search}
          onChangeText={setSearch}
          placeholderTextColor="#9ca3af"
        />
      </View>

      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.filterRow}>
        {['all', 'active', 'contacted', 'pledged', 'unresponsive'].map(f => (
          <TouchableOpacity
            key={f}
            style={[styles.filterChip, filter === f && styles.filterChipActive]}
            onPress={() => setFilter(f)}
          >
            <Text style={[styles.filterText, filter === f && styles.filterTextActive]}>
              {f === 'all' ? 'All' : f.charAt(0).toUpperCase() + f.slice(1)}
            </Text>
          </TouchableOpacity>
        ))}
      </ScrollView>

      {filtered.length === 0 ? (
        <EmptyState icon="people-outline" title="No contacts" description={search ? 'Try a different search' : 'No contacts found'} />
      ) : (
        filtered.slice(0, 50).map(c => (
          <View key={c.contact_id} style={styles.card}>
            <View style={styles.cardRow}>
              <View style={[styles.avatar, { backgroundColor: STATUS_COLORS[c.status] || '#6b7280' }]}>
                <Text style={styles.avatarText}>{(c.full_name || '?')[0].toUpperCase()}</Text>
              </View>
              <View style={styles.cardContent}>
                <Text style={styles.name} numberOfLines={1}>{c.full_name || '(Encrypted)'}</Text>
                <View style={styles.metaRow}>
                  <Text style={styles.meta}>{c.state_code || 'N/A'}</Text>
                  {c.lga_code && <Text style={styles.meta}> · {c.lga_code}</Text>}
                  {c.age_group && <Text style={styles.meta}> · {c.age_group}</Text>}
                  {c.gender && <Text style={styles.meta}> · {c.gender}</Text>}
                </View>
                {c.tags && c.tags.length > 0 && (
                  <View style={styles.tagsRow}>
                    {c.tags.slice(0, 3).map(t => (
                      <View key={t} style={styles.tag}>
                        <Text style={styles.tagText}>{t}</Text>
                      </View>
                    ))}
                  </View>
                )}
              </View>
              <View style={styles.statusCol}>
                <View style={[styles.statusDot, { backgroundColor: STATUS_COLORS[c.status] || '#6b7280' }]} />
                <Text style={styles.statusText}>{c.status}</Text>
              </View>
            </View>
          </View>
        ))
      )}
      {filtered.length > 50 && (
        <Text style={styles.moreText}>Showing 50 of {filtered.length} contacts</Text>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', padding: 16, paddingBottom: 8 },
  title: { fontSize: 22, fontWeight: '700', color: '#111827' },
  count: { fontSize: 14, color: '#6b7280' },
  searchRow: { flexDirection: 'row', alignItems: 'center', marginHorizontal: 16, marginBottom: 10, backgroundColor: '#fff', borderRadius: 10, borderWidth: 1, borderColor: '#e5e7eb' },
  searchIcon: { paddingLeft: 12 },
  searchInput: { flex: 1, paddingHorizontal: 10, paddingVertical: 10, fontSize: 15, color: '#111827' },
  filterRow: { paddingHorizontal: 16, marginBottom: 12, maxHeight: 36 },
  filterChip: { paddingHorizontal: 14, paddingVertical: 6, borderRadius: 16, backgroundColor: '#f3f4f6', marginRight: 8 },
  filterChipActive: { backgroundColor: '#006b3f' },
  filterText: { fontSize: 13, color: '#374151' },
  filterTextActive: { color: '#fff', fontWeight: '600' },
  card: { backgroundColor: '#fff', marginHorizontal: 16, marginBottom: 8, borderRadius: 10, padding: 12, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.04, shadowRadius: 2, elevation: 1 },
  cardRow: { flexDirection: 'row', alignItems: 'center' },
  avatar: { width: 40, height: 40, borderRadius: 20, alignItems: 'center', justifyContent: 'center' },
  avatarText: { color: '#fff', fontWeight: '700', fontSize: 16 },
  cardContent: { flex: 1, marginLeft: 12 },
  name: { fontSize: 15, fontWeight: '600', color: '#111827' },
  metaRow: { flexDirection: 'row', marginTop: 2 },
  meta: { fontSize: 12, color: '#6b7280' },
  tagsRow: { flexDirection: 'row', marginTop: 4, gap: 4 },
  tag: { backgroundColor: '#e0f2fe', paddingHorizontal: 6, paddingVertical: 2, borderRadius: 4 },
  tagText: { fontSize: 10, color: '#0369a1', fontWeight: '500' },
  statusCol: { alignItems: 'center' },
  statusDot: { width: 8, height: 8, borderRadius: 4, marginBottom: 2 },
  statusText: { fontSize: 10, color: '#6b7280', textTransform: 'capitalize' },
  moreText: { textAlign: 'center', color: '#6b7280', fontSize: 13, paddingVertical: 16 },
});
