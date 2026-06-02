import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useEffect } from 'react';
import { View } from 'react-native';
import { getDb } from '../src/lib/offline';
import { NetworkBanner } from '../src/components/NetworkBanner';

export default function RootLayout() {
  useEffect(() => {
    getDb();
  }, []);

  return (
    <View style={{ flex: 1 }}>
      <StatusBar style="light" />
      <NetworkBanner />
      <Stack
        screenOptions={{
          headerStyle: { backgroundColor: '#166534' },
          headerTintColor: '#fff',
          headerTitleStyle: { fontWeight: 'bold' },
          animation: 'slide_from_right',
        }}
      >
        <Stack.Screen name="index" options={{ headerShown: false }} />
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
      </Stack>
    </View>
  );
}
