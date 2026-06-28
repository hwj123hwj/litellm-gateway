module.exports = function (api) {
  api.cache(true)
  return {
    presets: ['babel-preset-expo'],
    plugins: [
      // react-native-reanimated/plugin 必须放在最后
      // 当使用 reanimated 动画时取消注释下面这行
      // 'react-native-reanimated/plugin',
    ],
  }
}
