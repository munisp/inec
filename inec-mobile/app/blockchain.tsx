import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';

// Response shape of GET /integrity/fabric/health (handleFabricAnchorHealth,
// fabric_anchor.go). The backend returns 503 with this same JSON body when
// anchoring is unavailable, so the error path also parses it.
interface FabricHealth {
  enabled: boolean;
  required: boolean;
  status: 'disabled' | 'healthy' | 'degraded' | 'unavailable' | string;
  pending?: number;
  failed?: number;
  unavailable?: number;
  channel?: string;
  chaincode?: string;
  reason?: string;
}

interface VerifyResult {
  valid: boolean;
  block_number: number;
  tx_id: string;
  timestamp: string;
}

export default function BlockchainScreen() {
  const [fabricHealth, setFabricHealth] = useState<FabricHealth | null>(null);
  const [verifyResult, setVerifyResult] = useState<VerifyResult | null>(null);
  const [loading, setLoading] = useState(false);

  const loadStatus = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      // Real route: GET /integrity/fabric/health (main.go registers it; the
      // previous '/blockchain/fabric/status' path does not exist — 404).
      const res = await apiCall<FabricHealth>('/integrity/fabric/health');
      setFabricHealth(res);
    } catch (e: unknown) {
      // The backend answers 503 with the health JSON body when anchoring is
      // unavailable — surface that honest status instead of a bare error.
      const msg = e instanceof Error ? e.message : '';
      const body = msg.match(/^\d+:\s*(\{[\s\S]*\})$/)?.[1];
      let parsed: FabricHealth | null = null;
      if (body) { try { parsed = JSON.parse(body) as FabricHealth; } catch { parsed = null; } }
      if (parsed && typeof parsed.status === 'string') {
        setFabricHealth(parsed);
      } else {
        setFabricHealth(null);
        Alert.alert('Error', msg || 'Failed to load Fabric anchoring status');
      }
    }
    setLoading(false);
  };

  const verifyChain = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      // Backend registers this route as GET (main.go). When no external
      // Fabric/IPFS backend is configured it answers 503 with an explicit
      // "not configured" error — that message is shown verbatim; no success
      // is ever simulated.
      const res = await apiCall<VerifyResult>('/blockchain/fabric/verify-chain');
      setVerifyResult(res);
    } catch (e: unknown) {
      setVerifyResult(null);
      Alert.alert('Chain verification unavailable', e instanceof Error ? e.message : 'Chain verification failed');
    }
    setLoading(false);
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="link-outline" size={24} color="#166534" />
          <Text style={styles.cardTitle}>Hyperledger Fabric</Text>
        </View>
        <Text style={styles.muted}>Immutable blockchain ledger for election result integrity.</Text>
        <TouchableOpacity style={styles.button} onPress={loadStatus} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>{loading ? 'Loading...' : 'Check Fabric Status'}</Text>
        </TouchableOpacity>
        {fabricHealth && (
          <View>
            <View style={styles.statsGrid}>
              <View style={styles.statCard}>
                <Text style={[styles.statNumber, { color: fabricHealth.status === 'healthy' ? '#166534' : fabricHealth.status === 'degraded' ? '#b45309' : '#dc2626', textTransform: 'capitalize' }]}>
                  {fabricHealth.status}
                </Text>
                <Text style={styles.statLabel}>Anchoring Status</Text>
              </View>
              <View style={styles.statCard}>
                <Text style={styles.statNumber}>{fabricHealth.pending ?? 0}</Text>
                <Text style={styles.statLabel}>Pending Anchors</Text>
              </View>
              <View style={styles.statCard}>
                <Text style={[styles.statNumber, { color: (fabricHealth.failed ?? 0) > 0 ? '#dc2626' : '#111827' }]}>{fabricHealth.failed ?? 0}</Text>
                <Text style={styles.statLabel}>Failed Anchors</Text>
              </View>
            </View>
            {fabricHealth.channel ? (
              <Text style={styles.muted}>Channel: {fabricHealth.channel} | Chaincode: {fabricHealth.chaincode ?? '—'}</Text>
            ) : null}
            {fabricHealth.reason ? (
              <Text style={[styles.muted, { color: '#dc2626' }]}>Reason: {fabricHealth.reason}</Text>
            ) : null}
            {!fabricHealth.enabled ? (
              <Text style={styles.muted}>Fabric anchoring is not enabled on this deployment{fabricHealth.required ? ' (required by policy)' : ''}.</Text>
            ) : null}
          </View>
        )}
      </View>

      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="shield-checkmark-outline" size={24} color="#7c3aed" />
          <Text style={styles.cardTitle}>Chain Verification</Text>
        </View>
        <Text style={styles.muted}>Verify integrity of the entire result chain from genesis block.</Text>
        <TouchableOpacity style={[styles.button, { backgroundColor: '#7c3aed' }]} onPress={verifyChain} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>Verify Chain Integrity</Text>
        </TouchableOpacity>
        {verifyResult && (
          <View style={[styles.resultBanner, { backgroundColor: verifyResult.valid ? '#dcfce7' : '#fef2f2' }]}>
            <Ionicons name={verifyResult.valid ? 'checkmark-circle' : 'close-circle'} size={28} color={verifyResult.valid ? '#166534' : '#dc2626'} />
            <View style={{ flex: 1, marginLeft: 12 }}>
              <Text style={{ fontSize: 16, fontWeight: '700', color: verifyResult.valid ? '#166534' : '#dc2626' }}>
                {verifyResult.valid ? 'Chain Valid' : 'Integrity Violation Detected'}
              </Text>
              <Text style={styles.muted}>Block: {verifyResult.block_number} | TX: {verifyResult.tx_id?.slice(0, 16)}...</Text>
            </View>
          </View>
        )}
      </View>

      <View style={styles.card}>
        <Text style={styles.cardTitle}>Ledger Features</Text>
        {[
          { icon: 'document-text-outline' as const, title: 'Result Submission', desc: 'Chaincode validates & commits results' },
          { icon: 'git-compare-outline' as const, title: 'TigerBeetle Dual-Ledger', desc: 'Financial-grade double-entry audit trail' },
          { icon: 'globe-outline' as const, title: 'IPFS Document Storage', desc: 'Tamper-proof document archival with CID' },
          { icon: 'key-outline' as const, title: 'eNaira/CBDC Integration', desc: 'Stablecoin-based election disbursements' },
        ].map((f) => (
          <View key={f.title} style={styles.capRow}>
            <Ionicons name={f.icon} size={20} color="#166534" />
            <View style={{ flex: 1, marginLeft: 10 }}>
              <Text style={{ fontSize: 14, fontWeight: '600', color: '#111827' }}>{f.title}</Text>
              <Text style={styles.muted}>{f.desc}</Text>
            </View>
          </View>
        ))}
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  card: { backgroundColor: '#fff', borderRadius: 16, padding: 16, marginBottom: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  cardHeader: { flexDirection: 'row', alignItems: 'center', gap: 10, marginBottom: 12 },
  cardTitle: { fontSize: 16, fontWeight: '700', color: '#111827' },
  muted: { fontSize: 13, color: '#9ca3af', marginBottom: 8 },
  button: { backgroundColor: '#166534', borderRadius: 12, padding: 14, alignItems: 'center', marginTop: 8 },
  buttonText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  statsGrid: { flexDirection: 'row', gap: 8, marginTop: 12 },
  statCard: { flex: 1, backgroundColor: '#f9fafb', borderRadius: 10, padding: 12, alignItems: 'center' },
  statNumber: { fontSize: 20, fontWeight: '700', color: '#111827' },
  statLabel: { fontSize: 11, color: '#6b7280', marginTop: 2 },
  resultBanner: { flexDirection: 'row', alignItems: 'center', borderRadius: 12, padding: 14, marginTop: 12 },
  capRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
});
