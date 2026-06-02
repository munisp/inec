import { useState, useCallback, useRef } from 'react';
import {
  View, Text, TextInput, StyleSheet, ScrollView, TouchableOpacity,
  RefreshControl, Image, Alert, Platform, Animated,
} from 'react-native';
import { CameraView, useCameraPermissions } from 'expo-camera';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect } from 'expo-router';
import { observerApi, ObserverReport } from '../../src/lib/api';
import { getPendingReports, savePendingReport, PendingReport } from '../../src/lib/offline';
import { EmptyState } from '../../src/components/EmptyState';
import { FeedSkeleton } from '../../src/components/SkeletonLoader';

export default function ReportsScreen() {
  const [permission, requestPermission] = useCameraPermissions();
  const [showCamera, setShowCamera] = useState(false);
  const [photo, setPhoto] = useState<string | null>(null);
  const [puCode, setPuCode] = useState('');
  const [description, setDescription] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [reports, setReports] = useState<ObserverReport[]>([]);
  const [pendingReports, setPendingReports] = useState<PendingReport[]>([]);
  const [refreshing, setRefreshing] = useState(false);
  const [loading, setLoading] = useState(true);
  const cameraRef = useRef<CameraView>(null);
  const buttonScale = useRef(new Animated.Value(1)).current;

  const loadReports = useCallback(async () => {
    try {
      const data = await observerApi.reports();
      setReports(data);
    } catch { /* ignore */ }
    const pending = await getPendingReports();
    setPendingReports(pending);
    setLoading(false);
  }, []);

  useFocusEffect(useCallback(() => { loadReports(); }, [loadReports]));

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await loadReports();
    setRefreshing(false);
  }, [loadReports]);

  const takePhoto = async () => {
    if (!cameraRef.current) return;
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Heavy);

    Animated.sequence([
      Animated.timing(buttonScale, { toValue: 0.85, duration: 50, useNativeDriver: true }),
      Animated.timing(buttonScale, { toValue: 1, duration: 100, useNativeDriver: true }),
    ]).start();

    const result = await cameraRef.current.takePictureAsync({ quality: 0.8 });
    if (result) {
      setPhoto(result.uri);
      setShowCamera(false);
    }
  };

  const submit = async () => {
    if (!puCode.trim()) {
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      Alert.alert('Required', 'Enter the Polling Unit code');
      return;
    }

    setSubmitting(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);

    try {
      const form = new FormData();
      form.append('polling_unit_code', puCode);
      form.append('notes', description || '');
      if (photo) {
        const filename = photo.split('/').pop() || 'photo.jpg';
        form.append('photo', { uri: photo, name: filename, type: 'image/jpeg' } as unknown as Blob);
      }
      await observerApi.submitReport(form);
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
      setPuCode('');
      setDescription('');
      setPhoto(null);
      loadReports();
    } catch {
      await savePendingReport(puCode, description, photo || '');
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Warning);
      const pending = await getPendingReports();
      setPendingReports(pending);
    } finally {
      setSubmitting(false);
    }
  };

  if (showCamera) {
    if (!permission?.granted) {
      return (
        <View style={styles.cameraPermission}>
          <View style={styles.permissionCard}>
            <View style={styles.permissionIcon}>
              <Ionicons name="camera" size={32} color="#166534" />
            </View>
            <Text style={styles.permissionTitle}>Camera Access Needed</Text>
            <Text style={styles.permissionDesc}>
              Take photos of EC8A result sheets for verification and evidence
            </Text>
            <TouchableOpacity style={styles.permissionButton} onPress={requestPermission} activeOpacity={0.8}>
              <Text style={styles.permissionButtonText}>Grant Camera Access</Text>
            </TouchableOpacity>
            <TouchableOpacity onPress={() => setShowCamera(false)}>
              <Text style={styles.cancelText}>Cancel</Text>
            </TouchableOpacity>
          </View>
        </View>
      );
    }

    return (
      <View style={styles.cameraContainer}>
        <CameraView ref={cameraRef} style={styles.camera}>
          <View style={styles.cameraOverlay}>
            <View style={styles.cameraTopBar}>
              <TouchableOpacity onPress={() => setShowCamera(false)} style={styles.cameraCloseBtn}>
                <Ionicons name="close" size={24} color="#fff" />
              </TouchableOpacity>
              <Text style={styles.cameraHint}>Align EC8A form within frame</Text>
            </View>
            <View style={styles.cameraFrame}>
              <View style={[styles.corner, { top: 0, left: 0 }]} />
              <View style={[styles.corner, { top: 0, right: 0, transform: [{ scaleX: -1 }] }]} />
              <View style={[styles.corner, { bottom: 0, left: 0, transform: [{ scaleY: -1 }] }]} />
              <View style={[styles.corner, { bottom: 0, right: 0, transform: [{ scale: -1 }] }]} />
            </View>
            <Animated.View style={[styles.captureButtonOuter, { transform: [{ scale: buttonScale }] }]}>
              <TouchableOpacity style={styles.captureButton} onPress={takePhoto} activeOpacity={0.7} />
            </Animated.View>
          </View>
        </CameraView>
      </View>
    );
  }

  return (
    <ScrollView
      style={styles.container}
      contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} colors={['#166534']} tintColor="#166534" />}
      showsVerticalScrollIndicator={false}
    >
      <View style={styles.formCard}>
        <Text style={styles.formTitle}>New Report</Text>

        {photo && (
          <View style={styles.photoPreview}>
            <Image source={{ uri: photo }} style={styles.previewImage} />
            <TouchableOpacity style={styles.removePhoto} onPress={() => { setPhoto(null); Haptics.selectionAsync(); }}>
              <Ionicons name="close-circle" size={24} color="#dc2626" />
            </TouchableOpacity>
          </View>
        )}

        <TouchableOpacity style={styles.cameraButton} onPress={() => setShowCamera(true)} activeOpacity={0.7}>
          <View style={styles.cameraButtonIcon}>
            <Ionicons name="camera" size={20} color="#166534" />
          </View>
          <View style={{ flex: 1 }}>
            <Text style={styles.cameraButtonText}>{photo ? 'Retake Photo' : 'Take Photo of EC8A Form'}</Text>
            <Text style={styles.cameraButtonHint}>Photograph the result sheet for verification</Text>
          </View>
          <Ionicons name="chevron-forward" size={18} color="#9ca3af" />
        </TouchableOpacity>

        <View style={styles.inputGroup}>
          <Text style={styles.label}>Polling Unit Code</Text>
          <View style={styles.inputWrapper}>
            <Ionicons name="location-outline" size={16} color="#9ca3af" style={{ marginLeft: 12 }} />
            <TextInput
              style={styles.input}
              placeholder="e.g. PU-23-014-001"
              placeholderTextColor="#9ca3af"
              value={puCode}
              onChangeText={setPuCode}
              autoCapitalize="characters"
            />
          </View>
        </View>

        <View style={styles.inputGroup}>
          <Text style={styles.label}>Description (optional)</Text>
          <TextInput
            style={styles.textArea}
            placeholder="Any observations about the results or process..."
            placeholderTextColor="#9ca3af"
            value={description}
            onChangeText={setDescription}
            multiline
            numberOfLines={3}
          />
        </View>

        <TouchableOpacity
          style={[styles.submitButton, submitting && styles.submitDisabled]}
          onPress={submit}
          disabled={submitting}
          activeOpacity={0.8}
        >
          <Ionicons name={submitting ? 'hourglass-outline' : 'cloud-upload'} size={18} color="#fff" />
          <Text style={styles.submitText}>{submitting ? 'Submitting...' : 'Submit Report'}</Text>
        </TouchableOpacity>
      </View>

      {pendingReports.length > 0 && (
        <View style={styles.section}>
          <View style={styles.sectionHeader}>
            <View style={styles.pendingBadge}>
              <Ionicons name="cloud-upload-outline" size={14} color="#92400e" />
              <Text style={styles.pendingText}>{pendingReports.length} Pending Upload</Text>
            </View>
          </View>
          {pendingReports.map((r) => (
            <View key={r.id} style={styles.pendingCard}>
              <Ionicons name="time-outline" size={18} color="#d97706" />
              <View style={{ flex: 1 }}>
                <Text style={styles.pendingPU}>{r.polling_unit_code}</Text>
                <Text style={styles.pendingDesc}>{r.description || 'No description'}</Text>
              </View>
            </View>
          ))}
        </View>
      )}

      <View style={styles.section}>
        <Text style={styles.sectionTitle}>Submitted Reports</Text>
        {loading ? <FeedSkeleton /> : reports.length > 0 ? (
          reports.map((r) => (
            <View key={r.id} style={styles.reportCard}>
              <View style={[styles.reportStatus, { backgroundColor: r.status === 'approved' ? '#dcfce7' : r.status === 'rejected' ? '#fef2f2' : '#fef3c7' }]}>
                <Ionicons
                  name={r.status === 'approved' ? 'checkmark-circle' : r.status === 'rejected' ? 'close-circle' : 'time'}
                  size={16}
                  color={r.status === 'approved' ? '#166534' : r.status === 'rejected' ? '#dc2626' : '#d97706'}
                />
              </View>
              <View style={{ flex: 1 }}>
                <Text style={styles.reportPU}>{r.polling_unit_code}</Text>
                <Text style={styles.reportDesc} numberOfLines={1}>{r.description || r.status}</Text>
              </View>
              <Text style={styles.reportTime}>{new Date(r.created_at).toLocaleDateString()}</Text>
            </View>
          ))
        ) : (
          <EmptyState
            icon="document-text-outline"
            title="No reports yet"
            description="Submit your first observer report to help verify election results"
          />
        )}
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  formCard: {
    backgroundColor: '#fff',
    margin: 12,
    padding: 16,
    borderRadius: 16,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.06,
    shadowRadius: 8,
    elevation: 2,
  },
  formTitle: { fontSize: 18, fontWeight: '700', color: '#111827', marginBottom: 16 },
  photoPreview: { position: 'relative', marginBottom: 12 },
  previewImage: { width: '100%', height: 180, borderRadius: 12 },
  removePhoto: { position: 'absolute', top: 8, right: 8 },
  cameraButton: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    padding: 14,
    backgroundColor: '#f0fdf4',
    borderRadius: 12,
    borderWidth: 1,
    borderColor: '#dcfce7',
    borderStyle: 'dashed',
    marginBottom: 14,
  },
  cameraButtonIcon: {
    width: 40,
    height: 40,
    borderRadius: 12,
    backgroundColor: '#dcfce7',
    alignItems: 'center',
    justifyContent: 'center',
  },
  cameraButtonText: { fontSize: 14, fontWeight: '600', color: '#166534' },
  cameraButtonHint: { fontSize: 11, color: '#6b7280', marginTop: 2 },
  inputGroup: { marginBottom: 14 },
  label: { fontSize: 13, fontWeight: '600', color: '#374151', marginBottom: 6 },
  inputWrapper: {
    flexDirection: 'row',
    alignItems: 'center',
    borderWidth: 1,
    borderColor: '#e5e7eb',
    borderRadius: 12,
    backgroundColor: '#f9fafb',
  },
  input: { flex: 1, padding: 12, fontSize: 15, color: '#111827' },
  textArea: {
    borderWidth: 1,
    borderColor: '#e5e7eb',
    borderRadius: 12,
    backgroundColor: '#f9fafb',
    padding: 12,
    fontSize: 15,
    color: '#111827',
    minHeight: 80,
    textAlignVertical: 'top',
  },
  submitButton: {
    backgroundColor: '#166534',
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    paddingVertical: 14,
    borderRadius: 12,
    shadowColor: '#166534',
    shadowOffset: { width: 0, height: 3 },
    shadowOpacity: 0.2,
    shadowRadius: 6,
    elevation: 3,
  },
  submitDisabled: { opacity: 0.6 },
  submitText: { color: '#fff', fontSize: 16, fontWeight: '700' },
  section: { paddingHorizontal: 12, marginBottom: 12 },
  sectionHeader: { marginBottom: 8 },
  sectionTitle: { fontSize: 16, fontWeight: '700', color: '#111827', marginBottom: 10 },
  pendingBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    alignSelf: 'flex-start',
    gap: 6,
    backgroundColor: '#fef3c7',
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 12,
  },
  pendingText: { fontSize: 13, fontWeight: '600', color: '#92400e' },
  pendingCard: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    backgroundColor: '#fff',
    padding: 14,
    borderRadius: 12,
    marginBottom: 6,
    borderLeftWidth: 3,
    borderLeftColor: '#fbbf24',
  },
  pendingPU: { fontSize: 14, fontWeight: '600', color: '#111827' },
  pendingDesc: { fontSize: 12, color: '#6b7280', marginTop: 2 },
  reportCard: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    backgroundColor: '#fff',
    padding: 14,
    borderRadius: 12,
    marginBottom: 6,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.03,
    shadowRadius: 2,
    elevation: 1,
  },
  reportStatus: {
    width: 36,
    height: 36,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
  },
  reportPU: { fontSize: 14, fontWeight: '600', color: '#111827' },
  reportDesc: { fontSize: 12, color: '#6b7280', marginTop: 2 },
  reportTime: { fontSize: 11, color: '#9ca3af' },
  cameraPermission: {
    flex: 1,
    backgroundColor: '#f9fafb',
    justifyContent: 'center',
    alignItems: 'center',
    padding: 24,
  },
  permissionCard: {
    backgroundColor: '#fff',
    borderRadius: 20,
    padding: 24,
    alignItems: 'center',
    width: '100%',
    maxWidth: 340,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 4 },
    shadowOpacity: 0.1,
    shadowRadius: 12,
    elevation: 4,
  },
  permissionIcon: {
    width: 64,
    height: 64,
    borderRadius: 20,
    backgroundColor: '#f0fdf4',
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: 16,
  },
  permissionTitle: { fontSize: 18, fontWeight: '700', color: '#111827', marginBottom: 8 },
  permissionDesc: { fontSize: 14, color: '#6b7280', textAlign: 'center', lineHeight: 20, marginBottom: 20 },
  permissionButton: {
    backgroundColor: '#166534',
    paddingVertical: 14,
    paddingHorizontal: 32,
    borderRadius: 12,
    width: '100%',
    alignItems: 'center',
    marginBottom: 12,
  },
  permissionButtonText: { color: '#fff', fontSize: 16, fontWeight: '700' },
  cancelText: { color: '#6b7280', fontSize: 14 },
  cameraContainer: { flex: 1, backgroundColor: '#000' },
  camera: { flex: 1 },
  cameraOverlay: { flex: 1, justifyContent: 'space-between', padding: 24 },
  cameraTopBar: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  cameraCloseBtn: {
    width: 40,
    height: 40,
    borderRadius: 20,
    backgroundColor: 'rgba(0,0,0,0.4)',
    alignItems: 'center',
    justifyContent: 'center',
  },
  cameraHint: {
    color: '#fff',
    fontSize: 14,
    fontWeight: '500',
    backgroundColor: 'rgba(0,0,0,0.3)',
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 8,
  },
  cameraFrame: {
    width: '90%',
    aspectRatio: 0.7,
    alignSelf: 'center',
    position: 'relative',
  },
  corner: {
    position: 'absolute',
    width: 24,
    height: 24,
    borderTopWidth: 3,
    borderLeftWidth: 3,
    borderColor: '#fff',
    borderTopLeftRadius: 4,
  },
  captureButtonOuter: {
    alignSelf: 'center',
    width: 72,
    height: 72,
    borderRadius: 36,
    backgroundColor: 'rgba(255,255,255,0.3)',
    alignItems: 'center',
    justifyContent: 'center',
  },
  captureButton: {
    width: 60,
    height: 60,
    borderRadius: 30,
    backgroundColor: '#fff',
    borderWidth: 3,
    borderColor: 'rgba(255,255,255,0.5)',
  },
});
