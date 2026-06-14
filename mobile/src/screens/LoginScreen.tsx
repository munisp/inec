import React, { useState, useEffect } from 'react';
import {
  View, Text, TextInput, TouchableOpacity, StyleSheet, Alert,
  KeyboardAvoidingView, Platform, Image, ActivityIndicator,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import {
  login, User, isBiometricAvailable, authenticateWithBiometric,
  isBiometricEnabled, getStoredUser,
} from '../lib/auth';

interface Props {
  onLogin: (user: User) => void;
}

export default function LoginScreen({ onLogin }: Props) {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  const [biometricAvailable, setBiometricAvailable] = useState(false);

  useEffect(() => {
    checkBiometric();
  }, []);

  const checkBiometric = async () => {
    const available = await isBiometricAvailable();
    const enabled = await isBiometricEnabled();
    setBiometricAvailable(available && enabled);

    if (available && enabled) {
      const stored = await getStoredUser();
      if (stored) {
        const success = await authenticateWithBiometric();
        if (success) onLogin(stored);
      }
    }
  };

  const handleLogin = async () => {
    if (!email.trim() || !password.trim()) {
      Alert.alert('Error', 'Please enter email and password');
      return;
    }
    setLoading(true);
    try {
      const user = await login(email, password);
      onLogin(user);
    } catch (err: any) {
      Alert.alert('Login Failed', err.message || 'Invalid credentials');
    } finally {
      setLoading(false);
    }
  };

  const handleBiometricLogin = async () => {
    const success = await authenticateWithBiometric();
    if (success) {
      const stored = await getStoredUser();
      if (stored) onLogin(stored);
      else Alert.alert('Error', 'No stored credentials. Please login with email first.');
    }
  };

  return (
    <KeyboardAvoidingView style={s.container} behavior={Platform.OS === 'ios' ? 'padding' : 'height'}>
      <View style={s.inner}>
        <View style={s.logoContainer}>
          <View style={s.logoCircle}>
            <Ionicons name="shield-checkmark" size={48} color="#fff" />
          </View>
          <Text style={s.title}>INEC Platform</Text>
          <Text style={s.subtitle}>Election Management & GOTV</Text>
        </View>

        <View style={s.form}>
          <View style={s.inputContainer}>
            <Ionicons name="mail-outline" size={20} color="#94a3b8" style={s.inputIcon} />
            <TextInput
              style={s.input}
              placeholder="Email address"
              placeholderTextColor="#94a3b8"
              value={email}
              onChangeText={setEmail}
              keyboardType="email-address"
              autoCapitalize="none"
              autoComplete="email"
            />
          </View>

          <View style={s.inputContainer}>
            <Ionicons name="lock-closed-outline" size={20} color="#94a3b8" style={s.inputIcon} />
            <TextInput
              style={s.input}
              placeholder="Password"
              placeholderTextColor="#94a3b8"
              value={password}
              onChangeText={setPassword}
              secureTextEntry={!showPassword}
              autoComplete="password"
            />
            <TouchableOpacity onPress={() => setShowPassword(!showPassword)} style={s.eyeIcon}>
              <Ionicons name={showPassword ? 'eye-off-outline' : 'eye-outline'} size={20} color="#94a3b8" />
            </TouchableOpacity>
          </View>

          <TouchableOpacity style={s.loginButton} onPress={handleLogin} disabled={loading}>
            {loading ? (
              <ActivityIndicator color="#fff" />
            ) : (
              <Text style={s.loginButtonText}>Sign In</Text>
            )}
          </TouchableOpacity>

          {biometricAvailable && (
            <TouchableOpacity style={s.biometricButton} onPress={handleBiometricLogin}>
              <Ionicons name="finger-print" size={24} color="#15803d" />
              <Text style={s.biometricText}>Sign in with Biometric</Text>
            </TouchableOpacity>
          )}
        </View>

        <Text style={s.footer}>Independent National Electoral Commission</Text>
      </View>
    </KeyboardAvoidingView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  inner: { flex: 1, justifyContent: 'center', padding: 24 },
  logoContainer: { alignItems: 'center', marginBottom: 48 },
  logoCircle: { width: 96, height: 96, borderRadius: 48, backgroundColor: '#15803d', justifyContent: 'center', alignItems: 'center', marginBottom: 16, elevation: 4 },
  title: { fontSize: 28, fontWeight: '700', color: '#1e293b' },
  subtitle: { fontSize: 14, color: '#64748b', marginTop: 4 },
  form: { gap: 16 },
  inputContainer: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#e2e8f0', paddingHorizontal: 16, height: 52 },
  inputIcon: { marginRight: 12 },
  input: { flex: 1, fontSize: 16, color: '#1e293b' },
  eyeIcon: { padding: 4 },
  loginButton: { backgroundColor: '#15803d', borderRadius: 12, height: 52, justifyContent: 'center', alignItems: 'center', marginTop: 8 },
  loginButtonText: { color: '#fff', fontSize: 16, fontWeight: '600' },
  biometricButton: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8, paddingVertical: 16 },
  biometricText: { fontSize: 14, color: '#15803d', fontWeight: '500' },
  footer: { textAlign: 'center', fontSize: 12, color: '#94a3b8', marginTop: 48 },
});
