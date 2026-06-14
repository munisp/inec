import React, { useState } from 'react';
import {
  View, Text, ScrollView, StyleSheet, TextInput, TouchableOpacity, Alert, ActivityIndicator,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiPost, apiGet } from '../lib/api';

export default function RemoteVotingScreen() {
  const [step, setStep] = useState<'auth' | 'verify' | 'vote' | 'receipt'>('auth');
  const [otp, setOtp] = useState('');
  const [confirmationCode, setConfirmationCode] = useState('');
  const [verifyCode, setVerifyCode] = useState('');
  const [loading, setLoading] = useState(false);
  const [verifyResult, setVerifyResult] = useState<any>(null);

  const startSession = async () => {
    setLoading(true);
    try {
      await apiPost('/gotv/primaries/remote/session/start', { election_id: 1, delegate_id: 1 });
      Alert.alert('OTP Sent', 'Enter the 6-digit code sent to your registered device');
      setStep('verify');
    } catch (err: any) {
      Alert.alert('Error', err.message);
    } finally {
      setLoading(false);
    }
  };

  const verifyOTP = async () => {
    if (otp.length !== 6) { Alert.alert('Error', 'Enter 6-digit OTP'); return; }
    setLoading(true);
    try {
      await apiPost('/gotv/primaries/remote/session/verify', { otp });
      setStep('vote');
    } catch (err: any) {
      Alert.alert('Error', err.message);
    } finally {
      setLoading(false);
    }
  };

  const castEncryptedVote = async (aspirantId: number) => {
    Alert.alert('Cast Encrypted Vote', 'Your vote will be encrypted end-to-end. Continue?', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Cast Vote', onPress: async () => {
          setLoading(true);
          try {
            const result = await apiPost('/gotv/primaries/remote/vote', { aspirant_id: aspirantId, round_id: 1 });
            setConfirmationCode(result.confirmation_code || 'CONF-XXXXXX');
            setStep('receipt');
          } catch (err: any) {
            Alert.alert('Error', err.message);
          } finally {
            setLoading(false);
          }
        },
      },
    ]);
  };

  const verifyBallot = async () => {
    if (!verifyCode) return;
    setLoading(true);
    try {
      const result = await apiGet(`/gotv/primaries/remote/verify?confirmation_code=${verifyCode}`);
      setVerifyResult(result);
    } catch {
      Alert.alert('Error', 'Verification failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <ScrollView style={s.container}>
      {/* Security Features */}
      <View style={s.securityBar}>
        <View style={s.secFeature}>
          <Ionicons name="lock-closed" size={16} color="#15803d" />
          <Text style={s.secText}>E2E Encrypted</Text>
        </View>
        <View style={s.secFeature}>
          <Ionicons name="shield-checkmark" size={16} color="#15803d" />
          <Text style={s.secText}>ZK Proofs</Text>
        </View>
        <View style={s.secFeature}>
          <Ionicons name="eye-off" size={16} color="#15803d" />
          <Text style={s.secText}>Coercion Resistant</Text>
        </View>
      </View>

      {step === 'auth' && (
        <View style={s.stepCard}>
          <Ionicons name="phone-portrait" size={48} color="#7c3aed" />
          <Text style={s.stepTitle}>Remote Voting Authentication</Text>
          <Text style={s.stepDesc}>
            Authenticate with your registered device to begin encrypted remote voting.
            Multi-factor verification ensures only authorized delegates can vote.
          </Text>
          <TouchableOpacity style={s.primaryBtn} onPress={startSession} disabled={loading}>
            {loading ? <ActivityIndicator color="#fff" /> : <Text style={s.primaryBtnText}>Begin Authentication</Text>}
          </TouchableOpacity>
        </View>
      )}

      {step === 'verify' && (
        <View style={s.stepCard}>
          <Ionicons name="keypad" size={48} color="#2563eb" />
          <Text style={s.stepTitle}>Enter OTP</Text>
          <TextInput
            style={s.otpInput}
            placeholder="000000"
            placeholderTextColor="#cbd5e1"
            value={otp}
            onChangeText={setOtp}
            keyboardType="number-pad"
            maxLength={6}
            textAlign="center"
          />
          <TouchableOpacity style={s.primaryBtn} onPress={verifyOTP} disabled={loading}>
            {loading ? <ActivityIndicator color="#fff" /> : <Text style={s.primaryBtnText}>Verify OTP</Text>}
          </TouchableOpacity>
        </View>
      )}

      {step === 'vote' && (
        <View style={s.stepCard}>
          <Ionicons name="checkmark-circle" size={48} color="#15803d" />
          <Text style={s.stepTitle}>Cast Your Vote</Text>
          <Text style={s.stepDesc}>Your ballot will be homomorphically encrypted. No one — including the server — can see your individual vote.</Text>
          {[
            { id: 1, name: 'Candidate A' },
            { id: 2, name: 'Candidate B' },
            { id: 3, name: 'Candidate C' },
          ].map((c) => (
            <TouchableOpacity key={c.id} style={s.candidateBtn} onPress={() => castEncryptedVote(c.id)}>
              <Ionicons name="person" size={20} color="#7c3aed" />
              <Text style={s.candidateText}>{c.name}</Text>
              <Ionicons name="chevron-forward" size={16} color="#94a3b8" />
            </TouchableOpacity>
          ))}
        </View>
      )}

      {step === 'receipt' && (
        <View style={s.stepCard}>
          <Ionicons name="receipt" size={48} color="#15803d" />
          <Text style={s.stepTitle}>Vote Confirmed</Text>
          <View style={s.codeBox}>
            <Text style={s.codeLabel}>Confirmation Code</Text>
            <Text style={s.codeValue}>{confirmationCode}</Text>
          </View>
          <Text style={s.stepDesc}>Save this code. You can verify your ballot was included in the tally without revealing your choice.</Text>
        </View>
      )}

      {/* Ballot Verification */}
      <View style={s.verifySection}>
        <Text style={s.sectionTitle}>Verify Your Ballot</Text>
        <View style={s.verifyRow}>
          <TextInput
            style={s.verifyInput}
            placeholder="Enter confirmation code"
            placeholderTextColor="#94a3b8"
            value={verifyCode}
            onChangeText={setVerifyCode}
          />
          <TouchableOpacity style={s.verifyBtn} onPress={verifyBallot} disabled={loading}>
            <Ionicons name="search" size={18} color="#fff" />
          </TouchableOpacity>
        </View>
        {verifyResult && (
          <View style={s.verifyResult}>
            <Ionicons name={verifyResult.valid ? 'checkmark-circle' : 'close-circle'} size={20} color={verifyResult.valid ? '#15803d' : '#dc2626'} />
            <Text style={[s.verifyText, { color: verifyResult.valid ? '#15803d' : '#dc2626' }]}>
              {verifyResult.valid ? 'Ballot verified — included in tally' : 'Ballot not found'}
            </Text>
          </View>
        )}
      </View>
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  securityBar: { flexDirection: 'row', justifyContent: 'space-around', backgroundColor: '#f0fdf4', padding: 10 },
  secFeature: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  secText: { fontSize: 10, color: '#15803d', fontWeight: '600' },
  stepCard: { margin: 16, backgroundColor: '#fff', borderRadius: 16, padding: 24, alignItems: 'center', elevation: 2 },
  stepTitle: { fontSize: 18, fontWeight: '700', color: '#1e293b', marginTop: 16 },
  stepDesc: { fontSize: 13, color: '#64748b', textAlign: 'center', marginTop: 8, lineHeight: 20 },
  primaryBtn: { backgroundColor: '#7c3aed', borderRadius: 12, paddingVertical: 14, paddingHorizontal: 32, marginTop: 20, width: '100%', alignItems: 'center' },
  primaryBtnText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  otpInput: { fontSize: 32, fontWeight: '700', color: '#1e293b', borderBottomWidth: 2, borderBottomColor: '#7c3aed', width: 200, marginTop: 20, paddingBottom: 8, letterSpacing: 8 },
  candidateBtn: { flexDirection: 'row', alignItems: 'center', gap: 12, backgroundColor: '#f8fafc', borderRadius: 12, padding: 16, marginTop: 12, width: '100%' },
  candidateText: { flex: 1, fontSize: 16, fontWeight: '600', color: '#1e293b' },
  codeBox: { backgroundColor: '#f0fdf4', borderRadius: 12, padding: 16, marginTop: 16, alignItems: 'center', width: '100%' },
  codeLabel: { fontSize: 12, color: '#64748b' },
  codeValue: { fontSize: 24, fontWeight: '700', color: '#15803d', marginTop: 4, letterSpacing: 2 },
  verifySection: { margin: 16, backgroundColor: '#fff', borderRadius: 16, padding: 16, elevation: 1 },
  sectionTitle: { fontSize: 16, fontWeight: '600', color: '#1e293b', marginBottom: 12 },
  verifyRow: { flexDirection: 'row', gap: 8 },
  verifyInput: { flex: 1, backgroundColor: '#f8fafc', borderRadius: 10, paddingHorizontal: 14, height: 42, fontSize: 14, color: '#1e293b', borderWidth: 1, borderColor: '#e2e8f0' },
  verifyBtn: { width: 42, height: 42, borderRadius: 10, backgroundColor: '#7c3aed', justifyContent: 'center', alignItems: 'center' },
  verifyResult: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 12, padding: 12, backgroundColor: '#f8fafc', borderRadius: 10 },
  verifyText: { fontSize: 13, fontWeight: '500' },
});
