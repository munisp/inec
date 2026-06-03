import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, FlatList, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { voterApi, Voter } from '../src/lib/api';

export default function VoterSearchScreen() {
  const [query, setQuery] = useState('');
  const [voters, setVoters] = useState<Voter[]>([]);
  const [loading, setLoading] = useState(false);

  const handleSearch = async () => {
    if (!query.trim()) return;
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const results = await voterApi.search(query);
      setVoters(Array.isArray(results) ? results : []);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Search failed');
      setVoters([]);
    }
    setLoading(false);
  };

  const renderVoter = ({ item }: { item: Voter }) => (
    <View style={styles.voterCard}>
      <View style={styles.voterHeader}>
        <View style={styles.avatarCircle}>
          <Ionicons name="person-outline" size={20} color="#166534" />
        </View>
        <View style={{ flex: 1 }}>
          <Text style={styles.voterName}>{item.full_name}</Text>
          <Text style={styles.voterVin}>{item.vin}</Text>
        </View>
        <View style={[styles.genderBadge, { backgroundColor: item.gender === 'M' ? '#dbeafe' : '#fce7f3' }]}>
          <Text style={{ fontSize: 12, fontWeight: '600', color: item.gender === 'M' ? '#2563eb' : '#be185d' }}>{item.gender}</Text>
        </View>
      </View>
      <View style={styles.voterDetails}>
        <View style={styles.detailRow}>
          <Ionicons name="location-outline" size={14} color="#6b7280" />
          <Text style={styles.detailText}>{item.state} → {item.lga} → {item.ward}</Text>
        </View>
        <View style={styles.detailRow}>
          <Ionicons name="business-outline" size={14} color="#6b7280" />
          <Text style={styles.detailText}>PU: {item.polling_unit_code}</Text>
        </View>
        <View style={styles.detailRow}>
          <Ionicons name="calendar-outline" size={14} color="#6b7280" />
          <Text style={styles.detailText}>DOB: {item.date_of_birth}</Text>
        </View>
      </View>
    </View>
  );

  return (
    <View style={styles.container}>
      <View style={styles.searchBar}>
        <Ionicons name="search-outline" size={20} color="#9ca3af" />
        <TextInput
          style={styles.searchInput}
          placeholder="Search by name or VIN..."
          value={query}
          onChangeText={setQuery}
          onSubmitEditing={handleSearch}
          returnKeyType="search"
          placeholderTextColor="#9ca3af"
        />
        <TouchableOpacity onPress={handleSearch} disabled={loading} style={styles.searchButton}>
          <Text style={styles.searchButtonText}>{loading ? '...' : 'Search'}</Text>
        </TouchableOpacity>
      </View>

      {voters.length === 0 && !loading ? (
        <View style={styles.emptyState}>
          <Ionicons name="people-outline" size={48} color="#d1d5db" />
          <Text style={styles.emptyText}>Search for voters by name or VIN number</Text>
        </View>
      ) : (
        <FlatList
          data={voters}
          keyExtractor={(item) => String(item.id)}
          renderItem={renderVoter}
          contentContainerStyle={{ padding: 16, paddingBottom: 40 }}
          ItemSeparatorComponent={() => <View style={{ height: 12 }} />}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  searchBar: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#fff', margin: 16, borderRadius: 12, paddingHorizontal: 12, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  searchInput: { flex: 1, fontSize: 15, paddingVertical: 14, paddingHorizontal: 8, color: '#111827' },
  searchButton: { backgroundColor: '#166534', paddingHorizontal: 14, paddingVertical: 8, borderRadius: 8 },
  searchButtonText: { color: '#fff', fontSize: 14, fontWeight: '600' },
  emptyState: { flex: 1, justifyContent: 'center', alignItems: 'center', paddingBottom: 100 },
  emptyText: { fontSize: 14, color: '#9ca3af', marginTop: 12 },
  voterCard: { backgroundColor: '#fff', borderRadius: 16, padding: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  voterHeader: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  avatarCircle: { width: 40, height: 40, borderRadius: 20, backgroundColor: '#dcfce7', justifyContent: 'center', alignItems: 'center' },
  voterName: { fontSize: 16, fontWeight: '600', color: '#111827' },
  voterVin: { fontSize: 12, color: '#6b7280', fontFamily: 'monospace', marginTop: 2 },
  genderBadge: { paddingHorizontal: 8, paddingVertical: 4, borderRadius: 8 },
  voterDetails: { marginTop: 12, gap: 6 },
  detailRow: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  detailText: { fontSize: 13, color: '#6b7280' },
});
