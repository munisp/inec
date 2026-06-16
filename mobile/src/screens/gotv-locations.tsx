/**
 * Mobile Locations Screen
 * Map view of assigned ward/PU with capacity information
 */
import React, { useState, useEffect } from 'react';
import { View, Text, ScrollView, TouchableOpacity, StyleSheet, FlatList } from 'react-native';

const API_BASE = 'http://localhost:8103';

interface LocationCapacity {
  state: string;
  lga: string;
  ward: string;
  volunteer_count: number;
  contact_count: number;
  coverage_pct: number;
  roles: Record<string, number>;
}

export default function GOTVLocationsScreen() {
  const [locations, setLocations] = useState<LocationCapacity[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => { fetchLocations(); }, []);

  const fetchLocations = async () => {
    try {
      const res = await fetch(`${API_BASE}/gotv/locations/capacity`, {
        headers: { 'X-GOTV-Party-Code': 'APC' },
      });
      const data = await res.json();
      setLocations(data.locations || []);
    } catch { /* ignore */ }
    setLoading(false);
  };

  const coverageColor = (pct: number) => pct >= 70 ? '#10b981' : pct >= 40 ? '#f59e0b' : '#ef4444';

  return (
    <ScrollView style={styles.container}>
      <Text style={styles.title}>Location Assignments</Text>
      <Text style={styles.subtitle}>{locations.length} locations with volunteer assignments</Text>

      {loading ? (
        <Text style={styles.loading}>Loading...</Text>
      ) : (
        <FlatList
          data={locations}
          scrollEnabled={false}
          keyExtractor={(_, i) => String(i)}
          renderItem={({ item: loc }) => (
            <View style={styles.card}>
              <View style={styles.row}>
                <View style={{ flex: 1 }}>
                  <Text style={styles.ward}>{loc.ward || 'Unassigned'}</Text>
                  <Text style={styles.meta}>{loc.state} • {loc.lga}</Text>
                </View>
                <View style={[styles.coverageBadge, { backgroundColor: coverageColor(loc.coverage_pct) }]}>
                  <Text style={styles.coverageText}>{loc.coverage_pct}%</Text>
                </View>
              </View>
              <View style={styles.statsRow}>
                <View style={styles.stat}>
                  <Text style={styles.statNum}>{loc.volunteer_count}</Text>
                  <Text style={styles.statLabel}>Volunteers</Text>
                </View>
                <View style={styles.stat}>
                  <Text style={styles.statNum}>{loc.contact_count}</Text>
                  <Text style={styles.statLabel}>Contacts</Text>
                </View>
                <View style={styles.stat}>
                  <Text style={styles.statNum}>{loc.contact_count > 0 ? Math.round(loc.volunteer_count / loc.contact_count * 100) : 0}%</Text>
                  <Text style={styles.statLabel}>Ratio</Text>
                </View>
              </View>
              {loc.roles && Object.keys(loc.roles).length > 0 && (
                <View style={styles.rolesRow}>
                  {Object.entries(loc.roles).map(([role, count]) => (
                    <View key={role} style={styles.roleChip}>
                      <Text style={styles.roleText}>{role}: {count}</Text>
                    </View>
                  ))}
                </View>
              )}
            </View>
          )}
        />
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  title: { fontSize: 24, fontWeight: 'bold' },
  subtitle: { fontSize: 13, color: '#6b7280', marginBottom: 12 },
  loading: { textAlign: 'center', marginTop: 40, color: '#6b7280' },
  card: { backgroundColor: '#fff', borderRadius: 8, padding: 16, marginBottom: 8, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  row: { flexDirection: 'row', alignItems: 'center' },
  ward: { fontSize: 16, fontWeight: '600' },
  meta: { fontSize: 12, color: '#6b7280' },
  coverageBadge: { paddingHorizontal: 10, paddingVertical: 4, borderRadius: 12 },
  coverageText: { color: '#fff', fontSize: 13, fontWeight: '700' },
  statsRow: { flexDirection: 'row', justifyContent: 'space-around', marginTop: 12, paddingTop: 12, borderTopWidth: 1, borderTopColor: '#f3f4f6' },
  stat: { alignItems: 'center' },
  statNum: { fontSize: 18, fontWeight: '700' },
  statLabel: { fontSize: 11, color: '#9ca3af' },
  rolesRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 6, marginTop: 8 },
  roleChip: { backgroundColor: '#eff6ff', paddingHorizontal: 8, paddingVertical: 3, borderRadius: 12 },
  roleText: { fontSize: 11, color: '#3b82f6' },
});
