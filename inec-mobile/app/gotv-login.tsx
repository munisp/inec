// Standalone GOTV Mobile Login — phone+OTP auth (NOT shared with INEC portal).
// Party faithful/canvassers/volunteers authenticate via their phone number
// and party code, completely independent of INEC Keycloak SSO.

import { useState, useRef, useEffect } from 'react';
import {
  View, Text, StyleSheet, TextInput, TouchableOpacity,
  KeyboardAvoidingView, Platform, Alert, ActivityIndicator,
  Animated, Dimensions,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { router } from 'expo-router';
import {
  requestOTP, verifyOTP, isAuthenticated,
  type GOTVUser,
} from '../lib/gotv-auth';

const { width } = Dimensions.get('window');

const PARTY_OPTIONS = [
  { code: 'APC', name: 'All Progressives Congress', color: '#009639' },
  { code: 'PDP', name: "Peoples Democratic Party", color: '#E30A0A' },
  { code: 'LP', name: 'Labour Party', color: '#4C8C2B' },
  { code: 'NNPP', name: 'New Nigeria Peoples Party', color: '#1E3A5F' },
  { code: 'ADC', name: 'African Democratic Congress', color: '#FF6600' },
];

type Step = 'phone' | 'otp' | 'loading';

export default function GOTVLoginScreen() {
  const [step, setStep] = useState<Step>('phone');
  const [phone, setPhone] = useState('');
  const [name, setName] = useState('');
  const [partyCode, setPartyCode] = useState('');
  const [otpCode, setOtpCode] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [countdown, setCountdown] = useState(0);
  const slideAnim = useRef(new Animated.Value(0)).current;
  const otpRefs = useRef<(TextInput | null)[]>([]);

  // Check if already authenticated
  useEffect(() => {
    isAuthenticated().then(auth => {
      if (auth) router.replace('/gotv-canvasser');
    });
  }, []);

  // Countdown timer for OTP resend
  useEffect(() => {
    if (countdown <= 0) return;
    const timer = setTimeout(() => setCountdown(c => c - 1), 1000);
    return () => clearTimeout(timer);
  }, [countdown]);

  const animateToStep = (newStep: Step) => {
    Animated.timing(slideAnim, {
      toValue: newStep === 'otp' ? -width : 0,
      duration: 300,
      useNativeDriver: true,
    }).start();
    setStep(newStep);
  };

  const handleRequestOTP = async () => {
    if (!phone || phone.length < 10) {
      setError('Enter a valid Nigerian phone number');
      return;
    }
    if (!partyCode) {
      setError('Select your party');
      return;
    }

    setLoading(true);
    setError('');
    try {
      await requestOTP(phone, partyCode, name);
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
      setCountdown(120);
      animateToStep('otp');
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to send OTP';
      setError(msg);
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
    } finally {
      setLoading(false);
    }
  };

  const handleVerifyOTP = async () => {
    if (otpCode.length !== 6) {
      setError('Enter the 6-digit OTP code');
      return;
    }

    setLoading(true);
    setError('');
    try {
      const user: GOTVUser = await verifyOTP(phone, partyCode, otpCode);
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
      Alert.alert('Welcome!', `Signed in as ${user.display_name} (${user.role})`);
      router.replace('/gotv-canvasser');
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'OTP verification failed';
      setError(msg);
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
    } finally {
      setLoading(false);
    }
  };

  const handleResendOTP = async () => {
    if (countdown > 0) return;
    setLoading(true);
    try {
      await requestOTP(phone, partyCode, name);
      setCountdown(120);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Resend failed');
    } finally {
      setLoading(false);
    }
  };

  // ─── OTP input with 6 individual boxes ────────────────────────────────
  const handleOTPChange = (text: string, index: number) => {
    const newOTP = otpCode.split('');
    newOTP[index] = text;
    const joined = newOTP.join('').slice(0, 6);
    setOtpCode(joined);

    if (text && index < 5) {
      otpRefs.current[index + 1]?.focus();
    }
  };

  const handleOTPKeyPress = (key: string, index: number) => {
    if (key === 'Backspace' && !otpCode[index] && index > 0) {
      otpRefs.current[index - 1]?.focus();
    }
  };

  return (
    <KeyboardAvoidingView
      style={styles.container}
      behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
    >
      {/* Header */}
      <View style={styles.header}>
        <Ionicons name="megaphone" size={48} color="#009639" />
        <Text style={styles.title}>GOTV Mobile</Text>
        <Text style={styles.subtitle}>Get Out The Vote — Party Canvasser App</Text>
        <Text style={styles.note}>
          This app is for party volunteers and canvassers.{'\n'}
          Not connected to INEC portal accounts.
        </Text>
      </View>

      {error ? (
        <View style={styles.errorBanner}>
          <Ionicons name="alert-circle" size={18} color="#fff" />
          <Text style={styles.errorText}>{error}</Text>
        </View>
      ) : null}

      <Animated.View style={[styles.formContainer, { transform: [{ translateX: slideAnim }] }]}>
        {/* Step 1: Phone + Party */}
        <View style={[styles.formStep, { width }]}>
          <Text style={styles.label}>Your Name</Text>
          <TextInput
            style={styles.input}
            placeholder="Full Name"
            value={name}
            onChangeText={setName}
            autoCapitalize="words"
          />

          <Text style={styles.label}>Phone Number</Text>
          <View style={styles.phoneRow}>
            <Text style={styles.countryCode}>+234</Text>
            <TextInput
              style={[styles.input, styles.phoneInput]}
              placeholder="080 1234 5678"
              keyboardType="phone-pad"
              value={phone}
              onChangeText={setPhone}
              maxLength={11}
            />
          </View>

          <Text style={styles.label}>Select Your Party</Text>
          <View style={styles.partyGrid}>
            {PARTY_OPTIONS.map(p => (
              <TouchableOpacity
                key={p.code}
                style={[
                  styles.partyChip,
                  partyCode === p.code && { backgroundColor: p.color, borderColor: p.color },
                ]}
                onPress={() => { setPartyCode(p.code); setError(''); }}
              >
                <Text style={[
                  styles.partyChipText,
                  partyCode === p.code && { color: '#fff' },
                ]}>
                  {p.code}
                </Text>
              </TouchableOpacity>
            ))}
          </View>

          <TouchableOpacity
            style={[styles.button, loading && styles.buttonDisabled]}
            onPress={handleRequestOTP}
            disabled={loading}
          >
            {loading ? (
              <ActivityIndicator color="#fff" />
            ) : (
              <Text style={styles.buttonText}>Send OTP</Text>
            )}
          </TouchableOpacity>
        </View>

        {/* Step 2: OTP Verification */}
        <View style={[styles.formStep, { width }]}>
          <TouchableOpacity onPress={() => animateToStep('phone')} style={styles.backButton}>
            <Ionicons name="arrow-back" size={24} color="#333" />
            <Text style={styles.backText}>Change Phone</Text>
          </TouchableOpacity>

          <Text style={styles.otpTitle}>Enter Verification Code</Text>
          <Text style={styles.otpSubtitle}>
            Sent to +234 {phone.replace(/(\d{3})(\d{3,4})(\d{4})/, '$1 $2 $3')}
          </Text>

          <View style={styles.otpRow}>
            {[0, 1, 2, 3, 4, 5].map(i => (
              <TextInput
                key={i}
                ref={el => { otpRefs.current[i] = el; }}
                style={[styles.otpBox, otpCode[i] ? styles.otpBoxFilled : null]}
                keyboardType="number-pad"
                maxLength={1}
                value={otpCode[i] ?? ''}
                onChangeText={text => handleOTPChange(text, i)}
                onKeyPress={({ nativeEvent }) => handleOTPKeyPress(nativeEvent.key, i)}
                selectTextOnFocus
              />
            ))}
          </View>

          <TouchableOpacity
            style={[styles.button, loading && styles.buttonDisabled]}
            onPress={handleVerifyOTP}
            disabled={loading || otpCode.length !== 6}
          >
            {loading ? (
              <ActivityIndicator color="#fff" />
            ) : (
              <Text style={styles.buttonText}>Verify & Sign In</Text>
            )}
          </TouchableOpacity>

          <TouchableOpacity
            onPress={handleResendOTP}
            disabled={countdown > 0}
            style={styles.resendButton}
          >
            <Text style={[styles.resendText, countdown > 0 && { color: '#9ca3af' }]}>
              {countdown > 0
                ? `Resend in ${Math.floor(countdown / 60)}:${(countdown % 60).toString().padStart(2, '0')}`
                : 'Resend OTP'
              }
            </Text>
          </TouchableOpacity>
        </View>
      </Animated.View>

      {/* Footer */}
      <View style={styles.footer}>
        <Text style={styles.footerText}>
          Powered by INEC GOTV Platform
        </Text>
        <Text style={styles.footerDisclaimer}>
          Your phone number is encrypted and never shared outside your party.
        </Text>
      </View>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#f8fafc',
  },
  header: {
    alignItems: 'center',
    paddingTop: 60,
    paddingBottom: 20,
    paddingHorizontal: 20,
  },
  title: {
    fontSize: 28,
    fontWeight: '800',
    color: '#1e293b',
    marginTop: 12,
  },
  subtitle: {
    fontSize: 15,
    color: '#64748b',
    marginTop: 4,
  },
  note: {
    fontSize: 12,
    color: '#94a3b8',
    textAlign: 'center',
    marginTop: 8,
    lineHeight: 18,
  },
  errorBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: '#ef4444',
    marginHorizontal: 20,
    padding: 10,
    borderRadius: 8,
    gap: 8,
    marginBottom: 10,
  },
  errorText: {
    color: '#fff',
    fontSize: 14,
    flex: 1,
  },
  formContainer: {
    flexDirection: 'row',
    flex: 1,
  },
  formStep: {
    paddingHorizontal: 24,
  },
  label: {
    fontSize: 14,
    fontWeight: '600',
    color: '#475569',
    marginBottom: 6,
    marginTop: 12,
  },
  input: {
    backgroundColor: '#fff',
    borderWidth: 1,
    borderColor: '#e2e8f0',
    borderRadius: 10,
    paddingHorizontal: 16,
    paddingVertical: 14,
    fontSize: 16,
    color: '#1e293b',
  },
  phoneRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  countryCode: {
    fontSize: 16,
    fontWeight: '600',
    color: '#64748b',
    paddingVertical: 14,
    paddingHorizontal: 12,
    backgroundColor: '#f1f5f9',
    borderRadius: 10,
    borderWidth: 1,
    borderColor: '#e2e8f0',
    overflow: 'hidden',
  },
  phoneInput: {
    flex: 1,
  },
  partyGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 8,
    marginBottom: 20,
  },
  partyChip: {
    paddingHorizontal: 16,
    paddingVertical: 10,
    borderRadius: 20,
    borderWidth: 2,
    borderColor: '#e2e8f0',
    backgroundColor: '#fff',
  },
  partyChipText: {
    fontSize: 14,
    fontWeight: '700',
    color: '#475569',
  },
  button: {
    backgroundColor: '#009639',
    paddingVertical: 16,
    borderRadius: 12,
    alignItems: 'center',
    marginTop: 10,
  },
  buttonDisabled: {
    opacity: 0.6,
  },
  buttonText: {
    color: '#fff',
    fontSize: 17,
    fontWeight: '700',
  },
  backButton: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    marginBottom: 10,
  },
  backText: {
    fontSize: 15,
    color: '#333',
  },
  otpTitle: {
    fontSize: 22,
    fontWeight: '700',
    color: '#1e293b',
    textAlign: 'center',
  },
  otpSubtitle: {
    fontSize: 14,
    color: '#64748b',
    textAlign: 'center',
    marginTop: 6,
    marginBottom: 24,
  },
  otpRow: {
    flexDirection: 'row',
    justifyContent: 'center',
    gap: 10,
    marginBottom: 24,
  },
  otpBox: {
    width: 48,
    height: 56,
    borderWidth: 2,
    borderColor: '#e2e8f0',
    borderRadius: 10,
    textAlign: 'center',
    fontSize: 24,
    fontWeight: '700',
    color: '#1e293b',
    backgroundColor: '#fff',
  },
  otpBoxFilled: {
    borderColor: '#009639',
    backgroundColor: '#f0fdf4',
  },
  resendButton: {
    alignItems: 'center',
    marginTop: 16,
  },
  resendText: {
    fontSize: 15,
    color: '#3b82f6',
    fontWeight: '600',
  },
  footer: {
    alignItems: 'center',
    paddingBottom: 40,
    paddingHorizontal: 20,
  },
  footerText: {
    fontSize: 12,
    color: '#94a3b8',
  },
  footerDisclaimer: {
    fontSize: 11,
    color: '#cbd5e1',
    marginTop: 4,
    textAlign: 'center',
  },
});
