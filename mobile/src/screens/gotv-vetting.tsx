/**
 * Mobile Vetting Pipeline Screen
 * Shows volunteer's own vetting status + camera capture for NIN verification
 */
import React, { useState, useEffect } from 'react';
import { View, Text, ScrollView, TouchableOpacity, StyleSheet, Alert, FlatList } from 'react-native';

const API_BASE = 'http://localhost:8103';

interface VettingVolunteer {
  volunteer_id: string;
  full_name: string;
  vetting_status: string;
  role: string;
  assigned_state: string;
}

export default function GOTVVettingScreen() {
  const [volunteers, setVolunteers] = useState<VettingVolunteer[]>([]);
  const [filter, setFilter] = useState<string>('all');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetchPipeline();
  }, []);

  const fetchPipeline = async () => {
    try {
      const res = await fetch(`${API_BASE}/gotv/volunteers/vetting`, {
        headers: { 'X-GOTV-Party-Code': 'APC' },
      });
      const data = await res.json();
      setVolunteers(data.volunteers || []);
    } catch (e) {
      console.error(e);
    }
    setLoading(false);
  };

  const advanceStatus = async (id: string, action: string) => {
    try {
      const endpoint = action === 'verify-nin' ? 'verify-nin'
        : action === 'training' ? 'training'
        : action === 'approve' ? 'approve' : 'reject';
      await fetch(`${API_BASE}/gotv/volunteers/${id}/${endpoint}`, {
        method: 'POST',
        headers: { 'X-GOTV-Party-Code': 'APC', 'Content-Type': 'application/json' },
        body: JSON.stringify({ verified_by: 'mobile_coordinator' }),
      });
      Alert.alert('Success', `Volunteer ${action} completed`);
      fetchPipeline();
    } catch {
      Alert.alert('Error', 'Action failed');
    }
  };

  const statusColors: Record<string, string> = {
    pending: '#f59e0b',
    nin_verified: '#3b82f6',
    trained: '#8b5cf6',
    approved: '#10b981',
    suspended: '#ef4444',
    rejected: '#6b7280',
  };

  const filtered = filter === 'all' ? volunteers : volunteers.filter(v => v.vetting_status === filter);

  return (
    <ScrollView style={styles.container}>
      <Text style={styles.title}>Vetting Pipeline</Text>

      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.filterRow}>
        {['all', 'pending', 'nin_verified', 'trained', 'approved', 'suspended', 'rejected'].map(f => (
          <TouchableOpacity
            key={f}
            style={[styles.filterChip, filter === f && styles.filterActive]}
            onPress={() => setFilter(f)}
          >
            <Text style={[styles.filterText, filter === f && styles.filterTextActive]}>
              {f === 'all' ? 'All' : f.replace('_', ' ').toUpperCase()} ({f === 'all' ? volunteers.length : volunteers.filter(v => v.vetting_status === f).length})
            </Text>
          </TouchableOpacity>
        ))}
      </ScrollView>

      {loading ? (
        <Text style={styles.loading}>Loading...</Text>
      ) : (
        <FlatList
          data={filtered}
          scrollEnabled={false}
          keyExtractor={v => v.volunteer_id}
          renderItem={({ item: v }) => (
            <View style={styles.card}>
              <View style={styles.cardHeader}>
                <Text style={styles.name}>{v.full_name}</Text>
                <View style={[styles.badge, { backgroundColor: statusColors[v.vetting_status] || '#6b7280' }]}>
                  <Text style={styles.badgeText}>{v.vetting_status}</Text>
                </View>
              </View>
              <Text style={styles.detail}>{v.role} • {v.assigned_state || 'Unassigned'}</Text>
              <View style={styles.actions}>
                {v.vetting_status === 'pending' && (
                  <TouchableOpacity style={styles.actionBtn} onPress={() => advanceStatus(v.volunteer_id, 'verify-nin')}>
                    <Text style={styles.actionText}>Verify NIN</Text>
                  </TouchableOpacity>
                )}
                {v.vetting_status === 'nin_verified' && (
                  <TouchableOpacity style={styles.actionBtn} onPress={() => advanceStatus(v.volunteer_id, 'training')}>
                    <Text style={styles.actionText}>Mark Trained</Text>
                  </TouchableOpacity>
                )}
                {v.vetting_status === 'trained' && (
                  <>
                    <TouchableOpacity style={[styles.actionBtn, { backgroundColor: '#10b981' }]} onPress={() => advanceStatus(v.volunteer_id, 'approve')}>
                      <Text style={styles.actionText}>Approve</Text>
                    </TouchableOpacity>
                    <TouchableOpacity style={[styles.actionBtn, { backgroundColor: '#ef4444' }]} onPress={() => advanceStatus(v.volunteer_id, 'reject')}>
                      <Text style={styles.actionText}>Reject</Text>
                    </TouchableOpacity>
                  </>
                )}
              </View>
            </View>
          )}
        />
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  title: { fontSize: 24, fontWeight: 'bold', marginBottom: 12 },
  filterRow: { flexDirection: 'row', marginBottom: 12 },
  filterChip: { paddingHorizontal: 12, paddingVertical: 6, backgroundColor: '#e5e7eb', borderRadius: 20, marginRight: 8 },
  filterActive: { backgroundColor: '#3b82f6' },
  filterText: { fontSize: 12, color: '#374151' },
  filterTextActive: { color: '#fff' },
  loading: { textAlign: 'center', marginTop: 40, color: '#6b7280' },
  card: { backgroundColor: '#fff', borderRadius: 8, padding: 16, marginBottom: 8, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  cardHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  name: { fontSize: 16, fontWeight: '600' },
  badge: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: 12 },
  badgeText: { color: '#fff', fontSize: 11, fontWeight: '600' },
  detail: { fontSize: 13, color: '#6b7280', marginTop: 4 },
  actions: { flexDirection: 'row', gap: 8, marginTop: 12 },
  actionBtn: { backgroundColor: '#3b82f6', paddingHorizontal: 16, paddingVertical: 8, borderRadius: 6 },
  actionText: { color: '#fff', fontWeight: '600', fontSize: 13 },
});
