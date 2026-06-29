const { withAndroidManifest, withDangerousMod } = require('expo/config-plugins')
const fs = require('fs')
const path = require('path')

const NETWORK_SECURITY_CONFIG = `<?xml version="1.0" encoding="utf-8"?>
<network-security-config>
    <base-config cleartextTrafficPermitted="true">
        <trust-anchors>
            <certificates src="system" />
        </trust-anchors>
    </base-config>
</network-security-config>`

/**
 * Expo Config Plugin: 允许 HTTP 明文流量
 *
 * expo prebuild 会重新生成 android/ 目录，手动修改会被覆盖。
 * 此插件在 prebuild 时自动注入配置，确保配置持久化。
 */
function withCleartextTraffic(config) {
  // 1. 修改 AndroidManifest.xml
  config = withAndroidManifest(config, (modConfig) => {
    const application = modConfig.modResults.manifest.application
    if (application && application.length > 0) {
      const attr = application[0].$ || {}
      attr['android:usesCleartextTraffic'] = 'true'
      attr['android:networkSecurityConfig'] = '@xml/network_security_config'
      application[0].$ = attr
    }
    return modConfig
  })

  // 2. 生成 network_security_config.xml
  config = withDangerousMod(config, [
    'android',
    async (modConfig) => {
      const projectRoot = modConfig.modRequest.projectRoot
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
