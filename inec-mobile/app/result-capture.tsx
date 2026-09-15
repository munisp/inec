/**
 * EC8A result capture (R5-106).
 *
 * Offline-first: a captured result is ALWAYS written to the durable local
 * outbox (pending_results, with a client-generated idempotency key) and the
 * drain is then attempted immediately. At a no-signal PU the result stays
 * queued and syncs later via /ingestion/offline-sync (per-item outcome
 * protocol) or /results/submit fallback — see src/lib/offline.ts.
 */
import { useState, useCallback } from 'react';
import {
  View, Text, TextInput, StyleSheet, ScrollView, TouchableOpacity,
  Alert, Platform,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect } from 'expo-router';
import { useResolvedElection } from '../src/lib/election';
import { useI18n } from '../src/lib/i18n';
import { getCurrentLocation } from '../src/lib/location';
import {
  queueResult, getPendingResults, syncPendingResults, type PendingResult,
} from '../src/lib/offline';

interface PartyScoreRow {
  key: string;
  party_code: string;
  votes: string;
}

let rowSeq = 0;
const newRow = (): PartyScoreRow => ({ key: `row-${++rowSeq}`, party_code: '', votes: '' });

export default function ResultCaptureScreen() {
  const { electionId, election, loading: electionLoading, error: electionError } = useResolvedElection();
  const { t } = useI18n();
  const [puCode, setPuCode] = useState('');
  const [scores, setScores] = useState<PartyScoreRow[]>([newRow()]);
  const [accredited, setAccredited] = useState('');
  const [rejected, setRejected] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [pending, setPending] = useState<PendingResult[]>([]);

  const loadPending = useCallback(async () => {
    setPending(await getPendingResults().catch(() => []));
  }, []);

  useFocusEffect(useCallback(() => { loadPending(); }, [loadPending]));

  const updateRow = (key: string, field: 'party_code' | 'votes', value: string) => {
    setScores((rows) => rows.map((r) => (r.key === key ? { ...r, [field]: value } : r)));
  };

  const removeRow = (key: string) => {
    setScores((rows) => (rows.length > 1 ? rows.filter((r) => r.key !== key) : rows));
  };

  const runSync = useCallback(async () => {
    setSyncing(true);
    try {
      const summary = await syncPendingResults();
      await loadPending();
      return summary;
    } finally {
      setSyncing(false);
    }
  }, [loadPending]);

  const submit = async () => {
    if (!puCode.trim()) {
      Alert.alert('Required', 'Enter the Polling Unit code');
      return;
    }
    const parsedScores = scores
      .map((r) => ({ party_code: r.party_code.trim().toUpperCase(), votes: parseInt(r.votes, 10) }))
      .filter((r) => r.party_code.length > 0);
    if (!parsedScores.length || parsedScores.some((r) => !Number.isFinite(r.votes) || r.votes < 0)) {
      Alert.alert('Invalid scores', 'Enter a party code and a non-negative vote count for at least one party');
      return;
    }
    if (!electionId) {
      Alert.alert(
        'Election Unavailable',
        electionError
          ? `Could not resolve the active election (${electionError}). Check your connection and retry.`
          : 'Still resolving the active election — please wait a moment and retry.'
      );
      return;
    }

    setSubmitting(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      // Backend geofencing (R5-024) requires the device location.
      const location = await getCurrentLocation().catch(() => null);

      // Durable first: queue with a client idempotency key, then drain.
      await queueResult({
        election_id: electionId,
        polling_unit_code: puCode.trim(),
        party_scores: parsedScores,
        accredited_voters: parseInt(accredited, 10) || 0,
        rejected_votes: parseInt(rejected, 10) || 0,
        latitude: location?.latitude ?? null,
        longitude: location?.longitude ?? null,
      });

      const summary = await runSync();
      if (summary.synced > 0 || summary.duplicates > 0) {
        Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
        Alert.alert(t('capture.submitted'), t('capture.submittedDesc'));
        setPuCode('');
        setScores([newRow()]);
        setAccredited('');
        setRejected('');
      } else {
        Haptics.notificationAsync(Haptics.NotificationFeedbackType.Warning);
        Alert.alert(t('capture.queued'), t('capture.queuedDesc'));
        setPuCode('');
        setScores([newRow()]);
        setAccredited('');
        setRejected('');
      }
    } catch (e) {
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      Alert.alert('Capture failed', String(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <ScrollView
      style={styles.container}
      contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 40 : 24 }}
      keyboardShouldPersistTaps="handled"
    >
      <View style={styles.card}>
        <Text style={styles.title}>{t('capture.title')}</Text>
        <Text style={styles.subtitle}>
          {electionLoading
            ? t('reports.resolvingElection')
            : election
              ? `${election.name} (ID ${election.id})`
              : 'No active election resolved'}
        </Text>

        <View style={styles.inputGroup}>
          <Text style={styles.label}>{t('reports.puCode')}</Text>
          <TextInput
            style={styles.input}
            placeholder="e.g. PU-23-014-001"
            placeholderTextColor="#9ca3af"
            value={puCode}
            onChangeText={setPuCode}
            autoCapitalize="characters"
            accessibilityLabel="Polling unit code"
          />
        </View>

        <Text style={styles.label}>{t('capture.partyScores')}</Text>
        {scores.map((row) => (
          <View key={row.key} style={styles.scoreRow}>
            <TextInput
              style={[styles.input, styles.partyInput]}
              placeholder="Party (e.g. APC)"
              placeholderTextColor="#9ca3af"
              value={row.party_code}
              onChangeText={(v) => updateRow(row.key, 'party_code', v)}
              autoCapitalize="characters"
              accessibilityLabel={`Party code row`}
            />
            <TextInput
              style={[styles.input, styles.votesInput]}
              placeholder="Votes"
              placeholderTextColor="#9ca3af"
              value={row.votes}
              onChangeText={(v) => updateRow(row.key, 'votes', v.replace(/[^0-9]/g, ''))}
              keyboardType="number-pad"
              accessibilityLabel="Votes for party"
            />
            <TouchableOpacity
              onPress={() => removeRow(row.key)}
              accessibilityLabel="Remove party row"
              accessibilityRole="button"
            >
              <Ionicons name="close-circle" size={24} color="#dc2626" />
            </TouchableOpacity>
          </View>
        ))}
        <TouchableOpacity
          style={styles.addRow}
          onPress={() => setScores((rows) => [...rows, newRow()])}
          accessibilityLabel="Add party score row"
          accessibilityRole="button"
        >
          <Ionicons name="add-circle-outline" size={18} color="#166534" />
          <Text style={styles.addRowText}>{t('capture.addParty')}</Text>
        </TouchableOpacity>

        <View style={styles.rowInputs}>
          <View style={[styles.inputGroup, { flex: 1 }]}>
            <Text style={styles.label}>{t('capture.accredited')}</Text>
            <TextInput
              style={styles.input}
              placeholder="0"
              placeholderTextColor="#9ca3af"
              value={accredited}
              onChangeText={(v) => setAccredited(v.replace(/[^0-9]/g, ''))}
              keyboardType="number-pad"
              accessibilityLabel="Accredited voters"
            />
          </View>
          <View style={[styles.inputGroup, { flex: 1 }]}>
            <Text style={styles.label}>{t('capture.rejected')}</Text>
            <TextInput
              style={styles.input}
              placeholder="0"
              placeholderTextColor="#9ca3af"
              value={rejected}
              onChangeText={(v) => setRejected(v.replace(/[^0-9]/g, ''))}
              keyboardType="number-pad"
              accessibilityLabel="Rejected votes"
            />
          </View>
        </View>

        <TouchableOpacity
          style={[styles.submitButton, (submitting || electionLoading) && styles.submitDisabled]}
          onPress={submit}
          disabled={submitting || electionLoading}
          activeOpacity={0.8}
          accessibilityLabel="Capture result"
          accessibilityRole="button"
          accessibilityState={{ disabled: submitting || electionLoading, busy: submitting }}
        >
          <Ionicons name={submitting ? 'hourglass-outline' : 'checkmark-circle'} size={18} color="#fff" />
          <Text style={styles.submitText}>{submitting ? t('capture.saving') : t('capture.submit')}</Text>
        </TouchableOpacity>
      </View>

      {pending.length > 0 && (
        <View style={styles.section}>
          <View style={styles.pendingHeader}>
            <View style={styles.pendingBadge}>
              <Ionicons name="cloud-upload-outline" size={14} color="#92400e" />
              <Text style={styles.pendingBadgeText}>{pending.length} {t('capture.pendingSync')}</Text>
            </View>
            <TouchableOpacity
              onPress={runSync}
              disabled={syncing}
              accessibilityLabel="Sync pending results now"
              accessibilityRole="button"
            >
              <Text style={styles.syncNow}>{syncing ? t('capture.syncing') : t('capture.syncNow')}</Text>
            </TouchableOpacity>
          </View>
          {pending.map((r) => (
            <View key={r.id} style={styles.pendingCard}>
              <Ionicons name="time-outline" size={18} color="#d97706" />
              <View style={{ flex: 1 }}>
                <Text style={styles.pendingPU}>{r.polling_unit_code} — election {r.election_id}</Text>
                <Text style={styles.pendingDesc} numberOfLines={1}>{r.party_scores}</Text>
                {r.last_error ? (
                  <Text style={styles.pendingError} numberOfLines={1}>
                    {r.attempts} attempt{r.attempts === 1 ? '' : 's'} — {r.last_error}
                  </Text>
                ) : null}
              </View>
            </View>
          ))}
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  card: {
    backgroundColor: '#fff', margin: 12, padding: 16, borderRadius: 16,
    shadowColor: '#000', shadowOffset: { width: 0, height: 2 }, shadowOpacity: 0.06,
    shadowRadius: 8, elevation: 2,
  },
  title: { fontSize: 18, fontWeight: '700', color: '#111827' },
  subtitle: { fontSize: 12, color: '#6b7280', marginTop: 2, marginBottom: 14 },
  inputGroup: { marginBottom: 14 },
  label: { fontSize: 13, fontWeight: '600', color: '#374151', marginBottom: 6 },
  input: {
    borderWidth: 1, borderColor: '#e5e7eb', borderRadius: 12,
    backgroundColor: '#f9fafb', padding: 12, fontSize: 15, color: '#111827',
  },
  scoreRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 8 },
  partyInput: { flex: 1 },
  votesInput: { width: 90 },
  addRow: { flexDirection: 'row', alignItems: 'center', gap: 6, marginBottom: 14 },
  addRowText: { color: '#166534', fontWeight: '600', fontSize: 13 },
  rowInputs: { flexDirection: 'row', gap: 10 },
  submitButton: {
    backgroundColor: '#166534', flexDirection: 'row', alignItems: 'center', justifyContent: 'center',
    gap: 8, paddingVertical: 14, borderRadius: 12, marginTop: 4,
  },
  submitDisabled: { opacity: 0.6 },
  submitText: { color: '#fff', fontSize: 16, fontWeight: '700' },
  section: { paddingHorizontal: 12, marginBottom: 12 },
  pendingHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 },
  pendingBadge: {
    flexDirection: 'row', alignItems: 'center', gap: 6, backgroundColor: '#fef3c7',
    paddingHorizontal: 12, paddingVertical: 6, borderRadius: 12,
  },
  pendingBadgeText: { fontSize: 13, fontWeight: '600', color: '#92400e' },
  syncNow: { color: '#166534', fontWeight: '700', fontSize: 13 },
  pendingCard: {
    flexDirection: 'row', alignItems: 'center', gap: 12, backgroundColor: '#fff',
    padding: 14, borderRadius: 12, marginBottom: 6, borderLeftWidth: 3, borderLeftColor: '#fbbf24',
  },
  pendingPU: { fontSize: 14, fontWeight: '600', color: '#111827' },
  pendingDesc: { fontSize: 12, color: '#6b7280', marginTop: 2 },
  pendingError: { fontSize: 11, color: '#b45309', marginTop: 2 },
});
