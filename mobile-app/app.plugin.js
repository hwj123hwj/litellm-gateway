const { withAndroidManifest } = require('expo/config-plugins')

/**
 * Expo Config Plugin: 允许 HTTP 明文流量
 *
 * 问题：`expo prebuild` 会重新生成 android/ 目录，
 * 手动修改 AndroidManifest.xml 和 network_security_config.xml 会被覆盖。
 *
 * 方案：通过 config plugin 在 prebuild 时自动注入以下配置：
 * 1. AndroidManifest.xml <application> 标签添加 android:usesCleartextTraffic="true"
 * 2. AndroidManifest.xml <application> 标签添加 android:networkSecurityConfig 引用
 * 3. 生成 res/xml/network_security_config.xml
 */
const NETWORK_SECURITY_CONFIG = `<?xml version="1.0" encoding="utf-8"?>
<network-security-config>
    <base-config cleartextTrafficPermitted="true">
        <trust-anchors>
            <certificates src="system" />
        </trust-anchors>
    </base-config>
</network-security-config>`

function withCleartextTraffic(config) {
  // 1. 修改 AndroidManifest.xml
  config = withAndroidManifest(config, (modConfig) => {
    const manifest = modConfig.modResults
    const application = manifest.manifest.application

    if (application && application.length > 0) {
      const app = application[0]
      const attr = app.$ || {}

      attr['android:usesCleartextTraffic'] = 'true'
      attr['android:networkSecurityConfig'] = '@xml/network_security_config'

      app.$ = attr
    }

    return modConfig
  })

  // 2. 生成 network_security_config.xml (通过 AndroidResources mod)
  // expo config-plugins 没有直接的 xml resource modifier，
  // 但我们可以通过 mod 里写文件
  const { withDangerousMod } = require('expo/config-plugins')
  const fs = require('fs')
  const path = require('path')

  config = withDangerousMod(config, [
    'android',
    (modConfig) => {
      const projectRoot = modConfig.projectRoot
      const resDir = path.join(
        projectRoot,
        'android',
        'app',
        'src',
        'main',
        'res',
        'xml',
      )
      fs.mkdirSync(resDir, { recursive: true })
      fs.writeFileSync(
        path.join(resDir, 'network_security_config.xml'),
        NETWORK_SECURITY_CONFIG,
      )
      return modConfig
    },
  ])

  return config
}

module.exports = withCleartextTraffic
