import { useEffect, useState } from 'react';
import { View, Text, StyleSheet, Animated } from 'react-native';
import NetInfo from '@react-native-community/netinfo';
import { Ionicons } from '@expo/vector-icons';

export function NetworkBanner() {
  const [isConnected, setIsConnected] = useState(true);
  const [showReconnect, setShowReconnect] = useState(false);
  const slideAnim = useState(new Animated.Value(-50))[0];

  useEffect(() => {
    const unsubscribe = NetInfo.addEventListener((state) => {
      const connected = state.isConnected ?? true;
      if (connected && !isConnected) {
        setShowReconnect(true);
        setTimeout(() => setShowReconnect(false), 3000);
      }
      setIsConnected(connected);
    });
    return () => unsubscribe();
  }, [isConnected]);

  useEffect(() => {
    const visible = !isConnected || showReconnect;
    Animated.spring(slideAnim, {
      toValue: visible ? 0 : -50,
      useNativeDriver: true,
      tension: 120,
      friction: 14,
    }).start();
  }, [isConnected, showReconnect, slideAnim]);

  if (isConnected && !showReconnect) return null;

  return (
    <Animated.View
      style={[
        styles.banner,
        { backgroundColor: isConnected ? '#22c55e' : '#3f3f46', transform: [{ translateY: slideAnim }] },
      ]}
    >
      <Ionicons
        name={isConnected ? 'wifi' : 'cloud-offline'}
        size={16}
        color="#fff"
      />
      <Text style={styles.text}>
        {isConnected ? 'Back online — syncing data' : 'No internet connection'}
      </Text>
    </Animated.View>
  );
}

const styles = StyleSheet.create({
  banner: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    paddingVertical: 8,
    paddingHorizontal: 16,
  },
  text: {
    color: '#fff',
    fontSize: 13,
    fontWeight: '500',
  },
});
