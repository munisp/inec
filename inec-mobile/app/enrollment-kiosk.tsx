import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';

type KioskStep = 'welcome' | 'capture_face' | 'capture_fingerprint' | 'capture_iris' | 'quality_check' | 'review' | 'complete';

const STEPS: { key: KioskStep; label: string; icon: string; desc: string }[] = [
  { key: 'welcome', label: 'Welcome', icon: 'hand-left-outline', desc: 'Start biometric enrollment' },
  { key: 'capture_face', label: 'Face Capture', icon: 'person-circle-outline', desc: 'Position face in oval guide' },
  { key: 'capture_fingerprint', label: 'Fingerprint', icon: 'finger-print-outline', desc: 'Place finger on sensor' },
  { key: 'capture_iris', label: 'Iris Scan', icon: 'eye-outline', desc: 'Look at the iris scanner' },
  { key: 'quality_check', label: 'Quality Check', icon: 'shield-checkmark-outline', desc: 'Verifying biometric quality' },
  { key: 'review', label: 'Review', icon: 'document-text-outline', desc: 'Confirm enrollment data' },
  { key: 'complete', label: 'Complete', icon: 'checkmark-done-circle-outline', desc: 'Enrollment successful' },
];

export default function EnrollmentKioskScreen() {
  const [currentStep, setCurrentStep] = useState(0);

  const nextStep = () => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    if (currentStep < STEPS.length - 1) {
      setCurrentStep(prev => prev + 1);
    } else {
      Alert.alert('Enrollment Complete', 'Biometric enrollment has been submitted.');
      setCurrentStep(0);
    }
  };

  const reset = () => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    setCurrentStep(0);
  };

  const step = STEPS[currentStep];

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      {/* Progress */}
      <View style={styles.progressBar}>
        {STEPS.map((s, i) => (
          <View key={s.key} style={[styles.progressDot, i <= currentStep && { backgroundColor: '#166534' }]} />
        ))}
      </View>

      <View style={styles.card}>
        <View style={{ alignItems: 'center', paddingVertical: 20 }}>
          <Ionicons name={step.icon as keyof typeof Ionicons.glyphMap} size={64} color="#166534" />
          <Text style={{ fontSize: 22, fontWeight: '700', color: '#111827', marginTop: 16 }}>{step.label}</Text>
          <Text style={[styles.muted, { textAlign: 'center', marginTop: 8, fontSize: 15 }]}>{step.desc}</Text>
          <Text style={[styles.muted, { marginTop: 4 }]}>Step {currentStep + 1} of {STEPS.length}</Text>
        </View>

        <TouchableOpacity style={styles.button} onPress={nextStep} activeOpacity={0.8}>
          <Text style={styles.buttonText}>
            {currentStep === STEPS.length - 1 ? 'Finish & Reset' : 'Next Step'}
          </Text>
        </TouchableOpacity>

        {currentStep > 0 && (
          <TouchableOpacity style={[styles.button, { backgroundColor: '#6b7280', marginTop: 8 }]} onPress={reset} activeOpacity={0.8}>
            <Text style={styles.buttonText}>Start Over</Text>
          </TouchableOpacity>
        )}
      </View>

      <View style={styles.card}>
        <Text style={styles.cardTitle}>Enrollment Steps</Text>
        {STEPS.map((s, i) => (
          <View key={s.key} style={styles.stepRow}>
            <Ionicons name={i < currentStep ? 'checkmark-circle' : i === currentStep ? 'radio-button-on' : 'radio-button-off'} size={20} color={i <= currentStep ? '#166534' : '#d1d5db'} />
            <Text style={{ marginLeft: 10, fontSize: 14, color: i <= currentStep ? '#111827' : '#9ca3af', fontWeight: i === currentStep ? '600' : '400' }}>{s.label}</Text>
          </View>
        ))}
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  card: { backgroundColor: '#fff', borderRadius: 16, padding: 16, marginBottom: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  cardTitle: { fontSize: 16, fontWeight: '700', color: '#111827', marginBottom: 12 },
  muted: { fontSize: 13, color: '#9ca3af' },
  button: { backgroundColor: '#166534', borderRadius: 12, padding: 14, alignItems: 'center', marginTop: 8 },
  buttonText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  progressBar: { flexDirection: 'row', justifyContent: 'center', gap: 6, marginBottom: 16 },
  progressDot: { width: 10, height: 10, borderRadius: 5, backgroundColor: '#d1d5db' },
  stepRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 8 },
});
