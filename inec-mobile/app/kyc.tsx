import { useState, useCallback, useRef } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity, Image, Alert,
  Platform, Animated, ActivityIndicator,
} from 'react-native';
import { CameraView, useCameraPermissions } from 'expo-camera';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { kycApi, KYCResult, LivenessResult } from '../src/lib/api';

type Step = 'start' | 'id_capture' | 'selfie' | 'liveness' | 'processing' | 'result';

const STEPS: { key: Step; title: string; icon: keyof typeof Ionicons.glyphMap }[] = [
  { key: 'start', title: 'Begin', icon: 'shield-checkmark-outline' },
  { key: 'id_capture', title: 'ID Photo', icon: 'card-outline' },
  { key: 'selfie', title: 'Selfie', icon: 'person-circle-outline' },
  { key: 'liveness', title: 'Liveness', icon: 'eye-outline' },
  { key: 'processing', title: 'Verify', icon: 'hourglass-outline' },
  { key: 'result', title: 'Result', icon: 'checkmark-circle-outline' },
];

export default function KYCScreen() {
  const [permission, requestPermission] = useCameraPermissions();
  const [step, setStep] = useState<Step>('start');
  const [idPhoto, setIdPhoto] = useState<string | null>(null);
  const [selfiePhoto, setSelfiePhoto] = useState<string | null>(null);
  const [kycResult, setKycResult] = useState<KYCResult | null>(null);
  const [livenessResult, setLivenessResult] = useState<LivenessResult | null>(null);
  const [processing, setProcessing] = useState(false);
  const cameraRef = useRef<CameraView>(null);
  const progressAnim = useRef(new Animated.Value(0)).current;

  const currentStepIndex = STEPS.findIndex(s => s.key === step);

  const animateProgress = (toStep: number) => {
    Animated.spring(progressAnim, { toValue: toStep / (STEPS.length - 1), useNativeDriver: false }).start();
  };

  const goToStep = (s: Step) => {
    const idx = STEPS.findIndex(st => st.key === s);
    animateProgress(idx);
    setStep(s);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
  };

  const takePhoto = async (type: 'id' | 'selfie') => {
    if (!cameraRef.current) return;
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Heavy);
    try {
      const photo = await cameraRef.current.takePictureAsync({ quality: 0.8, base64: false });
      if (photo) {
        if (type === 'id') {
          setIdPhoto(photo.uri);
          goToStep('selfie');
        } else {
          setSelfiePhoto(photo.uri);
          goToStep('liveness');
        }
      }
    } catch (e) {
      Alert.alert('Error', 'Failed to capture photo');
    }
  };

  const runLiveness = async () => {
    if (!selfiePhoto) return;
    goToStep('processing');
    setProcessing(true);
    try {
      const form = new FormData();
      form.append('selfie', { uri: selfiePhoto, type: 'image/jpeg', name: 'selfie.jpg' } as unknown as Blob);
      const result = await kycApi.liveness(form);
      setLivenessResult(result);
      Haptics.notificationAsync(
        result.passed ? Haptics.NotificationFeedbackType.Success : Haptics.NotificationFeedbackType.Warning
      );
    } catch {
      setLivenessResult({ user_id: 0, passed: false, confidence: 0, method: 'error', anti_spoofing_score: 0, checks: [], timestamp: '' });
    }

    if (idPhoto && selfiePhoto) {
      try {
        const form = new FormData();
        form.append('id_document', { uri: idPhoto, type: 'image/jpeg', name: 'id.jpg' } as unknown as Blob);
        form.append('selfie', { uri: selfiePhoto, type: 'image/jpeg', name: 'selfie.jpg' } as unknown as Blob);
        form.append('id_type', 'nin');
        form.append('id_number', '12345678901');
        const result = await kycApi.verify(form);
        setKycResult(result);
      } catch {
        setKycResult({ user_id: 0, status: 'rejected', identity_match_score: 0, document_verified: false, face_match_score: 0, liveness_passed: false, risk_score: 1, checks_performed: [], flags: ['verification_error'], verification_timestamp: '' });
      }
    }
    setProcessing(false);
    goToStep('result');
  };

  const progressWidth = progressAnim.interpolate({ inputRange: [0, 1], outputRange: ['0%', '100%'] });

  if (step === 'id_capture' || step === 'selfie') {
    if (!permission?.granted) {
      return (
        <View style={styles.container}>
          <View style={styles.permissionCard}>
            <Ionicons name="camera-outline" size={48} color="#166534" />
            <Text style={styles.permissionTitle}>Camera Access Required</Text>
            <Text style={styles.permissionText}>
              {step === 'id_capture' ? 'Take a photo of your government-issued ID' : 'Take a selfie for face verification'}
            </Text>
            <TouchableOpacity style={styles.primaryButton} onPress={requestPermission}>
              <Text style={styles.primaryButtonText}>Grant Camera Access</Text>
            </TouchableOpacity>
          </View>
        </View>
      );
    }

    return (
      <View style={styles.cameraContainer}>
        <CameraView
          ref={cameraRef}
          style={styles.camera}
          facing={step === 'selfie' ? 'front' : 'back'}
        >
          <View style={styles.cameraOverlay}>
            <View style={styles.cameraGuide}>
              <View style={[styles.corner, styles.topLeft]} />
              <View style={[styles.corner, styles.topRight]} />
              <View style={[styles.corner, styles.bottomLeft]} />
              <View style={[styles.corner, styles.bottomRight]} />
            </View>
            <Text style={styles.cameraHint}>
              {step === 'id_capture' ? 'Align your ID within the frame' : 'Center your face in the frame'}
            </Text>
          </View>
          <View style={styles.cameraControls}>
            <TouchableOpacity style={styles.captureButton} onPress={() => takePhoto(step === 'id_capture' ? 'id' : 'selfie')}>
              <View style={styles.captureInner} />
            </TouchableOpacity>
          </View>
        </CameraView>
      </View>
    );
  }

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}>
      {/* Progress bar */}
      <View style={styles.progressContainer}>
        <View style={styles.progressTrack}>
          <Animated.View style={[styles.progressFill, { width: progressWidth }]} />
        </View>
        <View style={styles.stepsRow}>
          {STEPS.map((s, i) => (
            <View key={s.key} style={styles.stepItem}>
              <View style={[styles.stepCircle, i <= currentStepIndex && styles.stepCircleActive]}>
                <Ionicons name={s.icon} size={14} color={i <= currentStepIndex ? '#fff' : '#9ca3af'} />
              </View>
              <Text style={[styles.stepLabel, i <= currentStepIndex && styles.stepLabelActive]}>{s.title}</Text>
            </View>
          ))}
        </View>
      </View>

      {step === 'start' && (
        <View style={styles.startCard}>
          <View style={styles.startIconCircle}>
            <Ionicons name="shield-checkmark" size={40} color="#166534" />
          </View>
          <Text style={styles.startTitle}>Identity Verification</Text>
          <Text style={styles.startSubtitle}>
            Complete KYC verification to access all observer features. You will need:
          </Text>
          <View style={styles.requirementsList}>
            {['Government-issued ID (NIN, Voter Card, Passport)', 'A clear selfie photo', 'Complete a liveness check'].map((req, i) => (
              <View key={i} style={styles.requirementItem}>
                <Ionicons name="checkmark-circle" size={18} color="#166534" />
                <Text style={styles.requirementText}>{req}</Text>
              </View>
            ))}
          </View>
          <TouchableOpacity style={styles.primaryButton} onPress={() => goToStep('id_capture')}>
            <Ionicons name="arrow-forward" size={18} color="#fff" />
            <Text style={styles.primaryButtonText}>Start Verification</Text>
          </TouchableOpacity>
        </View>
      )}

      {step === 'liveness' && (
        <View style={styles.startCard}>
          <View style={[styles.startIconCircle, { backgroundColor: '#ede9fe' }]}>
            <Ionicons name="eye" size={40} color="#7c3aed" />
          </View>
          <Text style={styles.startTitle}>Liveness Check</Text>
          <Text style={styles.startSubtitle}>
            We need to verify you are a real person. The check analyzes facial movement and texture.
          </Text>
          {selfiePhoto && <Image source={{ uri: selfiePhoto }} style={styles.previewImage} />}
          <TouchableOpacity style={[styles.primaryButton, { backgroundColor: '#7c3aed' }]} onPress={runLiveness}>
            <Ionicons name="scan" size={18} color="#fff" />
            <Text style={styles.primaryButtonText}>Run Liveness Check</Text>
          </TouchableOpacity>
        </View>
      )}

      {step === 'processing' && (
        <View style={styles.processingCard}>
          <ActivityIndicator size="large" color="#166534" />
          <Text style={styles.processingTitle}>Verifying Identity...</Text>
          <Text style={styles.processingSubtitle}>Analyzing documents, face match, and liveness</Text>
        </View>
      )}

      {step === 'result' && (
        <View>
          {livenessResult && (
            <View style={[styles.resultCard, { borderLeftColor: livenessResult.passed ? '#22c55e' : '#ef4444' }]}>
              <View style={styles.resultHeader}>
                <Ionicons name={livenessResult.passed ? 'checkmark-circle' : 'close-circle'} size={24} color={livenessResult.passed ? '#22c55e' : '#ef4444'} />
                <Text style={styles.resultTitle}>Liveness: {livenessResult.passed ? 'PASSED' : 'FAILED'}</Text>
              </View>
              <View style={styles.resultRow}>
                <Text style={styles.resultLabel}>Confidence</Text>
                <Text style={styles.resultValue}>{(livenessResult.confidence * 100).toFixed(1)}%</Text>
              </View>
              <View style={styles.resultRow}>
                <Text style={styles.resultLabel}>Anti-Spoofing</Text>
                <Text style={styles.resultValue}>{(livenessResult.anti_spoofing_score * 100).toFixed(1)}%</Text>
              </View>
              {livenessResult.checks.map((c, i) => (
                <View key={i} style={styles.checkRow}>
                  <Ionicons name={c.passed ? 'checkmark' : 'close'} size={14} color={c.passed ? '#22c55e' : '#ef4444'} />
                  <Text style={styles.checkText}>{c.name}</Text>
                </View>
              ))}
            </View>
          )}

          {kycResult && (
            <View style={[styles.resultCard, { borderLeftColor: kycResult.status === 'verified' ? '#22c55e' : kycResult.status === 'pending_review' ? '#f59e0b' : '#ef4444' }]}>
              <View style={styles.resultHeader}>
                <Ionicons
                  name={kycResult.status === 'verified' ? 'checkmark-circle' : kycResult.status === 'pending_review' ? 'time' : 'close-circle'}
                  size={24}
                  color={kycResult.status === 'verified' ? '#22c55e' : kycResult.status === 'pending_review' ? '#f59e0b' : '#ef4444'}
                />
                <Text style={styles.resultTitle}>KYC: {kycResult.status.replace(/_/g, ' ').toUpperCase()}</Text>
              </View>
              <View style={styles.resultRow}>
                <Text style={styles.resultLabel}>Face Match</Text>
                <Text style={styles.resultValue}>{(kycResult.face_match_score * 100).toFixed(1)}%</Text>
              </View>
              <View style={styles.resultRow}>
                <Text style={styles.resultLabel}>Identity Match</Text>
                <Text style={styles.resultValue}>{(kycResult.identity_match_score * 100).toFixed(1)}%</Text>
              </View>
              <View style={styles.resultRow}>
                <Text style={styles.resultLabel}>Risk Score</Text>
                <Text style={[styles.resultValue, { color: kycResult.risk_score > 0.5 ? '#ef4444' : '#22c55e' }]}>
                  {(kycResult.risk_score * 100).toFixed(0)}%
                </Text>
              </View>
              {kycResult.flags.length > 0 && (
                <View style={styles.flagsContainer}>
                  {kycResult.flags.map((f, i) => (
                    <View key={i} style={styles.flagBadge}>
                      <Text style={styles.flagText}>{f}</Text>
                    </View>
                  ))}
                </View>
              )}
            </View>
          )}

          <TouchableOpacity style={styles.retryButton} onPress={() => { setIdPhoto(null); setSelfiePhoto(null); setKycResult(null); setLivenessResult(null); goToStep('start'); }}>
            <Ionicons name="refresh" size={18} color="#166534" />
            <Text style={styles.retryButtonText}>Start Over</Text>
          </TouchableOpacity>
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  progressContainer: { padding: 16, backgroundColor: '#fff', borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
  progressTrack: { height: 4, backgroundColor: '#e5e7eb', borderRadius: 2, overflow: 'hidden' },
  progressFill: { height: '100%', backgroundColor: '#166534', borderRadius: 2 },
  stepsRow: { flexDirection: 'row', justifyContent: 'space-between', marginTop: 8 },
  stepItem: { alignItems: 'center', flex: 1 },
  stepCircle: { width: 28, height: 28, borderRadius: 14, backgroundColor: '#f3f4f6', justifyContent: 'center', alignItems: 'center' },
  stepCircleActive: { backgroundColor: '#166534' },
  stepLabel: { fontSize: 9, color: '#9ca3af', marginTop: 4, fontWeight: '500' },
  stepLabelActive: { color: '#166534', fontWeight: '700' },
  startCard: { margin: 16, backgroundColor: '#fff', borderRadius: 20, padding: 24, alignItems: 'center', shadowColor: '#000', shadowOffset: { width: 0, height: 2 }, shadowOpacity: 0.06, shadowRadius: 8, elevation: 3 },
  startIconCircle: { width: 80, height: 80, borderRadius: 40, backgroundColor: '#dcfce7', justifyContent: 'center', alignItems: 'center', marginBottom: 16 },
  startTitle: { fontSize: 22, fontWeight: '800', color: '#111827', marginBottom: 8 },
  startSubtitle: { fontSize: 14, color: '#6b7280', textAlign: 'center', lineHeight: 20, marginBottom: 20 },
  requirementsList: { width: '100%', gap: 12, marginBottom: 24 },
  requirementItem: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  requirementText: { fontSize: 14, color: '#374151', flex: 1 },
  primaryButton: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8, backgroundColor: '#166534', paddingVertical: 14, paddingHorizontal: 28, borderRadius: 14, width: '100%' },
  primaryButtonText: { fontSize: 16, fontWeight: '700', color: '#fff' },
  permissionCard: { flex: 1, justifyContent: 'center', alignItems: 'center', padding: 32, gap: 16 },
  permissionTitle: { fontSize: 20, fontWeight: '700', color: '#111827' },
  permissionText: { fontSize: 14, color: '#6b7280', textAlign: 'center' },
  cameraContainer: { flex: 1 },
  camera: { flex: 1 },
  cameraOverlay: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  cameraGuide: { width: 280, height: 180, position: 'relative' },
  corner: { position: 'absolute', width: 24, height: 24, borderColor: '#fff' },
  topLeft: { top: 0, left: 0, borderTopWidth: 3, borderLeftWidth: 3, borderTopLeftRadius: 8 },
  topRight: { top: 0, right: 0, borderTopWidth: 3, borderRightWidth: 3, borderTopRightRadius: 8 },
  bottomLeft: { bottom: 0, left: 0, borderBottomWidth: 3, borderLeftWidth: 3, borderBottomLeftRadius: 8 },
  bottomRight: { bottom: 0, right: 0, borderBottomWidth: 3, borderRightWidth: 3, borderBottomRightRadius: 8 },
  cameraHint: { color: '#fff', fontSize: 14, fontWeight: '600', marginTop: 16, textShadowColor: '#000', textShadowOffset: { width: 0, height: 1 }, textShadowRadius: 4 },
  cameraControls: { position: 'absolute', bottom: 40, left: 0, right: 0, alignItems: 'center' },
  captureButton: { width: 72, height: 72, borderRadius: 36, backgroundColor: 'rgba(255,255,255,0.3)', justifyContent: 'center', alignItems: 'center', borderWidth: 3, borderColor: '#fff' },
  captureInner: { width: 56, height: 56, borderRadius: 28, backgroundColor: '#fff' },
  previewImage: { width: 200, height: 200, borderRadius: 100, marginVertical: 16, borderWidth: 3, borderColor: '#7c3aed' },
  processingCard: { margin: 16, backgroundColor: '#fff', borderRadius: 20, padding: 48, alignItems: 'center', gap: 16 },
  processingTitle: { fontSize: 18, fontWeight: '700', color: '#111827' },
  processingSubtitle: { fontSize: 14, color: '#6b7280' },
  resultCard: { margin: 16, marginBottom: 8, backgroundColor: '#fff', borderRadius: 16, padding: 16, borderLeftWidth: 4, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  resultHeader: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 12 },
  resultTitle: { fontSize: 16, fontWeight: '700', color: '#111827' },
  resultRow: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 6 },
  resultLabel: { fontSize: 14, color: '#6b7280' },
  resultValue: { fontSize: 14, fontWeight: '700', color: '#111827' },
  checkRow: { flexDirection: 'row', alignItems: 'center', gap: 6, paddingVertical: 3 },
  checkText: { fontSize: 13, color: '#374151' },
  flagsContainer: { flexDirection: 'row', flexWrap: 'wrap', gap: 6, marginTop: 8 },
  flagBadge: { backgroundColor: '#fef2f2', paddingHorizontal: 8, paddingVertical: 4, borderRadius: 8 },
  flagText: { fontSize: 11, color: '#dc2626', fontWeight: '600' },
  retryButton: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8, margin: 16, padding: 14, borderRadius: 14, borderWidth: 1.5, borderColor: '#166534' },
  retryButtonText: { fontSize: 15, fontWeight: '600', color: '#166534' },
});
