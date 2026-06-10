// GOTV Canvasser Field Workflow — door-to-door tracking with offline-first storage.
// Uses expo-location for GPS, expo-sqlite for offline queue, background sync via lib/sync.ts.

import { useState, useEffect, useCallback, useRef } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity,
  TextInput, Alert, Platform, Vibration,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Location from 'expo-location';
import * as Haptics from 'expo-haptics';
import {
  saveDoorKnock, savePledge, saveLocation,
  getCachedContacts, getPendingCounts,
  type CachedContact,
} from '../lib/storage';
import { syncManager, type SyncState } from '../lib/sync';

const OUTCOMES = [
  { key: 'home', label: 'Home — Spoke', icon: 'checkmark-circle', color: '#10b981' },
  { key: 'not_home', label: 'Not Home', icon: 'close-circle', color: '#9ca3af' },
  { key: 'refused', label: 'Refused', icon: 'ban', color: '#ef4444' },
  { key: 'pledged', label: 'Pledged to Vote', icon: 'thumbs-up', color: '#3b82f6' },
  { key: 'already_voted', label: 'Already Voted', icon: 'checkmark-done', color: '#8b5cf6' },
] as const;

type Outcome = typeof OUTCOMES[number]['key'];

export default function GOTVCanvasserScreen() {
  const [shiftActive, setShiftActive] = useState(false);
  const [volunteerId] = useState('vol-001'); // Would come from auth
  const [currentLocation, setCurrentLocation] = useState<Location.LocationObject | null>(null);
  const [walklist, setWalklist] = useState<CachedContact[]>([]);
  const [selectedContact, setSelectedContact] = useState<CachedContact | null>(null);
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const [notes, setNotes] = useState('');
  const [knockCount, setKnockCount] = useState(0);
  const [syncState, setSyncState] = useState<SyncState>('idle');
  const [pendingCount, setPendingCount] = useState(0);
  const locationWatcher = useRef<Location.LocationSubscription | null>(null);

  // ─── Location Tracking ──────────────────────────────────────────────────

  useEffect(() => {
    (async () => {
      const { status } = await Location.requestForegroundPermissionsAsync();
      if (status !== 'granted') {
        Alert.alert('Permission Required', 'Location access is needed for canvassing.');
        return;
      }
      const loc = await Location.getCurrentPositionAsync({
        accuracy: Location.Accuracy.High,
      });
      setCurrentLocation(loc);
    })();
  }, []);

  const startLocationTracking = useCallback(async () => {
    if (locationWatcher.current) return;

    locationWatcher.current = await Location.watchPositionAsync(
      {
        accuracy: Location.Accuracy.High,
        distanceInterval: 10, // Update every 10 meters
        timeInterval: 5000,   // Or every 5 seconds
      },
      async (loc) => {
        setCurrentLocation(loc);
        // Save to offline queue for background sync
        await saveLocation({
          volunteer_id: volunteerId,
          latitude: loc.coords.latitude,
          longitude: loc.coords.longitude,
          battery: 100, // Would read from Device API
          speed_kmh: (loc.coords.speed ?? 0) * 3.6, // m/s to km/h
          recorded_at: new Date().toISOString(),
        });
      },
    );
  }, [volunteerId]);

  const stopLocationTracking = useCallback(() => {
    if (locationWatcher.current) {
      locationWatcher.current.remove();
      locationWatcher.current = null;
    }
  }, []);

  // ─── Sync ───────────────────────────────────────────────────────────────

  useEffect(() => {
    const unsub = syncManager.subscribe((state, pending) => {
      setSyncState(state);
      setPendingCount(pending);
    });
    syncManager.start();
    return () => { unsub(); syncManager.stop(); };
  }, []);

  // ─── Load Walklist ──────────────────────────────────────────────────────

  const loadWalklist = useCallback(async () => {
    const contacts = await getCachedContacts();
    setWalklist(contacts);
  }, []);

  useEffect(() => { loadWalklist(); }, [loadWalklist]);

  // ─── Shift Controls ─────────────────────────────────────────────────────

  const startShift = async () => {
    await startLocationTracking();
    setShiftActive(true);
    setKnockCount(0);
    if (Platform.OS !== 'web') Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
  };

  const endShift = async () => {
    stopLocationTracking();
    setShiftActive(false);
    Alert.alert('Shift Ended', `Doors knocked: ${knockCount}`);
  };

  // ─── Record Door Knock ──────────────────────────────────────────────────

  const recordKnock = async () => {
    if (!outcome) {
      Alert.alert('Select Outcome', 'Please select what happened at the door.');
      return;
    }
    if (!currentLocation) {
      Alert.alert('No Location', 'Waiting for GPS fix...');
      return;
    }

    const speed = (currentLocation.coords.speed ?? 0) * 3.6;

    await saveDoorKnock({
      volunteer_id: volunteerId,
      contact_id: selectedContact?.contact_id ?? null,
      latitude: currentLocation.coords.latitude,
      longitude: currentLocation.coords.longitude,
      outcome,
      notes,
      speed_kmh: speed,
      recorded_at: new Date().toISOString(),
    });

    // If pledged, also save a pledge record
    if (outcome === 'pledged' && selectedContact) {
      await savePledge({
        contact_id: selectedContact.contact_id,
        pledge_type: 'will_vote',
        notes,
        recorded_at: new Date().toISOString(),
      });
    }

    setKnockCount(prev => prev + 1);
    setOutcome(null);
    setNotes('');
    setSelectedContact(null);
    if (Platform.OS !== 'web') Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);

    // Refresh pending count
    const counts = await getPendingCounts();
    setPendingCount(counts.knocks + counts.pledges + counts.locations);
  };

  // ─── Sync Status Badge ──────────────────────────────────────────────────

  const syncColor = syncState === 'idle' ? '#10b981' : syncState === 'syncing' ? '#3b82f6' :
    syncState === 'offline' ? '#f59e0b' : '#ef4444';
  const syncLabel = syncState === 'idle' ? 'Synced' : syncState === 'syncing' ? 'Syncing...' :
    syncState === 'offline' ? 'Offline' : 'Error';

  // ─── Render ─────────────────────────────────────────────────────────────

  return (
    <ScrollView style={styles.container}>
      {/* Header */}
      <View style={styles.header}>
        <Text style={styles.title}>Canvasser</Text>
        <View style={styles.syncBadge}>
          <View style={[styles.syncDot, { backgroundColor: syncColor }]} />
          <Text style={styles.syncText}>{syncLabel}</Text>
          {pendingCount > 0 && (
            <Text style={styles.pendingText}>({pendingCount} pending)</Text>
          )}
        </View>
      </View>

      {/* Shift Status */}
      <View style={styles.card}>
        <View style={styles.row}>
          <Ionicons name={shiftActive ? 'walk' : 'pause-circle'} size={24}
            color={shiftActive ? '#10b981' : '#9ca3af'} />
          <Text style={styles.cardTitle}>
            {shiftActive ? `Shift Active — ${knockCount} doors` : 'Shift Not Started'}
          </Text>
        </View>

        {currentLocation && (
          <Text style={styles.coordText}>
            GPS: {currentLocation.coords.latitude.toFixed(6)}, {currentLocation.coords.longitude.toFixed(6)}
            {currentLocation.coords.speed != null && ` | ${(currentLocation.coords.speed * 3.6).toFixed(1)} km/h`}
          </Text>
        )}

        <TouchableOpacity
          style={[styles.button, { backgroundColor: shiftActive ? '#ef4444' : '#10b981' }]}
          onPress={shiftActive ? endShift : startShift}
        >
          <Ionicons name={shiftActive ? 'stop' : 'play'} size={20} color="#fff" />
          <Text style={styles.buttonText}>{shiftActive ? 'End Shift' : 'Start Shift'}</Text>
        </TouchableOpacity>
      </View>

      {/* Door Knock Form */}
      {shiftActive && (
        <>
          {/* Contact Selection */}
          <View style={styles.card}>
            <Text style={styles.sectionTitle}>Contact (optional)</Text>
            {selectedContact ? (
              <View style={styles.contactCard}>
                <Text style={styles.contactName}>{selectedContact.full_name_encrypted}</Text>
                <Text style={styles.contactDetail}>
                  {selectedContact.state_code} / {selectedContact.lga_code} — {selectedContact.voter_status}
                </Text>
                <TouchableOpacity onPress={() => setSelectedContact(null)}>
                  <Text style={styles.clearText}>Clear</Text>
                </TouchableOpacity>
              </View>
            ) : (
              <ScrollView horizontal showsHorizontalScrollIndicator={false}>
                {walklist.slice(0, 10).map(c => (
                  <TouchableOpacity
                    key={c.contact_id}
                    style={styles.contactChip}
                    onPress={() => setSelectedContact(c)}
                  >
                    <Text style={styles.chipText} numberOfLines={1}>
                      {c.full_name_encrypted || c.phone_masked}
                    </Text>
                  </TouchableOpacity>
                ))}
              </ScrollView>
            )}
          </View>

          {/* Outcome Selection */}
          <View style={styles.card}>
            <Text style={styles.sectionTitle}>Door Outcome</Text>
            <View style={styles.outcomeGrid}>
              {OUTCOMES.map(o => (
                <TouchableOpacity
                  key={o.key}
                  style={[
                    styles.outcomeButton,
                    outcome === o.key && { backgroundColor: o.color + '20', borderColor: o.color },
                  ]}
                  onPress={() => setOutcome(o.key)}
                >
                  <Ionicons name={o.icon as keyof typeof Ionicons.glyphMap} size={24} color={o.color} />
                  <Text style={[styles.outcomeLabel, outcome === o.key && { color: o.color }]}>
                    {o.label}
                  </Text>
                </TouchableOpacity>
              ))}
            </View>
          </View>

          {/* Notes */}
          <View style={styles.card}>
            <Text style={styles.sectionTitle}>Notes</Text>
            <TextInput
              style={styles.notesInput}
              placeholder="Any observations..."
              value={notes}
              onChangeText={setNotes}
              multiline
              numberOfLines={3}
            />
          </View>

          {/* Submit */}
          <TouchableOpacity
            style={[styles.button, styles.submitButton, !outcome && styles.buttonDisabled]}
            onPress={recordKnock}
            disabled={!outcome}
          >
            <Ionicons name="checkmark-circle" size={24} color="#fff" />
            <Text style={styles.buttonText}>Record Door Knock</Text>
          </TouchableOpacity>
        </>
      )}

      {/* Stats */}
      <View style={[styles.card, { marginBottom: 40 }]}>
        <Text style={styles.sectionTitle}>Session Stats</Text>
        <View style={styles.statsRow}>
          <View style={styles.statItem}>
            <Text style={styles.statValue}>{knockCount}</Text>
            <Text style={styles.statLabel}>Doors</Text>
          </View>
          <View style={styles.statItem}>
            <Text style={styles.statValue}>{pendingCount}</Text>
            <Text style={styles.statLabel}>Pending Sync</Text>
          </View>
          <View style={styles.statItem}>
            <Text style={[styles.statValue, { color: syncColor }]}>{syncLabel}</Text>
            <Text style={styles.statLabel}>Connection</Text>
          </View>
        </View>
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 },
  title: { fontSize: 24, fontWeight: '700', color: '#111827' },
  syncBadge: { flexDirection: 'row', alignItems: 'center', gap: 6, backgroundColor: '#fff',
    paddingHorizontal: 12, paddingVertical: 6, borderRadius: 16, borderWidth: 1, borderColor: '#e5e7eb' },
  syncDot: { width: 8, height: 8, borderRadius: 4 },
  syncText: { fontSize: 12, fontWeight: '600', color: '#374151' },
  pendingText: { fontSize: 11, color: '#6b7280' },
  card: { backgroundColor: '#fff', borderRadius: 12, padding: 16, marginBottom: 12,
    borderWidth: 1, borderColor: '#e5e7eb' },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 8 },
  cardTitle: { fontSize: 16, fontWeight: '600', color: '#111827' },
  coordText: { fontSize: 11, color: '#6b7280', fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace', marginBottom: 12 },
  button: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8,
    paddingVertical: 14, borderRadius: 10 },
  buttonText: { fontSize: 16, fontWeight: '600', color: '#fff' },
  buttonDisabled: { opacity: 0.4 },
  submitButton: { backgroundColor: '#3b82f6', marginBottom: 12 },
  sectionTitle: { fontSize: 14, fontWeight: '600', color: '#374151', marginBottom: 8 },
  contactCard: { backgroundColor: '#f0fdf4', padding: 12, borderRadius: 8, borderWidth: 1, borderColor: '#86efac' },
  contactName: { fontSize: 15, fontWeight: '600', color: '#111827' },
  contactDetail: { fontSize: 12, color: '#6b7280', marginTop: 2 },
  clearText: { fontSize: 12, color: '#ef4444', marginTop: 4, fontWeight: '500' },
  contactChip: { backgroundColor: '#eff6ff', paddingHorizontal: 14, paddingVertical: 8,
    borderRadius: 20, marginRight: 8, borderWidth: 1, borderColor: '#bfdbfe' },
  chipText: { fontSize: 13, color: '#1e40af', maxWidth: 120 },
  outcomeGrid: { gap: 8 },
  outcomeButton: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 12,
    borderRadius: 10, borderWidth: 1.5, borderColor: '#e5e7eb', backgroundColor: '#fff' },
  outcomeLabel: { fontSize: 14, fontWeight: '500', color: '#374151' },
  notesInput: { borderWidth: 1, borderColor: '#d1d5db', borderRadius: 8, padding: 12,
    fontSize: 14, textAlignVertical: 'top', minHeight: 80 },
  statsRow: { flexDirection: 'row', justifyContent: 'space-around' },
  statItem: { alignItems: 'center' },
  statValue: { fontSize: 20, fontWeight: '700', color: '#111827' },
  statLabel: { fontSize: 11, color: '#6b7280', marginTop: 2 },
});
