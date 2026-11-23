declare module 'virtual:i18n-source' {
  const texts: Record<string, { zh: string; en: string; file: string }>
  export default texts
  // the server's own texts (SMS, push): key -> language -> text
  export const sys: Record<string, Record<string, string>>
}
