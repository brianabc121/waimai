// Suspend, resume, delete and restore are offered both in the list and on the
// detail page. Each returns true when something changed.
import { del, post } from '../../api'
import { t } from '../../i18n'
import type { MerchantRow } from './types'

export async function suspendMerchant(m: MerchantRow): Promise<boolean> {
  const reason = prompt(`${t('ad.suspend_merchant')}: ${m.name}\n${t('ad.suspend_hint')}\n\n${t('ad.suspend_reason')}`, '')
  if (reason === null) return false
  await post(`/admin/merchants/${m.id}/suspend`, { reason })
  return true
}

export async function resumeMerchant(m: MerchantRow): Promise<boolean> {
  await post(`/admin/merchants/${m.id}/resume`)
  return true
}

export async function deleteMerchant(m: MerchantRow): Promise<boolean> {
  const name = prompt(`${t('ad.delete_merchant')}: ${m.name}\n${t('ad.delete_hint')}`, '')
  if (name === null) return false
  await del(`/admin/merchants/${m.id}`, { confirm_name: name })
  return true
}

export async function restoreMerchant(m: MerchantRow): Promise<boolean> {
  await post(`/admin/merchants/${m.id}/restore`)
  return true
}
