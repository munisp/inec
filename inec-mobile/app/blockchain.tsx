import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';

interface ChainStatus {
  channel: string;
  block_height: number;
  peers_connected: number;
  orderer_status: string;
  chaincode_version: string;
}

interface VerifyResult {
  valid: boolean;
  block_number: number;
  tx_id: string;
  timestamp: string;
}

export default function BlockchainScreen() {
  const [chainStatus, setChainStatus] = useState<ChainStatus | null>(null);
  const [verifyResult, setVerifyResult] = useState<VerifyResult | null>(null);
  const [loading, setLoading] = useState(false);

  const loadStatus = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const res = await apiCall<ChainStatus>('/blockchain/fabric/status');
      setChainStatus(res);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to load blockchain status');
    }
    setLoading(false);
  };

  const verifyChain = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      const res = await apiCall<VerifyResult>('/blockchain/fabric/verify-chain', { method: 'POST' });
      setVerifyResult(res);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Chain verification failed');
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
        {chainStatus && (
          <View style={styles.statsGrid}>
            <View style={styles.statCard}>
              <Text style={styles.statNumber}>{chainStatus.block_height}</Text>
              <Text style={styles.statLabel}>Block Height</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statNumber, { color: '#166534' }]}>{chainStatus.peers_connected}</Text>
              <Text style={styles.statLabel}>Peers</Text>
            </View>
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
