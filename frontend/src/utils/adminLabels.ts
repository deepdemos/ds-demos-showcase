// 后台动作词表（统一操作语言的单一来源）：
// 同一个语义在四个面板里叫四种名字（批准/采纳/收下/应用），等于逼管理员每次重新学习。
// 所有面板的动作徽章、按钮、确认文案都从这里取词。
import { t } from '../i18n'

const ACTION_KEYS: Record<string, [string, string]> = {
  create: ['actCreate', '新建'],
  update: ['actUpdate', '修改'],
  status_set: ['actStatus', '改状态'],
  merge: ['actMerge', '合并'],
  unmerge: ['actUnmerge', '撤销合并'],
  alias_add: ['actAliasAdd', '加别名'],
  alias_remove: ['actAliasRemove', '删别名'],
  attach: ['actAttach', '挂题'],
  detach: ['actDetach', '撤挂'],
  delete: ['actDelete', '删除'],
  review: ['actReview', '批准/驳回'],
  attribute: ['actAttribute', '归属'],
  slug_set: ['actSlug', '改 slug'],
}

export function actionLabel(action: string): string {
  const hit = ACTION_KEYS[action]
  return hit ? t(`admin.${hit[0]}`, hit[1]) : action
}

/**
 * 审计行的动作：`review` 一条记录其实是"批准"或"驳回"之一，
 * 显示成「批准/驳回」等于把已发生的事重新变成疑问 —— 方向就在 after.status 里。
 */
export function auditActionLabel(a: { action: string; after?: Record<string, unknown> | string | null }): string {
  if (a.action === 'review') {
    const status = a.after && typeof a.after === 'object' ? (a.after as Record<string, unknown>).status : undefined
    if (status === 'approved') return t('admin.actReviewOk', '批准')
    if (status === 'rejected') return t('admin.actReviewNo', '驳回')
  }
  return actionLabel(a.action)
}

/** 破坏性动作：红色 + 必须预览（跨面板一致的告警语汇） */
export const DESTRUCTIVE = new Set(['delete', 'merge', 'unmerge', 'attribute', 'slug_set'])

export function isDestructive(action: string): boolean {
  return DESTRUCTIVE.has(action)
}

/** 统一时间格式：后台看的是"什么时候发生的"，秒级足够 */
export function fmtTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
}

/**
 * 收件箱 kind 词表（M2-t4 单一源化）：概览台 kind 直达链/收件箱分节/批量确认共用同一份，
 * 杜绝「概览台叫类型细分、收件箱叫 retag」的双词表漂移。[zh, en]。
 */
export const INBOX_KINDS: Record<string, [string, string]> = {
  task_match: ['挂题请求', 'attach task'],
  new_model: ['新模型', 'new model'],
  new_task: ['新题目', 'new task'],
  new_prompt: ['新提示词', 'new prompt'],
  merge_model: ['模型合并', 'merge model'],
  merge_task: ['题目合并', 'merge task'],
  alias: ['别名归一', 'alias'],
  retag_demo: ['类型细分', 'refine type'],
}

/** 概览台合成待办的固定值申请 chip（TagValueSuggestion，不并进 EntitySuggestion kind） */
export const TAGREQ_KIND: [string, string] = ['固定值申请', 'fixed-value request']

export function inboxKindLabel(k: string): string {
  const hit = INBOX_KINDS[k]
  return hit ? t(`admin.kind.${k}`, hit[0]) : k
}
