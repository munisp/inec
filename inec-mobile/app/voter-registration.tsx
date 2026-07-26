import { useEffect, useState } from 'react';
import { Alert, Linking, ScrollView, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { integrityApi, OfficialVoterService } from '../src/lib/api';

export default function VoterRegistrationScreen() {
  const [service, setService] = useState<OfficialVoterService | null>(null);
  const [notice, setNotice] = useState('This platform does not submit voter applications, retain voter-register copies, or determine eligibility.');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const loadService = async () => {
    setLoading(true);
    setError('');
    try {
      const response = await integrityApi.voterServices();
      const authoritative = response.services.find((item) => item.authoritative) || null;
      if (!authoritative) throw new Error('No authorised voter-service route is currently available.');
      setService(authoritative);
      setNotice(response.notice || notice);
    } catch (caught: unknown) {
      setService(null);
      setError(caught instanceof Error ? caught.message : 'Official voter-service navigation is unavailable.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void loadService(); }, []);

  const openOfficialService = async () => {
    if (!service) return;
    const canOpen = await Linking.canOpenURL(service.url);
    if (!canOpen) {
      Alert.alert('Service unavailable', 'This device cannot open the official voter-service address.');
      return;
    }
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await Linking.openURL(service.url);
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.content}>
      <View style={styles.card}>
        <View style={styles.iconWrap}><Ionicons name="person-add-outline" size={27} color="#166534" /></View>
        <Text style={styles.eyebrow}>OFFICIAL VOTER SERVICE</Text>
        <Text style={styles.title}>Registration and voter-record services</Text>
        <Text style={styles.description}>{notice}</Text>

        {loading ? <Text style={styles.status}>Retrieving authorised service navigation…</Text> : null}
        {error ? <View style={styles.errorBox}><Text style={styles.errorText}>{error}</Text><TouchableOpacity accessibilityRole="button" onPress={loadService}><Text style={styles.retry}>Retry lookup</Text></TouchableOpacity></View> : null}

        {service ? <>
          <View style={styles.serviceBox}>
            <Text style={styles.serviceLabel}>{service.label}</Text>
            <Text style={styles.servicePurpose}>{service.purpose}</Text>
            <Text style={styles.serviceUrl} numberOfLines={1}>{service.url}</Text>
          </View>
          <TouchableOpacity accessibilityRole="link" accessibilityLabel={`Open ${service.label}`} style={styles.button} onPress={openOfficialService} activeOpacity={0.8}>
            <Text style={styles.buttonText}>Open official INEC service</Text>
            <Ionicons name="open-outline" size={18} color="#fff" />
          </TouchableOpacity>
        </> : null}
      </View>

      <View style={styles.guidanceCard}>
        <Ionicons name="information-circle-outline" size={20} color="#0369a1" />
        <View style={{ flex: 1 }}>
          <Text style={styles.guidanceTitle}>Why this redirects to INEC</Text>
          <Text style={styles.guidanceText}>Registration, transfer, correction, eligibility, and biometric enrolment must remain in the official process. The platform provides safe navigation and may display authorised operational information; it never creates a VIN or PVC record locally.</Text>
        </View>
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  content: { padding: 16, paddingBottom: 40 },
  card: { backgroundColor: '#fff', borderRadius: 16, padding: 20, borderWidth: 1, borderColor: '#d1fae5' },
  iconWrap: { width: 48, height: 48, borderRadius: 24, backgroundColor: '#ecfdf5', alignItems: 'center', justifyContent: 'center', marginBottom: 14 },
  eyebrow: { color: '#166534', fontSize: 10, fontWeight: '800', letterSpacing: 1.2 },
  title: { color: '#111827', fontSize: 22, fontWeight: '800', marginTop: 4 },
  description: { color: '#475569', fontSize: 13, lineHeight: 19, marginTop: 9 },
  status: { color: '#475569', fontSize: 13, marginTop: 18 },
  errorBox: { marginTop: 18, backgroundColor: '#fef2f2', borderWidth: 1, borderColor: '#fecaca', borderRadius: 10, padding: 12 },
  errorText: { color: '#991b1b', fontSize: 13 },
  retry: { color: '#166534', fontWeight: '800', fontSize: 13, marginTop: 8 },
  serviceBox: { marginTop: 18, padding: 13, borderRadius: 11, backgroundColor: '#f0fdf4', borderWidth: 1, borderColor: '#bbf7d0' },
  serviceLabel: { color: '#14532d', fontSize: 14, fontWeight: '800' },
  servicePurpose: { color: '#166534', fontSize: 12, lineHeight: 17, marginTop: 5 },
  serviceUrl: { color: '#047857', fontFamily: 'monospace', fontSize: 10, marginTop: 8 },
  button: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8, marginTop: 14, borderRadius: 10, paddingVertical: 13, backgroundColor: '#166534' },
  buttonText: { color: '#fff', fontSize: 14, fontWeight: '800' },
  guidanceCard: { flexDirection: 'row', gap: 10, backgroundColor: '#eff6ff', borderWidth: 1, borderColor: '#bfdbfe', borderRadius: 14, padding: 14, marginTop: 14 },
  guidanceTitle: { color: '#0c4a6e', fontSize: 13, fontWeight: '800' },
  guidanceText: { color: '#1e3a5f', fontSize: 12, lineHeight: 18, marginTop: 4 },
});
