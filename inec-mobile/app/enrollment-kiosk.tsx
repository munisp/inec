import { useRef, useState } from 'react';
import { View, Text, TextInput, StyleSheet, ScrollView, TouchableOpacity, Image, ActivityIndicator } from 'react-native';
import { CameraView, useCameraPermissions } from 'expo-camera';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';

type KioskStep = 'welcome' | 'identity' | 'capture_face' | 'review' | 'complete';

const STEPS: { key: KioskStep; label: string; icon: string; desc: string }[] = [
  { key: 'welcome', label: 'Welcome', icon: 'hand-left-outline', desc: 'Start biometric enrollment' },
  { key: 'identity', label: 'Identity', icon: 'card-outline', desc: 'Enter voter VIN and device ID' },
  { key: 'capture_face', label: 'Face Capture', icon: 'person-circle-outline', desc: 'Capture a live face photo with the device camera' },
  { key: 'review', label: 'Review', icon: 'document-text-outline', desc: 'Confirm enrollment data' },
  { key: 'complete', label: 'Complete', icon: 'checkmark-done-circle-outline', desc: 'Server-confirmed enrollment result' },
];

export default function EnrollmentKioskScreen() {
  const [step, setStep] = useState<KioskStep>('welcome');
  const [vin, setVin] = useState('');
  const [deviceId, setDeviceId] = useState('');
  const [faceImage, setFaceImage] = useState<{ uri: string; base64: string } | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [enrollResponse, setEnrollResponse] = useState<Record<string, unknown> | null>(null);
  const [permission, requestPermission] = useCameraPermissions();
  const cameraRef = useRef<CameraView>(null);

  const currentStep = STEPS.findIndex(s => s.key === step);
  const identityValid = vin.trim().length > 0 && deviceId.trim().length > 0;

  const goTo = (s: KioskStep) => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    setStep(s);
  };

  const captureFace = async () => {
    if (!cameraRef.current) return;
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Heavy);
    try {
      const photo = await cameraRef.current.takePictureAsync({ quality: 0.8, base64: true });
      if (photo?.base64) {
        setFaceImage({ uri: photo.uri, base64: photo.base64 });
        goTo('review');
      }
    } catch {
      setSubmitError('Camera capture failed — no image was recorded. Please try again.');
    }
  };

  // Real submission: POST /biometric/engine/enroll (backend proxies to the
  // configured biometric pipeline and fails loudly when it is absent).
  // Success is shown ONLY from a real server response — never assumed.
  const submitEnrollment = async () => {
    if (!identityValid || !faceImage) return;
    setSubmitting(true);
    setSubmitError(null);
    setEnrollResponse(null);
    try {
      const res = await apiCall<Record<string, unknown>>('/biometric/engine/enroll', {
        method: 'POST',
        body: JSON.stringify({
          vin: vin.trim(),
          modality: 'face',
          device_id: deviceId.trim(),
          image_data: faceImage.base64,
        }),
      });
      setEnrollResponse(res);
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
      goTo('complete');
    } catch (e: unknown) {
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      setSubmitError(e instanceof Error ? e.message : 'Enrollment submission failed');
    }
    setSubmitting(false);
  };

  const reset = () => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    setVin('');
    setDeviceId('');
    setFaceImage(null);
    setSubmitError(null);
    setEnrollResponse(null);
    setStep('welcome');
  };

  // Face capture uses the real device camera (full-screen, like the KYC flow).
  if (step === 'capture_face') {
    if (!permission?.granted) {
      return (
        <View style={[styles.container, { justifyContent: 'center' }]}>
          <View style={[styles.card, { alignItems: 'center' }]}>
            <Ionicons name="camera-outline" size={48} color="#166534" />
            <Text style={[styles.cardTitle, { marginTop: 12 }]}>Camera Access Required</Text>
            <Text style={[styles.muted, { textAlign: 'center' }]}>Face enrollment requires a live photo captured by this device's camera.</Text>
            <TouchableOpacity style={styles.button} onPress={requestPermission} activeOpacity={0.8}>
              <Text style={styles.buttonText}>Grant Camera Access</Text>
            </TouchableOpacity>
            <TouchableOpacity style={[styles.button, { backgroundColor: '#6b7280' }]} onPress={() => goTo('identity')} activeOpacity={0.8}>
              <Text style={styles.buttonText}>Back</Text>
            </TouchableOpacity>
          </View>
        </View>
      );
    }
    return (
      <View style={{ flex: 1 }}>
        <CameraView ref={cameraRef} style={{ flex: 1 }} facing="front">
          <View style={{ flex: 1, justifyContent: 'center', alignItems: 'center' }}>
            <Text style={{ color: '#fff', fontSize: 14, fontWeight: '600', textShadowColor: '#000', textShadowRadius: 4 }}>
              Center your face in the frame
            </Text>
          </View>
          <View style={{ position: 'absolute', bottom: 40, left: 0, right: 0, alignItems: 'center' }}>
            <TouchableOpacity onPress={captureFace} style={styles.captureButton} activeOpacity={0.8}>
              <View style={styles.captureInner} />
            </TouchableOpacity>
          </View>
        </CameraView>
      </View>
    );
  }

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      {/* Progress */}
      <View style={styles.progressBar}>
        {STEPS.map((s, i) => (
          <View key={s.key} style={[styles.progressDot, i <= currentStep && { backgroundColor: '#166534' }]} />
        ))}
      </View>

      {step === 'welcome' && (
        <View style={styles.card}>
          <View style={{ alignItems: 'center', paddingVertical: 20 }}>
            <Ionicons name="hand-left-outline" size={64} color="#166534" />
            <Text style={{ fontSize: 22, fontWeight: '700', color: '#111827', marginTop: 16 }}>Biometric Enrollment</Text>
            <Text style={[styles.muted, { textAlign: 'center', marginTop: 8, fontSize: 15 }]}>
              Enroll a voter's face biometric against the ABIS pipeline. The record is saved only when the server confirms it.
            </Text>
          </View>
          <View style={[styles.card, { backgroundColor: '#fffbeb', marginBottom: 12 }]}>
            <Text style={[styles.muted, { color: '#92400e', marginBottom: 0 }]}>
              Fingerprint and iris enrollment require dedicated BVAS kiosk sensors and cannot be captured by this device — those modalities are not offered here rather than simulated.
            </Text>
          </View>
          <TouchableOpacity style={styles.button} onPress={() => goTo('identity')} activeOpacity={0.8}>
            <Text style={styles.buttonText}>Start Enrollment</Text>
          </TouchableOpacity>
        </View>
      )}

      {step === 'identity' && (
        <View style={styles.card}>
          <View style={{ alignItems: 'center', paddingVertical: 12 }}>
            <Ionicons name="card-outline" size={48} color="#166534" />
            <Text style={{ fontSize: 20, fontWeight: '700', color: '#111827', marginTop: 12 }}>Voter Identity</Text>
          </View>
          <Text style={styles.inputLabel}>Voter Identification Number (VIN)</Text>
          <TextInput
            style={styles.input}
            value={vin}
            onChangeText={setVin}
            placeholder="Enter voter VIN"
            autoCapitalize="characters"
            autoComplete="off"
          />
          <Text style={styles.inputLabel}>Enrollment Device ID</Text>
          <TextInput
            style={styles.input}
            value={deviceId}
            onChangeText={setDeviceId}
            placeholder="e.g. BVAS-001"
            autoCapitalize="characters"
            autoComplete="off"
          />
          <TouchableOpacity
            style={[styles.button, !identityValid && { opacity: 0.5 }]}
            disabled={!identityValid}
            onPress={() => goTo('capture_face')}
            activeOpacity={0.8}
          >
            <Text style={styles.buttonText}>Continue to Face Capture</Text>
          </TouchableOpacity>
          <TouchableOpacity style={[styles.button, { backgroundColor: '#6b7280' }]} onPress={() => goTo('welcome')} activeOpacity={0.8}>
            <Text style={styles.buttonText}>Back</Text>
          </TouchableOpacity>
        </View>
      )}

      {step === 'review' && (
        <View style={styles.card}>
          <View style={{ alignItems: 'center', paddingVertical: 12 }}>
            <Ionicons name="document-text-outline" size={48} color="#166534" />
            <Text style={{ fontSize: 20, fontWeight: '700', color: '#111827', marginTop: 12 }}>Review & Submit</Text>
          </View>
          {faceImage && <Image source={{ uri: faceImage.uri }} style={styles.previewImage} />}
          <View style={styles.reviewRow}><Text style={styles.reviewLabel}>VIN</Text><Text style={styles.reviewValue}>{vin}</Text></View>
          <View style={styles.reviewRow}><Text style={styles.reviewLabel}>Device</Text><Text style={styles.reviewValue}>{deviceId}</Text></View>
          <View style={styles.reviewRow}><Text style={styles.reviewLabel}>Modality</Text><Text style={styles.reviewValue}>Face (camera capture)</Text></View>

          {submitError && (
            <View style={styles.errorBanner}>
              <Ionicons name="alert-circle" size={20} color="#dc2626" />
              <View style={{ flex: 1, marginLeft: 8 }}>
                <Text style={{ color: '#dc2626', fontWeight: '700' }}>Enrollment NOT saved</Text>
                <Text style={{ color: '#7f1d1d', fontSize: 13, marginTop: 2 }}>{submitError}</Text>
              </View>
            </View>
          )}

          <TouchableOpacity
            style={[styles.button, submitting && { opacity: 0.5 }]}
            disabled={submitting}
            onPress={submitEnrollment}
            activeOpacity={0.8}
          >
            {submitting ? <ActivityIndicator color="#fff" /> : <Text style={styles.buttonText}>Submit Enrollment</Text>}
          </TouchableOpacity>
          <TouchableOpacity style={[styles.button, { backgroundColor: '#6b7280' }]} onPress={() => goTo('capture_face')} disabled={submitting} activeOpacity={0.8}>
            <Text style={styles.buttonText}>Retake Photo</Text>
          </TouchableOpacity>
        </View>
      )}

      {step === 'complete' && (
        <View style={styles.card}>
          <View style={{ alignItems: 'center', paddingVertical: 20 }}>
            <Ionicons name="checkmark-done-circle-outline" size={64} color="#166534" />
            <Text style={{ fontSize: 22, fontWeight: '700', color: '#166534', marginTop: 16 }}>Enrollment Confirmed</Text>
            <Text style={[styles.muted, { textAlign: 'center', marginTop: 8, fontSize: 15 }]}>
              The biometric pipeline accepted the enrollment for VIN {vin}.
            </Text>
            {enrollResponse && (
              <View style={[styles.reviewRow, { marginTop: 12, alignSelf: 'stretch' }]}>
                <Text style={styles.reviewLabel}>Server response</Text>
                <Text style={[styles.reviewValue, { flex: 1, textAlign: 'right' }]} numberOfLines={3}>
                  {Object.entries(enrollResponse)
                    .filter(([, v]) => typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean')
                    .map(([k, v]) => `${k}: ${String(v)}`)
                    .join(' | ') || 'accepted'}
                </Text>
              </View>
            )}
          </View>
          <TouchableOpacity style={styles.button} onPress={reset} activeOpacity={0.8}>
            <Text style={styles.buttonText}>Enroll Next Voter</Text>
          </TouchableOpacity>
        </View>
      )}

      {/* Step overview */}
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
  inputLabel: { fontSize: 13, fontWeight: '600', color: '#374151', marginBottom: 6 },
  input: { backgroundColor: '#f9fafb', borderWidth: 1, borderColor: '#d1d5db', borderRadius: 12, padding: 12, fontSize: 15, color: '#111827', marginBottom: 12 },
  button: { backgroundColor: '#166534', borderRadius: 12, padding: 14, alignItems: 'center', marginTop: 8 },
  buttonText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  progressBar: { flexDirection: 'row', justifyContent: 'center', gap: 6, marginBottom: 16 },
  progressDot: { width: 10, height: 10, borderRadius: 5, backgroundColor: '#d1d5db' },
  stepRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 8 },
  captureButton: { width: 72, height: 72, borderRadius: 36, backgroundColor: 'rgba(255,255,255,0.3)', justifyContent: 'center', alignItems: 'center', borderWidth: 3, borderColor: '#fff' },
  captureInner: { width: 56, height: 56, borderRadius: 28, backgroundColor: '#fff' },
  previewImage: { width: 160, height: 160, borderRadius: 80, alignSelf: 'center', marginVertical: 12, borderWidth: 3, borderColor: '#166534' },
  reviewRow: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 8, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
  reviewLabel: { fontSize: 14, color: '#6b7280' },
  reviewValue: { fontSize: 14, fontWeight: '600', color: '#111827' },
  errorBanner: { flexDirection: 'row', alignItems: 'flex-start', backgroundColor: '#fef2f2', borderRadius: 12, padding: 12, marginTop: 12 },
});
