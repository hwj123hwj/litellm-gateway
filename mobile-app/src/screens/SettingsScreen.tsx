import { useState, useCallback, useRef, useEffect } from 'react'
import {
  View,
  Text,
  TextInput,
  TouchableOpacity,
  StyleSheet,
  ScrollView,
  Alert,
} from 'react-native'
import { useStore } from '../store'
import { PageHeader, PageContainer } from '../components'
import { Colors, Typography, Spacing, Radius, Shadow } from '../theme'

export default function SettingsScreen() {
  const backendUrl = useStore((s) => s.backendUrl)
  const setBackendUrl = useStore((s) => s.setBackendUrl)
  const apiKey = useStore((s) => s.apiKey)
  const setApiKey = useStore((s) => s.setApiKey)
  const initialized = useStore((s) => s.initialized)
  const [urlInput, setUrlInput] = useState(backendUrl)
  const [keyInput, setKeyInput] = useState(apiKey)
  const [saved, setSaved] = useState(false)
  const savedTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const handleSaveUrl = useCallback(async () => {
    await setBackendUrl(urlInput)
    setSaved(true)
    // 清理上一次的定时器，避免快速点击产生多个并存的定时器。
    // 组件卸载时未触发的 setState 会通过 effect 的清理函数取消。
    if (savedTimer.current) clearTimeout(savedTimer.current)
    savedTimer.current = setTimeout(() => {
      setSaved(false)
    }, 2000)
  }, [urlInput, setBackendUrl])

  const handleSaveKey = useCallback(async () => {
    await setApiKey(keyInput)
    Alert.alert('已保存', 'API Key 已更新')
  }, [keyInput, setApiKey])

  // 组件卸载时清理定时器，避免 setState on unmounted component
  useEffect(() => {
    return () => {
      if (savedTimer.current) clearTimeout(savedTimer.current)
    }
  }, [])

  return (
    <PageContainer loading={!initialized}>
      <ScrollView
        style={styles.scroll}
        contentContainerStyle={styles.scrollContent}
        showsVerticalScrollIndicator={false}
      >
        <PageHeader title="设置" subtitle="网关配置与管理" />

        {/* 连接配置 */}
        <View style={styles.group}>
          <Text style={styles.groupTitle}>连接配置</Text>

          {/* 后端地址 */}
          <View style={styles.item}>
            <View style={styles.itemHeader}>
              <View style={[styles.icon, { backgroundColor: '#9a3412' }]}>
                <Text style={styles.iconText}>🌐</Text>
              </View>
              <View style={styles.info}>
                <Text style={styles.label}>后端地址</Text>
                <Text style={styles.desc}>Gateway API 地址</Text>
              </View>
            </View>
            <View style={styles.inputRow}>
              <TextInput
                style={styles.input}
                placeholder="http://your-server:4001"
                placeholderTextColor={Colors.textMuted}
                value={urlInput}
                onChangeText={setUrlInput}
                autoCapitalize="none"
                autoCorrect={false}
                keyboardType="url"
              />
              <TouchableOpacity
                style={[styles.saveBtn, saved && styles.saveBtnOk]}
                onPress={handleSaveUrl}
              >
                <Text style={styles.saveBtnText}>
                  {saved ? '✓ 已保存' : '保存'}
                </Text>
              </TouchableOpacity>
            </View>
            {backendUrl ? (
              <Text style={styles.currentValue}>当前: {backendUrl}</Text>
            ) : null}
          </View>

          {/* API Key */}
          <View style={styles.item}>
            <View style={styles.itemHeader}>
              <View style={[styles.icon, { backgroundColor: '#059669' }]}>
                <Text style={styles.iconText}>🔑</Text>
              </View>
              <View style={styles.info}>
                <Text style={styles.label}>API Key</Text>
                <Text style={styles.desc}>LITELLM_MASTER_KEY 或 ADMIN_TOKEN</Text>
              </View>
            </View>
            <TextInput
              style={styles.input}
              placeholder="输入 API Key"
              placeholderTextColor={Colors.textMuted}
              value={keyInput}
              onChangeText={setKeyInput}
              secureTextEntry
              autoCapitalize="none"
              autoCorrect={false}
            />
            <TouchableOpacity style={styles.saveBtn} onPress={handleSaveKey}>
              <Text style={styles.saveBtnText}>保存 Key</Text>
            </TouchableOpacity>
          </View>
        </View>

        {/* 通用 */}
        <View style={styles.group}>
          <Text style={styles.groupTitle}>通用</Text>
          <View style={styles.menuItem}>
            <View style={[styles.icon, { backgroundColor: '#6366f1' }]}>
              <Text style={styles.iconText}>🔌</Text>
            </View>
            <View style={styles.info}>
              <Text style={styles.label}>提供商管理</Text>
              <Text style={styles.desc}>添加、配置、启用/禁用</Text>
            </View>
          </View>
          <View style={styles.menuItem}>
            <View style={[styles.icon, { backgroundColor: '#d97706' }]}>
              <Text style={styles.iconText}>📊</Text>
            </View>
            <View style={styles.info}>
              <Text style={styles.label}>用量与计费</Text>
              <Text style={styles.desc}>Token 统计、请求量报表</Text>
            </View>
          </View>
        </View>

        {/* 关于 */}
        <View style={styles.group}>
          <Text style={styles.groupTitle}>关于</Text>
          <View style={styles.menuItem}>
            <View style={[styles.icon, { backgroundColor: '#7c3aed' }]}>
              <Text style={styles.iconText}>ℹ️</Text>
            </View>
            <View style={styles.info}>
              <Text style={styles.label}>版本信息</Text>
              <Text style={styles.desc}>litellm-gateway · admin v1.0.0</Text>
            </View>
          </View>
        </View>
      </ScrollView>
    </PageContainer>
  )
}

