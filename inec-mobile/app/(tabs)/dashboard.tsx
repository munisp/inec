import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, RefreshControl, TouchableOpacity,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { useFocusEffect } from 'expo-router';
import { observerApi, PartyDashboard } from '../../src/lib/api';
import { getCurrentLocation } from '../../src/lib/location';

const PARTIES = ['APC', 'PDP', 'LP', 'NNPP', 'ADC', 'SDP', 'APGA', 'YPP'];

export default function DashboardScreen() {
  const [selectedParty, setSelectedParty] = useState('APC');
  const [data, setData] = useState<PartyDashboard | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [checkedIn, setCheckedIn] = useState(false);
  const [checkInStatus, setCheckInStatus] = useState('');

  const loadDashboard = useCallback(async () => {
    try {
      const result = await observerApi.partyDashboard(selectedParty);
      setData(result);
    } catch { /* ignore */ }
  }, [selectedParty]);

  useFocusEffect(useCallback(() => { loadDashboard(); }, [loadDashboard]));

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    await loadDashboard();
    setRefreshing(false);
  }, [loadDashboard]);

  const handleCheckIn = async () => {
    const location = await getCurrentLocation();
    if (!location) {
      setCheckInStatus('Location unavailable');
      return;
    }

    try {
      const result = await observerApi.checkIn('PU-001', location.latitude, location.longitude);
      setCheckedIn(result.within_geofence);
      setCheckInStatus(
        result.within_geofence
          ? `Checked in (${Math.round(result.distance_m)}m from PU)`
          : `Outside geofence (${Math.round(result.distance_m)}m away)`
      );
    } catch (e: unknown) {
      setCheckInStatus(e instanceof Error ? e.message : 'Check-in failed');
    }
  };

  return (
    <ScrollView
      style={styles.container}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} colors={['#166534']} />}
    >
      {/* Check-In Card */}
      <View style={styles.checkInCard}>
        <View style={styles.checkInHeader}>
          <Ionicons name="location" size={20} color={checkedIn ? '#166534' : '#6b7280'} />
          <Text style={styles.checkInTitle}>Polling Unit Check-In</Text>
        </View>
        <TouchableOpacity style={styles.checkInButton} onPress={handleCheckIn}>
          <Ionicons name={checkedIn ? 'checkmark-circle' : 'navigate'} size={18} color="#fff" />
          <Text style={styles.checkInButtonText}>
            {checkedIn ? 'Checked In' : 'Check In Now'}
          </Text>
        </TouchableOpacity>
        {checkInStatus ? (
          <Text style={[styles.checkInStatus, { color: checkedIn ? '#166534' : '#dc2626' }]}>
            {checkInStatus}
          </Text>
        ) : null}
      </View>

      {/* Party Selector */}
      <View style={styles.partySelector}>
        <Text style={styles.sectionTitle}>Select Party</Text>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.partyRow}>
          {PARTIES.map((party) => (
            <TouchableOpacity
              key={party}
              style={[styles.partyChip, selectedParty === party && styles.partyChipActive]}
              onPress={() => setSelectedParty(party)}
            >
              <Text style={[styles.partyChipText, selectedParty === party && styles.partyChipTextActive]}>
                {party}
              </Text>
            </TouchableOpacity>
          ))}
        </ScrollView>
      </View>

      {/* Dashboard Stats */}
      {data && (
        <>
          <View style={styles.statsGrid}>
            <View style={styles.statCard}>
              <Text style={styles.statValue}>{data.total_votes.toLocaleString()}</Text>
              <Text style={styles.statLabel}>Total Votes</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={styles.statValue}>{data.polling_units_with_results.toLocaleString()}</Text>
              <Text style={styles.statLabel}>PUs Reported</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={styles.statValue}>{data.total_polling_units.toLocaleString()}</Text>
              <Text style={styles.statLabel}>Total PUs</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statValue, { color: '#166534' }]}>{data.coverage_pct}%</Text>
              <Text style={styles.statLabel}>Coverage</Text>
            </View>
          </View>

          {/* State Breakdown */}
          {data.state_breakdown.length > 0 && (
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>State Breakdown</Text>
              {data.state_breakdown.slice(0, 10).map((state, i) => (
                <View key={i} style={styles.stateRow}>
                  <Text style={styles.stateName}>{state.state}</Text>
                  <Text style={styles.stateVotes}>{state.votes.toLocaleString()}</Text>
                </View>
              ))}
            </View>
          )}

          {/* Recent Results */}
          {data.recent_results.length > 0 && (
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>Recent Results</Text>
              {data.recent_results.slice(0, 10).map((result, i) => (
                <View key={i} style={styles.resultRow}>
                  <Text style={styles.resultPU}>{result.polling_unit}</Text>
                  <Text style={styles.resultVotes}>{result.votes}</Text>
                </View>
              ))}
            </View>
          )}
        </>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  checkInCard: {
    backgroundColor: '#fff', margin: 12, padding: 16, borderRadius: 12,
    borderWidth: 1, borderColor: '#e5e7eb',
  },
  checkInHeader: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 12 },
  checkInTitle: { fontSize: 16, fontWeight: '600', color: '#1f2937' },
  checkInButton: {
    backgroundColor: '#166534', flexDirection: 'row', alignItems: 'center',
    justifyContent: 'center', gap: 8, padding: 14, borderRadius: 8,
  },
  checkInButtonText: { color: '#fff', fontWeight: '600', fontSize: 15 },
  checkInStatus: { marginTop: 8, fontSize: 13, textAlign: 'center' },
  partySelector: { paddingHorizontal: 12, marginBottom: 8 },
  sectionTitle: { fontSize: 15, fontWeight: '600', color: '#1f2937', marginBottom: 8 },
  partyRow: { gap: 8, paddingVertical: 4 },
  partyChip: {
    paddingHorizontal: 16, paddingVertical: 10, borderRadius: 20,
    backgroundColor: '#fff', borderWidth: 1, borderColor: '#e5e7eb',
  },
  partyChipActive: { backgroundColor: '#166534', borderColor: '#166534' },
  partyChipText: { fontSize: 14, fontWeight: '500', color: '#374151' },
  partyChipTextActive: { color: '#fff' },
  statsGrid: { flexDirection: 'row', flexWrap: 'wrap', padding: 12, gap: 8 },
  statCard: {
    flex: 1, minWidth: '45%', backgroundColor: '#fff', padding: 16, borderRadius: 10,
    alignItems: 'center', borderWidth: 1, borderColor: '#e5e7eb',
  },
  statValue: { fontSize: 22, fontWeight: 'bold', color: '#1f2937' },
  statLabel: { fontSize: 11, color: '#6b7280', marginTop: 4 },
  section: { padding: 12 },
  stateRow: {
    flexDirection: 'row', justifyContent: 'space-between',
    backgroundColor: '#fff', padding: 12, borderRadius: 6, marginBottom: 4,
    borderWidth: 1, borderColor: '#f3f4f6',
  },
  stateName: { fontSize: 14, color: '#374151' },
  stateVotes: { fontSize: 14, fontWeight: '600', color: '#166534' },
  resultRow: {
    flexDirection: 'row', justifyContent: 'space-between',
    backgroundColor: '#fff', padding: 12, borderRadius: 6, marginBottom: 4,
    borderWidth: 1, borderColor: '#f3f4f6',
  },
  resultPU: { fontSize: 13, color: '#374151' },
  resultVotes: { fontSize: 13, fontWeight: '600', color: '#1f2937' },
});
