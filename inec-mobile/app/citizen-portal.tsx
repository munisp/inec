import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';

interface VoterInfo {
  vin: string;
  pvc_number: string;
  first_name: string;
  last_name: string;
  polling_unit: string;
  state: string;
  lga: string;
  ward: string;
  pvc_collected: boolean;
}

export default function CitizenPortalScreen() {
  const [searchQuery, setSearchQuery] = useState('');
  const [voterInfo, setVoterInfo] = useState<VoterInfo | null>(null);
  const [loading, setLoading] = useState(false);

  const searchVoter = async () => {
    if (!searchQuery.trim()) return;
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const res = await apiCall<VoterInfo>(`/ems/voters/search?vin=${encodeURIComponent(searchQuery)}`);
      setVoterInfo(res);
    } catch (e: unknown) {
      Alert.alert('Not Found', 'No voter record found for this VIN.');
      setVoterInfo(null);
    }
    setLoading(false);
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="people-outline" size={24} color="#166534" />
          <Text style={styles.cardTitle}>Citizen Portal</Text>
        </View>
        <Text style={styles.muted}>Verify your voter registration, check PVC collection status, and find your polling unit.</Text>
        <TextInput
          style={styles.input}
          placeholder="Enter VIN (Voter ID Number)"
          value={searchQuery}
          onChangeText={setSearchQuery}
          autoCapitalize="characters"
          returnKeyType="search"
          onSubmitEditing={searchVoter}
        />
        <TouchableOpacity style={styles.button} onPress={searchVoter} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>{loading ? 'Searching...' : 'Search'}</Text>
        </TouchableOpacity>
      </View>

      {voterInfo && (
        <View style={styles.card}>
          <View style={styles.cardHeader}>
            <Ionicons name="person-outline" size={24} color="#166534" />
            <Text style={styles.cardTitle}>{voterInfo.first_name} {voterInfo.last_name}</Text>
          </View>
          {[
            { label: 'VIN', value: voterInfo.vin },
            { label: 'PVC Number', value: voterInfo.pvc_number },
            { label: 'State', value: voterInfo.state },
            { label: 'LGA', value: voterInfo.lga },
            { label: 'Ward', value: voterInfo.ward },
            { label: 'Polling Unit', value: voterInfo.polling_unit },
          ].map((row) => (
            <View key={row.label} style={styles.infoRow}>
              <Text style={styles.infoLabel}>{row.label}</Text>
              <Text style={styles.infoValue}>{row.value}</Text>
            </View>
          ))}
          <View style={[styles.resultBanner, { backgroundColor: voterInfo.pvc_collected ? '#dcfce7' : '#fef9c3' }]}>
            <Ionicons name={voterInfo.pvc_collected ? 'checkmark-circle' : 'alert-circle'} size={24} color={voterInfo.pvc_collected ? '#166534' : '#ca8a04'} />
            <Text style={{ marginLeft: 10, fontWeight: '600', color: voterInfo.pvc_collected ? '#166534' : '#ca8a04' }}>
              {voterInfo.pvc_collected ? 'PVC Collected' : 'PVC Not Yet Collected'}
            </Text>
          </View>
        </View>
      )}

      <View style={styles.card}>
        <Text style={styles.cardTitle}>Services</Text>
        {[
          { icon: 'search-outline' as const, title: 'Polling Unit Finder', desc: 'Find your assigned polling unit location' },
          { icon: 'card-outline' as const, title: 'PVC Status', desc: 'Check if your PVC is ready for collection' },
          { icon: 'megaphone-outline' as const, title: 'SMS Results', desc: 'Get results via SMS to your registered phone' },
          { icon: 'call-outline' as const, title: 'USSD Access', desc: 'Dial *723# for election info without internet' },
        ].map((svc) => (
          <View key={svc.title} style={styles.capRow}>
            <Ionicons name={svc.icon} size={20} color="#166534" />
            <View style={{ flex: 1, marginLeft: 10 }}>
              <Text style={{ fontSize: 14, fontWeight: '600', color: '#111827' }}>{svc.title}</Text>
              <Text style={styles.muted}>{svc.desc}</Text>
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
  input: { borderWidth: 1, borderColor: '#d1d5db', borderRadius: 12, padding: 14, fontSize: 15, backgroundColor: '#f9fafb', marginBottom: 8 },
  button: { backgroundColor: '#166534', borderRadius: 12, padding: 14, alignItems: 'center', marginTop: 8 },
  buttonText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  infoRow: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 8, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
  infoLabel: { fontSize: 13, color: '#6b7280' },
  infoValue: { fontSize: 13, fontWeight: '600', color: '#111827' },
  resultBanner: { flexDirection: 'row', alignItems: 'center', borderRadius: 12, padding: 14, marginTop: 12 },
  capRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
});