const styles = StyleSheet.create({
  scroll: {
    flex: 1,
  },
  scrollContent: {
    padding: Spacing[4],
    paddingBottom: Spacing[16],
  },
  group: {
    marginBottom: Spacing[6],
  },
  groupTitle: {
    fontSize: Typography.fontSize.sm,
    fontWeight: Typography.fontWeight.semibold,
    color: Colors.textMuted,
    textTransform: 'uppercase',
    letterSpacing: 0.5,
    marginBottom: Spacing[2],
    paddingHorizontal: Spacing[1],
    fontFamily: Typography.fontFamily.body,
  },
  item: {
    backgroundColor: Colors.card,
    borderWidth: 1,
    borderColor: Colors.border,
    borderRadius: Radius.lg,
    padding: Spacing[4],
    gap: Spacing[3],
    ...Shadow.card,
  },
  menuItem: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing[3],
    backgroundColor: Colors.card,
    borderWidth: 1,
    borderColor: Colors.border,
    borderRadius: Radius.lg,
    padding: Spacing[4],
    ...Shadow.card,
  },
  itemHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing[3],
  },
  icon: {
    width: 36,
    height: 36,
    borderRadius: Radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
  iconText: {
    fontSize: 16,
  },
  info: {
    flex: 1,
  },
  label: {
    fontSize: Typography.fontSize.md,
    fontWeight: Typography.fontWeight.semibold,
    color: Colors.text,
    fontFamily: Typography.fontFamily.body,
  },
  desc: {
    fontSize: Typography.fontSize.sm,
    color: Colors.textMuted,
    fontFamily: Typography.fontFamily.body,
  },
  inputRow: {
    flexDirection: 'row',
    gap: Spacing[2],
  },
  input: {
    flex: 1,
    paddingHorizontal: Spacing[3],
    paddingVertical: Spacing[2],
    borderWidth: 1,
    borderColor: Colors.border,
    borderRadius: Radius.sm,
    fontSize: Typography.fontSize.base,
    color: Colors.text,
    backgroundColor: '#fff',
    fontFamily: Typography.fontFamily.body,
  },
  saveBtn: {
    paddingHorizontal: Spacing[4],
    paddingVertical: Spacing[2],
    backgroundColor: Colors.terracotta[700],
    borderRadius: Radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
  saveBtnOk: {
    backgroundColor: Colors.green,
  },
  saveBtnText: {
    color: '#fff',
    fontSize: Typography.fontSize.base,
    fontWeight: Typography.fontWeight.semibold,
    fontFamily: Typography.fontFamily.body,
  },
  currentValue: {
    fontSize: Typography.fontSize.xs,
    color: Colors.textMuted,
    fontFamily: Typography.fontFamily.body,
  },
})
