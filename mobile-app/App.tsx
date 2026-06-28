import { useEffect, useState, useCallback } from 'react'
import { StatusBar } from 'expo-status-bar'
import {
  View,
  Text,
  TextInput,
  TouchableOpacity,
  StyleSheet,
  KeyboardAvoidingView,
  Platform,
  ScrollView,
} from 'react-native'
import { NavigationContainer } from '@react-navigation/native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { useStore } from './src/store'
import { TabNavigator } from './src/navigation'
import { Colors, Typography, Spacing, Radius, Shadow } from './src/theme'

function SetupScreen() {
  const { setBackendUrl, setApiKey, backendUrl } = useStore()
  const [url, setUrl] = useState('http://')
  const [key, setKey] = useState('')
  const [saving, setSaving] = useState(false)

  const handleStart = useCallback(async () => {
    setSaving(true)
    try {
      if (url.trim()) {
        await setBackendUrl(url.trim())
      }
      if (key.trim()) {
        await setApiKey(key.trim())
      }
    } finally {
      setSaving(false)
    }
  }, [url, key, setBackendUrl, setApiKey])

  return (
    <KeyboardAvoidingView
      style={styles.setupContainer}
      behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
    >
      <ScrollView
        contentContainerStyle={styles.setupScroll}
        keyboardShouldPersistTaps="handled"
      >
        <View style={styles.setupCard}>
          <Text style={styles.setupIcon}>🚀</Text>
          <Text style={styles.setupTitle}>LiteLLM Admin</Text>
          <Text style={styles.setupSubtitle}>
            首次使用，请配置 Gateway 地址
          </Text>

          <View style={styles.setupInputGroup}>
            <Text style={styles.setupLabel}>🌐 后端地址</Text>
            <TextInput
              style={styles.setupInput}
              placeholder="http://your-server:4001"
              placeholderTextColor={Colors.textMuted}
              value={url}
              onChangeText={setUrl}
              autoCapitalize="none"
              autoCorrect={false}
              keyboardType="url"
            />
          </View>

          <View style={styles.setupInputGroup}>
            <Text style={styles.setupLabel}>🔑 API Key</Text>
            <TextInput
              style={styles.setupInput}
              placeholder="输入 API Key（可选）"
              placeholderTextColor={Colors.textMuted}
              value={key}
              onChangeText={setKey}
              secureTextEntry
              autoCapitalize="none"
              autoCorrect={false}
            />
          </View>

          <TouchableOpacity
            style={[styles.setupButton, saving && styles.setupButtonDisabled]}
            onPress={handleStart}
            disabled={saving}
            activeOpacity={0.8}
          >
            <Text style={styles.setupButtonText}>
              {saving ? '保存中...' : '开始使用'}
            </Text>
          </TouchableOpacity>
        </View>
      </ScrollView>
    </KeyboardAvoidingView>
  )
}

export default function App() {
  const { initialized, init, backendUrl, apiKey, fetchHealth } = useStore()
  const [showSetup, setShowSetup] = useState(false)

  useEffect(() => {
    init()
  }, [init])

  useEffect(() => {
    if (!initialized) return
    // 如果没有配置后端地址，显示设置引导
    if (!backendUrl) {
      setShowSetup(true)
    } else {
      setShowSetup(false)
    }
  }, [initialized, backendUrl])

  // 定期刷新 health
  useEffect(() => {
    if (!apiKey || !backendUrl) return
    fetchHealth()
    const timer = setInterval(fetchHealth, 15000)
    return () => clearInterval(timer)
  }, [apiKey, backendUrl, fetchHealth])

  if (!initialized) {
    return (
      <View style={styles.loadingContainer}>
        <Text style={styles.loadingText}>加载中...</Text>
        <StatusBar style="dark" />
      </View>
    )
  }

  if (showSetup) {
    return (
      <SafeAreaProvider>
        <SetupScreen />
        <StatusBar style="dark" />
      </SafeAreaProvider>
    )
  }

  return (
    <SafeAreaProvider>
      <NavigationContainer>
        <TabNavigator />
        <StatusBar style="dark" />
      </NavigationContainer>
    </SafeAreaProvider>
  )
}

const styles = StyleSheet.create({
  loadingContainer: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: Colors.background,
  },
  loadingText: {
    fontSize: Typography.fontSize.md,
    color: Colors.textMuted,
    fontFamily: Typography.fontFamily.body,
  },
  // Setup
  setupContainer: {
    flex: 1,
    backgroundColor: Colors.background,
  },
  setupScroll: {
    flexGrow: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: Spacing[5],
  },
  setupCard: {
    backgroundColor: '#fff',
    borderRadius: Radius.xl,
    padding: Spacing[10],
    maxWidth: 400,
    width: '100%',
    alignItems: 'center',
    ...Shadow.cardHover,
  },
  setupIcon: {
    fontSize: 64,
    marginBottom: Spacing[4],
  },
  setupTitle: {
    fontSize: Typography.fontSize['5xl'],
    fontWeight: Typography.fontWeight.bold,
    color: Colors.terracotta[700],
    marginBottom: Spacing[2],
    fontFamily: Typography.fontFamily.heading,
  },
  setupSubtitle: {
    fontSize: Typography.fontSize.md,
    color: Colors.textMuted,
    marginBottom: Spacing[6],
    fontFamily: Typography.fontFamily.body,
  },
  setupInputGroup: {
    width: '100%',
    marginBottom: Spacing[4],
  },
  setupLabel: {
    fontSize: Typography.fontSize.base,
    fontWeight: Typography.fontWeight.semibold,
    color: Colors.text,
    marginBottom: Spacing[2],
    fontFamily: Typography.fontFamily.body,
  },
  setupInput: {
    width: '100%',
    paddingHorizontal: Spacing[3],
    paddingVertical: Spacing[3],
    borderWidth: 1,
    borderColor: Colors.border,
    borderRadius: Radius.sm,
    fontSize: Typography.fontSize.md,
    color: Colors.text,
    backgroundColor: '#fff',
    fontFamily: Typography.fontFamily.body,
  },
  setupButton: {
    width: '100%',
    paddingVertical: Spacing[4],
    backgroundColor: Colors.terracotta[700],
    borderRadius: Radius.md,
    alignItems: 'center',
    marginTop: Spacing[4],
  },
  setupButtonDisabled: {
    opacity: 0.6,
  },
  setupButtonText: {
    color: '#fff',
    fontSize: Typography.fontSize.lg,
    fontWeight: Typography.fontWeight.bold,
    fontFamily: Typography.fontFamily.body,
  },
})
