import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';

interface RegistrationResult {
  vin: string;
  pvc_number: string;
  message: string;
}

export default function VoterRegistrationScreen() {
  const [formData, setFormData] = useState({
    first_name: '',
    last_name: '',
    date_of_birth: '',
    phone: '',
    state_code: '',
    lga_code: '',
  });
  const [result, setResult] = useState<RegistrationResult | null>(null);
  const [loading, setLoading] = useState(false);

  const updateField = (field: string, value: string) => {
    setFormData(prev => ({ ...prev, [field]: value }));
  };

  const submitRegistration = async () => {
    if (!formData.first_name || !formData.last_name || !formData.date_of_birth) {
      Alert.alert('Required Fields', 'Please fill in first name, last name, and date of birth.');
      return;
    }
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Heavy);
    try {
      const res = await apiCall<RegistrationResult>('/ems/voters/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          ...formData,
          ward_code: 'W001',
          polling_unit_code: 'PU001',
          biometric_data: 'pending_capture',
        }),
      });
      setResult(res);
      Alert.alert('Success', `Voter registered. VIN: ${res.vin}`);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Registration failed');
    }
    setLoading(false);
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="person-add-outline" size={24} color="#166534" />
          <Text style={styles.cardTitle}>Voter Registration</Text>
        </View>
        <Text style={styles.muted}>Register new voters with biometric enrollment at INEC registration centers.</Text>

        {[
          { key: 'first_name', label: 'First Name', placeholder: 'Enter first name' },
          { key: 'last_name', label: 'Last Name', placeholder: 'Enter last name' },
          { key: 'date_of_birth', label: 'Date of Birth', placeholder: 'YYYY-MM-DD' },
          { key: 'phone', label: 'Phone Number', placeholder: '+234...' },
          { key: 'state_code', label: 'State Code', placeholder: 'e.g. LA, KN, AB' },
          { key: 'lga_code', label: 'LGA Code', placeholder: 'Local Government Area code' },
        ].map((field) => (
          <View key={field.key} style={{ marginBottom: 12 }}>
            <Text style={styles.fieldLabel}>{field.label}</Text>
            <TextInput
              style={styles.input}
              placeholder={field.placeholder}
              value={formData[field.key as keyof typeof formData]}
              onChangeText={(v) => updateField(field.key, v)}
              autoCapitalize={field.key === 'state_code' ? 'characters' : 'words'}
            />
          </View>
        ))}

        <TouchableOpacity style={styles.button} onPress={submitRegistration} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>{loading ? 'Registering...' : 'Register Voter'}</Text>
        </TouchableOpacity>
      </View>

      {result && (
        <View style={[styles.card, { backgroundColor: '#dcfce7' }]}>
          <View style={styles.cardHeader}>
            <Ionicons name="checkmark-circle" size={24} color="#166534" />
            <Text style={[styles.cardTitle, { color: '#166534' }]}>Registration Complete</Text>
          </View>
          <View style={styles.infoRow}>
            <Text style={styles.infoLabel}>VIN</Text>
            <Text style={styles.infoValue}>{result.vin}</Text>
          </View>
          <View style={styles.infoRow}>
            <Text style={styles.infoLabel}>PVC Number</Text>
            <Text style={styles.infoValue}>{result.pvc_number}</Text>
          </View>
          <Text style={[styles.muted, { marginTop: 8 }]}>Biometric capture required at enrollment center.</Text>
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  card: { backgroundColor: '#fff', borderRadius: 16, padding: 16, marginBottom: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  cardHeader: { flexDirection: 'row', alignItems: 'center', gap: 10, marginBottom: 12 },
  cardTitle: { fontSize: 16, fontWeight: '700', color: '#111827' },
  muted: { fontSize: 13, color: '#9ca3af', marginBottom: 8 },
  fieldLabel: { fontSize: 13, fontWeight: '600', color: '#374151', marginBottom: 4 },
  input: { borderWidth: 1, borderColor: '#d1d5db', borderRadius: 12, padding: 14, fontSize: 15, backgroundColor: '#f9fafb' },
  button: { backgroundColor: '#166534', borderRadius: 12, padding: 14, alignItems: 'center', marginTop: 8 },
  buttonText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  infoRow: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 8, borderBottomWidth: 1, borderBottomColor: '#bbf7d0' },
  infoLabel: { fontSize: 13, color: '#166534' },
  infoValue: { fontSize: 13, fontWeight: '600', color: '#166534' },
});
