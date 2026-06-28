import { registerPlugin } from '@capacitor/core'

export interface ApkUpdaterPlugin {
  downloadAndInstall(options: { url: string; fileName?: string }): Promise<{
    success: boolean
    path: string
  }>
  getAppVersion(): Promise<{ version: string }>
  addListener(
    eventName: 'downloadProgress',
    listenerFunc: (data: { percent: number }) => void,
  ): Promise<any>
}

const ApkUpdater = registerPlugin<ApkUpdaterPlugin>('ApkUpdater')

export default ApkUpdater
