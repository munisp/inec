import { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Clipboard from 'expo-clipboard';
import * as Haptics from 'expo-haptics';
import { router, useLocalSearchParams } from 'expo-router';
import { integrityApi, IntegrityJourney } from '../src/lib/api';

type RecordValue = Record<string, unknown>;

function value(record: RecordValue | undefined, key: string) {
  const item = record?.[key];
  return item === undefined || item === null || item === '' ? '—' : String(item);
}

function title(valueToFormat: unknown) {
  return String(valueToFormat || 'not available').replaceAll('_', ' ');
}

function compactHash(raw: unknown) {
  const text = typeof raw === 'string' ? raw : '';
  return text ? `${text.slice(0, 12)}…${text.slice(-9)}` : '—';
}

export default function EvidenceScreen() {
  const params = useLocalSearchParams<{ result_id?: string; election_id?: string }>();
  const [journey, setJourney] = useState<IntegrityJourney | null>(null);
  const [materials, setMaterials] = useState<RecordValue[]>([]);
  const [loading, setLoading] = useState(true);
  const [verifying, setVerifying] = useState(false);
  const [error, setError] = useState('');
  const resultId = Number(params.result_id || 0);
  const electionId = Number(params.election_id || 0);

  const load = useCallback(async () => {
    if (!Number.isInteger(resultId) || resultId <= 0) {
      setError('A valid result ID is required to inspect evidence.');
      setLoading(false);
      return;
    }
    setLoading(true);
    setError('');
    try {
      const response = await integrityApi.journey(resultId);
      setJourney(response);
    } catch (caught: unknown) {
      setJourney(null);
      setError((caught as Error).message || 'Evidence journey is unavailable.');
    } finally {
      setLoading(false);
    }
  }, [resultId]);

  useEffect(() => { void load(); }, [load]);

  const verify = async () => {
    if (!resultId) return;
    setVerifying(true);
    setError('');
    try {
      const verification = await integrityApi.verify(resultId);
      setJourney((current) => current ? { ...current, verification } : current);
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
    } catch (caught: unknown) {
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      setError((caught as Error).message || 'Evidence verification failed.');
    } finally {
      setVerifying(false);
    }
  };

  const loadMaterials = async () => {
    const resolvedElectionId = electionId || Number(journey?.result?.election_id || 0);
    if (!Number.isInteger(resolvedElectionId) || resolvedElectionId <= 0) {
      setError('An election ID is required to retrieve approved material manifests.');
      return;
    }
    try {
      const response = await integrityApi.materialManifests(resolvedElectionId);
      setMaterials(Array.isArray(response.material_manifests) ? response.material_manifests : []);
    } catch (caught: unknown) {
      setError((caught as Error).message || 'Material manifests are unavailable.');
    }
  };

  const copyHash = async (hash: unknown, label: string) => {
    if (typeof hash !== 'string' || !hash) return;
    await Clipboard.setStringAsync(hash);
    Haptics.selectionAsync();
    Alert.alert('Copied', `${label} copied to the clipboard.`);
  };

  if (loading) {
    return <View style={styles.loading}><ActivityIndicator color="#166534" /><Text style={styles.loadingText}>Loading evidence journey…</Text></View>;
  }

  const verification = journey?.verification;
  const events = journey?.events || [];
  const cases = journey?.reconciliation_cases || [];
  const artifacts = journey?.artifacts || [];
  const fabricAnchors = journey?.fabric_anchors || [];
  const committedFabricAnchors = fabricAnchors.filter((anchor) => anchor.status === 'committed');
  const verificationGood = Boolean(verification?.chain_valid) && verification?.signature_valid !== false;

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.content}>
      <View style={styles.header}>
        <TouchableOpacity accessibilityRole="button" accessibilityLabel="Go back" onPress={() => router.back()} style={styles.backButton}>
          <Ionicons name="arrow-back" size={20} color="#166534" />
        </TouchableOpacity>
        <View style={{ flex: 1 }}>
          <Text style={styles.eyebrow}>ELECTION EVIDENCE</Text>
          <Text style={styles.title}>Result lifecycle</Text>
          <Text style={styles.subtitle}>Hashes and signatures make changes detectable. They do not replace authorised collation or declaration.</Text>
        </View>
      </View>

      {error ? <View style={styles.error}><Text style={styles.errorText}>{error}</Text><TouchableOpacity onPress={load}><Text style={styles.retry}>Retry</Text></TouchableOpacity></View> : null}

      {journey && <>
        <View style={styles.overviewCard}>
          <View><Text style={styles.label}>POLLING UNIT</Text><Text style={styles.puCode}>{value(journey.result, 'polling_unit_code')}</Text></View>
          <View style={styles.statusBadge}><Text style={styles.statusText}>{title(journey.result?.status).toUpperCase()}</Text></View>
          <View style={styles.metricRow}><View><Text style={styles.label}>POLICY VERSION</Text><Text style={styles.metric}>{journey.policy_version_id || '—'}</Text></View><View><Text style={styles.label}>EVIDENCE EVENTS</Text><Text style={styles.metric}>{verification?.event_count ?? events.length}</Text></View></View>
        </View>

        <View style={[styles.verificationCard, verificationGood ? styles.verificationGood : styles.verificationReview]}>
          <View style={styles.verificationHeader}><Ionicons name={verificationGood ? 'shield-checkmark-outline' : 'shield-outline'} size={22} color={verificationGood ? '#166534' : '#b45309'} /><View style={{ flex: 1 }}><Text style={styles.verificationTitle}>Evidence verification</Text><Text style={styles.verificationText}>Chain: {verification?.chain_valid ? 'valid' : 'unverified'} · Signature: {verification?.signature_checked ? (verification.signature_valid ? 'valid' : 'invalid') : 'not checked'}</Text></View></View>
          {(verification?.failure_reasons || []).map((reason) => <Text key={reason} style={styles.failureReason}>• {reason}</Text>)}
          <TouchableOpacity accessibilityRole="button" style={styles.verifyButton} onPress={verify} disabled={verifying}><Text style={styles.verifyButtonText}>{verifying ? 'Verifying…' : 'Verify chain now'}</Text></TouchableOpacity>
        </View>

        <Text style={styles.sectionTitle}>Consortium ledger anchors</Text>
        <View style={[styles.fabricSummary, fabricAnchors.length > 0 && committedFabricAnchors.length === fabricAnchors.length ? styles.fabricCommitted : styles.fabricPending]}>
          <View style={styles.fabricHeader}><Ionicons name="link-outline" size={21} color={fabricAnchors.length > 0 && committedFabricAnchors.length === fabricAnchors.length ? '#0369a1' : '#b45309'} /><View style={{ flex: 1 }}><Text style={styles.fabricTitle}>Hyperledger Fabric provenance</Text><Text style={styles.fabricText}>Committed: {committedFabricAnchors.length}/{fabricAnchors.length}. Only signed hashes and receipt metadata are anchored; raw election records remain off-chain.</Text></View></View>
        </View>
        {fabricAnchors.length === 0 ? <View style={styles.emptyCard}><Text style={styles.emptyText}>No consortium receipt is recorded for this result. Local evidence verification is separate from independent Fabric confirmation.</Text></View> : fabricAnchors.map((anchor) => <View key={String(anchor.id || anchor.anchor_id)} style={styles.fabricCard}><View style={styles.fabricCardHeader}><Text style={styles.fabricStatus}>{title(anchor.status).toUpperCase()}</Text><Text style={styles.fabricMeta}>{anchor.channel || '—'} / {anchor.chaincode || '—'}</Text></View>{anchor.anchor_id ? <TouchableOpacity onPress={() => copyHash(anchor.anchor_id, 'Fabric anchor hash')}><Text style={styles.hash}>{compactHash(anchor.anchor_id)}</Text></TouchableOpacity> : null}{anchor.transaction_id ? <TouchableOpacity onPress={() => copyHash(anchor.transaction_id, 'Fabric transaction ID')}><Text style={styles.fabricTx}>Transaction: {compactHash(anchor.transaction_id)}</Text></TouchableOpacity> : <Text style={styles.fabricTx}>No committed Fabric transaction receipt yet.</Text>}{anchor.receipt_sha256 ? <TouchableOpacity onPress={() => copyHash(anchor.receipt_sha256, 'Fabric receipt hash')}><Text style={styles.fabricTx}>Receipt: {compactHash(anchor.receipt_sha256)}</Text></TouchableOpacity> : null}{anchor.diagnostic ? <Text style={styles.fabricDiagnostic}>{anchor.diagnostic}</Text> : null}</View>)}

        <Text style={styles.sectionTitle}>Immutable event sequence</Text>
        {events.length === 0 ? <View style={styles.emptyCard}><Text style={styles.emptyText}>No evidence event is linked to this result yet.</Text></View> : events.map((event) => <View key={String(event.sequence_no)} style={styles.eventCard}><View style={styles.eventHeader}><View style={styles.sequence}><Text style={styles.sequenceText}>{value(event, 'sequence_no')}</Text></View><View style={{ flex: 1 }}><Text style={styles.eventTitle}>{title(event.event_type)}</Text><Text style={styles.eventTime}>{value(event, 'created_at')}</Text></View><Text style={styles.signer}>{value(event, 'signer_status')}</Text></View><TouchableOpacity onPress={() => copyHash(event.event_hash, 'Evidence event hash')}><Text style={styles.hash}>{compactHash(event.event_hash)}</Text></TouchableOpacity></View>)}

        <Text style={styles.sectionTitle}>Reconciliation cases</Text>
        {cases.length === 0 ? <View style={styles.emptyCard}><Text style={styles.emptyText}>No reconciliation case is linked to this result.</Text></View> : cases.map((caseItem) => <View key={String(caseItem.id)} style={styles.caseCard}><View style={styles.caseHeader}><Text style={styles.caseTitle}>{title(caseItem.case_type)}</Text><Text style={styles.caseSeverity}>{title(caseItem.severity)}</Text></View><Text style={styles.caseText}>{value(caseItem, 'description')}</Text><Text style={styles.caseMeta}>State: {title(caseItem.status)} · Blocking: {String(Boolean(caseItem.blocking))}</Text></View>)}

        <Text style={styles.sectionTitle}>Source artifacts</Text>
        {artifacts.length === 0 ? <View style={styles.emptyCard}><Text style={styles.emptyText}>No source artifact is linked to this result yet.</Text></View> : artifacts.map((artifact) => <View key={String(artifact.id)} style={styles.artifactCard}><Text style={styles.artifactTitle}>{title(artifact.artifact_kind)}</Text><Text style={styles.artifactText}>{value(artifact, 'original_filename')} · {value(artifact, 'byte_size')} bytes</Text><TouchableOpacity onPress={() => copyHash(artifact.content_sha256, 'Artifact hash')}><Text style={styles.hash}>{compactHash(artifact.content_sha256)}</Text></TouchableOpacity></View>)}
      </>}

      <View style={styles.materialSection}><View><Text style={styles.sectionTitle}>Approved election materials</Text><Text style={styles.materialDescription}>Retrieve versioned, hash-addressed forms and official lists for this election.</Text></View><TouchableOpacity style={styles.materialButton} onPress={loadMaterials}><Text style={styles.materialButtonText}>Load materials</Text></TouchableOpacity>{materials.map((material) => <View key={String(material.id)} style={styles.materialCard}><Text style={styles.artifactTitle}>{title(material.material_type)} · v{value(material, 'version')}</Text><Text style={styles.artifactText}>{title(material.status)}</Text><TouchableOpacity onPress={() => copyHash(material.manifest_sha256, 'Material manifest hash')}><Text style={styles.hash}>{compactHash(material.manifest_sha256)}</Text></TouchableOpacity></View>)}</View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  content: { padding: 16, paddingBottom: 48 },
  loading: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 10, backgroundColor: '#f8fafc' },
  loadingText: { color: '#475569', fontSize: 14 },
  header: { flexDirection: 'row', gap: 12, marginBottom: 18 },
  backButton: { width: 38, height: 38, alignItems: 'center', justifyContent: 'center', borderRadius: 19, backgroundColor: '#ecfdf5' },
  eyebrow: { color: '#166534', fontSize: 10, fontWeight: '800', letterSpacing: 1.2 },
  title: { color: '#111827', fontSize: 25, fontWeight: '800', marginTop: 2 },
  subtitle: { color: '#64748b', fontSize: 13, lineHeight: 19, marginTop: 5 },
  error: { borderWidth: 1, borderColor: '#fecaca', backgroundColor: '#fef2f2', padding: 12, borderRadius: 10, marginBottom: 14 },
  errorText: { color: '#991b1b', fontSize: 13 }, retry: { color: '#166534', fontWeight: '700', marginTop: 7 },
  overviewCard: { backgroundColor: '#fff', padding: 15, borderRadius: 14, borderWidth: 1, borderColor: '#e2e8f0', gap: 12 },
  label: { color: '#64748b', fontSize: 10, fontWeight: '800', letterSpacing: .8 }, puCode: { color: '#111827', fontFamily: 'monospace', fontSize: 16, fontWeight: '800', marginTop: 3 },
  statusBadge: { alignSelf: 'flex-start', backgroundColor: '#ecfdf5', paddingHorizontal: 8, paddingVertical: 4, borderRadius: 7 }, statusText: { color: '#166534', fontSize: 10, fontWeight: '800' },
  metricRow: { flexDirection: 'row', gap: 42 }, metric: { color: '#111827', fontSize: 19, fontWeight: '800', marginTop: 3 },
  verificationCard: { marginTop: 14, borderWidth: 1, borderRadius: 14, padding: 14 }, verificationGood: { borderColor: '#bbf7d0', backgroundColor: '#f0fdf4' }, verificationReview: { borderColor: '#fde68a', backgroundColor: '#fffbeb' },
  verificationHeader: { flexDirection: 'row', gap: 10, alignItems: 'center' }, verificationTitle: { color: '#111827', fontSize: 15, fontWeight: '800' }, verificationText: { color: '#475569', fontSize: 12, marginTop: 2 }, failureReason: { color: '#92400e', fontSize: 12, marginTop: 7 },
  verifyButton: { alignSelf: 'flex-start', marginTop: 12, backgroundColor: '#166534', paddingHorizontal: 12, paddingVertical: 8, borderRadius: 8 }, verifyButtonText: { color: '#fff', fontSize: 12, fontWeight: '800' },
  sectionTitle: { color: '#111827', fontSize: 17, fontWeight: '800', marginTop: 22, marginBottom: 9 },
  emptyCard: { backgroundColor: '#fff', borderWidth: 1, borderColor: '#e2e8f0', borderRadius: 12, padding: 13 }, emptyText: { color: '#64748b', fontSize: 13 },
  eventCard: { backgroundColor: '#fff', borderWidth: 1, borderColor: '#e2e8f0', borderRadius: 12, padding: 13, marginBottom: 8 }, eventHeader: { flexDirection: 'row', alignItems: 'center', gap: 9 }, sequence: { backgroundColor: '#ecfdf5', width: 25, height: 25, borderRadius: 13, alignItems: 'center', justifyContent: 'center' }, sequenceText: { color: '#166534', fontSize: 11, fontWeight: '800' }, eventTitle: { color: '#111827', fontSize: 13, fontWeight: '800' }, eventTime: { color: '#64748b', fontSize: 10, marginTop: 2 }, signer: { color: '#475569', fontSize: 10, fontWeight: '700' },
  hash: { color: '#166534', fontFamily: 'monospace', fontSize: 11, marginTop: 9 },
  caseCard: { backgroundColor: '#fff7ed', borderLeftWidth: 3, borderLeftColor: '#f97316', borderRadius: 8, padding: 12, marginBottom: 8 }, caseHeader: { flexDirection: 'row', justifyContent: 'space-between', gap: 8 }, caseTitle: { color: '#111827', fontSize: 13, fontWeight: '800', flex: 1 }, caseSeverity: { color: '#9a3412', fontSize: 10, fontWeight: '800', textTransform: 'uppercase' }, caseText: { color: '#475569', fontSize: 12, lineHeight: 17, marginTop: 5 }, caseMeta: { color: '#78716c', fontSize: 10, marginTop: 6 },
  artifactCard: { backgroundColor: '#fff', borderWidth: 1, borderColor: '#e2e8f0', borderRadius: 12, padding: 13, marginBottom: 8 }, artifactTitle: { color: '#111827', fontSize: 13, fontWeight: '800' }, artifactText: { color: '#64748b', fontSize: 11, marginTop: 4 },
  fabricSummary: { borderWidth: 1, borderRadius: 12, padding: 13, marginBottom: 9 }, fabricCommitted: { borderColor: '#bae6fd', backgroundColor: '#f0f9ff' }, fabricPending: { borderColor: '#fde68a', backgroundColor: '#fffbeb' }, fabricHeader: { flexDirection: 'row', gap: 10, alignItems: 'flex-start' }, fabricTitle: { color: '#111827', fontSize: 14, fontWeight: '800' }, fabricText: { color: '#475569', fontSize: 11, lineHeight: 16, marginTop: 3 }, fabricCard: { backgroundColor: '#fff', borderWidth: 1, borderColor: '#bae6fd', borderRadius: 12, padding: 13, marginBottom: 8 }, fabricCardHeader: { flexDirection: 'row', justifyContent: 'space-between', gap: 8 }, fabricStatus: { color: '#075985', fontSize: 10, fontWeight: '800' }, fabricMeta: { color: '#64748b', fontSize: 10, flexShrink: 1, textAlign: 'right' }, fabricTx: { color: '#0369a1', fontFamily: 'monospace', fontSize: 10, marginTop: 6 }, fabricDiagnostic: { color: '#92400e', fontSize: 10, lineHeight: 14, marginTop: 7 },
  materialSection: { marginTop: 6, paddingTop: 4 }, materialDescription: { color: '#64748b', fontSize: 12, lineHeight: 17 }, materialButton: { alignSelf: 'flex-start', marginTop: 10, backgroundColor: '#111827', paddingHorizontal: 12, paddingVertical: 8, borderRadius: 8 }, materialButtonText: { color: '#fff', fontSize: 12, fontWeight: '800' }, materialCard: { backgroundColor: '#fff', borderWidth: 1, borderColor: '#e2e8f0', borderRadius: 12, padding: 13, marginTop: 9 },
});
