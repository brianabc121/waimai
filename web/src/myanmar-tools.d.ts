declare module 'myanmar-tools' {
  export class ZawgyiDetector { getZawgyiProbability(s: string): number }
  export class ZawgyiConverter { zawgyiToUnicode(s: string): string; unicodeToZawgyi(s: string): string }
}
