import type { Model, ModelSpecs } from '@/api';

/**
 * 模型档案完整度与下架派生判断（模型域 §6.7.6 / #596）。
 *
 * # 为什么不落库、不进契约、且全前端只写这一份
 *
 * 档案完整度是纯派生值（只依赖 `model_specs` 的存在性与 `contextWindow` / `modalitiesIn`），
 * 不落库也不增设接口字段。管理端列表与后续确认面板（#597）都消费同一份判定；
 * 若在两处各写一遍 `specs == null`，口径改动时必然一处漏改。
 */
export type ProfileCompleteness = 'missing' | 'partial' | 'complete';

export const PROFILE_COMPLETENESS_LABEL: Record<ProfileCompleteness, string> = {
  missing: '档案缺失',
  partial: '档案部分',
  complete: '档案完整',
};

export const MODEL_STATUS_LABEL: Record<Model['status'], string> = {
  active: '可用',
  candidate: '候选',
  unverified: '未核实',
  deprecated: '已退役',
};

/**
 * 档案完整度三档唯一判定：
 * - `missing`：无 `model_specs` 行（`specs` 为 `null` / `undefined`）
 * - `partial`：有行，但 `contextWindow` 或 `modalitiesIn` 任一为空
 * - `complete`：两者都有
 */
export function profileCompleteness(specs: ModelSpecs | null | undefined): ProfileCompleteness {
  if (!specs) return 'missing';
  const hasContext = specs.contextWindow != null;
  const hasModalities = (specs.modalitiesIn ?? '').trim() !== '';
  return hasContext && hasModalities ? 'complete' : 'partial';
}

/**
 * 「已下架」派生标记：已退役且没有合并归宿（不新增状态值、不新增列）。
 */
export function isModelDelisted(model: Pick<Model, 'status' | 'mergedIntoSlug'>): boolean {
  return model.status === 'deprecated' && !model.mergedIntoSlug;
}
