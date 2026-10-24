<script setup lang="ts">
defineOptions({ name: 'PromptNewView' })
import { ref } from 'vue'
import { api } from '../api'
import type { PromptProposalOut } from '../api/types'
import { t } from '../i18n'
import { isImeComposing } from '../utils/ime'
import PageHero from '../components/PageHero.vue'

const title = ref('')
const content = ref('')
const category = ref('')
const sourceUrl = ref('')
const submitting = ref(false)
const error = ref('')
const accepted = ref<PromptProposalOut | null>(null)

async function submit() {
  if (submitting.value) return
  error.value = ''
  if (!title.value.trim() || !content.value.trim()) {
    error.value = t('prompts.required', '请填写标题和提示词正文')
    return
  }
  const source = sourceUrl.value.trim()
  if (source) {
    try {
      const url = new URL(source)
      const authority = source.match(/^https?:\/\/([^/?#]*)/i)?.[1]
      if (!authority || authority.includes('@') || !['http:', 'https:'].includes(url.protocol) || url.username || url.password
        || Array.from(source).some((char) => /\s/u.test(char) || char.charCodeAt(0) < 32 || char === '\u0085')) throw new Error()
    } catch {
      error.value = t('prompts.sourceInvalid', '来源链接需为完整的 http 或 https 地址')
      return
    }
  }
  submitting.value = true
  try {
    accepted.value = await api.proposePrompt({
      title: title.value.trim(),
      content: content.value.trim(),
      category: category.value.trim() || null,
      source_url: source || null,
    })
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    submitting.value = false
  }
}

function again() {
  accepted.value = null
  title.value = ''
  content.value = ''
  category.value = ''
  sourceUrl.value = ''
  error.value = ''
}
</script>

<template>
  <div class="route-page">
    <PageHero>
      <h1 class="page-title">{{ t('prompts.contribute', '收录提示词') }}</h1>
      <p class="sub">{{ t('prompts.contributeSubtitle', '存下一条值得测试的提示词。还没有作品，也可以先收录。') }}</p>
    </PageHero>
    <section class="section pn-section">
      <div v-if="accepted" class="card card-default pn-success" role="status">
        <h2 class="pn-success-title">{{ accepted.status === 'existing' ? t('prompts.alreadyExists', '这条提示词已在库中') : t('prompts.submitted', '已提交，等待审核') }}</h2>
        <p>{{ accepted.status === 'existing' ? t('prompts.existingHint', '可以直接复制已有原文，或给它补上一份测试作品。') : t('prompts.pendingHint', '管理员审核后会公开展示，随后就能关联测试作品。') }}</p>
        <div class="filter-row pn-actions">
          <RouterLink v-if="accepted.status === 'existing' && accepted.prompt_id" class="btn btn-primary" :to="`/prompts/${accepted.prompt_id}`">{{ t('prompts.viewExisting', '查看已收录提示词') }}</RouterLink>
          <RouterLink v-else class="btn btn-primary" to="/prompts">{{ t('prompts.back', '返回提示词库') }}</RouterLink>
          <button class="btn btn-secondary" type="button" @click="again">{{ t('prompts.contributeAnother', '再收录一条') }}</button>
        </div>
      </div>
      <form v-else class="card card-default pn-form" @submit.prevent="submit" @keydown.enter="isImeComposing($event) && $event.preventDefault()">
        <label class="pn-field">
          <span>{{ t('prompts.fieldTitle', '标题') }} <span aria-hidden="true">*</span></span>
          <input v-model="title" class="input" required maxlength="200" :placeholder="t('prompts.titlePlaceholder', '给这条提示词起一个容易找到的名字')" />
        </label>
        <label class="pn-field">
          <span>{{ t('prompts.fieldContent', '提示词正文') }} <span aria-hidden="true">*</span></span>
          <textarea v-model="content" class="input pn-content" required rows="12" maxlength="20000" aria-describedby="prompt-content-hint"
            :placeholder="t('prompts.contentPlaceholder', '粘贴完整原文，保留要求、约束与格式。')"></textarea>
        </label>
        <p id="prompt-content-hint" class="pn-hint">{{ t('prompts.contentHint', '后续作品会按这份原文关联。需要参考图片或附件时，请在正文中写明获取方式。') }}</p>
        <div class="pn-optional">
          <label class="pn-field">
            <span>{{ t('prompts.fieldCategory', '分类（可选）') }}</span>
            <input v-model="category" class="input" maxlength="64" :placeholder="t('prompts.categoryExample', '例如：游戏、工具、视觉效果')" />
          </label>
          <label class="pn-field">
            <span>{{ t('prompts.fieldSource', '来源链接（可选）') }}</span>
            <input v-model="sourceUrl" class="input" type="url" inputmode="url" maxlength="2048" placeholder="https://" aria-describedby="prompt-source-hint" />
          </label>
        </div>
        <p id="prompt-source-hint" class="pn-hint">{{ t('prompts.sourceHint', '从其他地方收集的提示词，可以附上原作者的页面链接。') }}</p>
        <p v-if="error" class="notice notice-error" role="alert">{{ error }}</p>
        <div class="filter-row pn-actions">
          <button class="btn btn-primary" type="submit" :disabled="submitting">{{ submitting ? t('prompts.submitting', '提交中…') : t('prompts.submit', '提交审核') }}</button>
          <RouterLink class="btn btn-secondary" to="/prompts">{{ t('common.cancel', '取消') }}</RouterLink>
        </div>
      </form>
    </section>
  </div>
</template>

<style scoped>
.pn-section { padding-top: var(--sp-8); }
.pn-form, .pn-success { max-width: var(--w-read); display: grid; gap: var(--sp-16); padding: var(--sp-24); }
.pn-field { display: grid; gap: var(--sp-8); min-width: 0; font-size: var(--fs-14); font-weight: 800; }
.pn-content { font-family: var(--font-code); font-size: var(--fs-14); line-height: 1.7; resize: vertical; }
.pn-hint { margin: 0; font-size: var(--fs-12); line-height: 1.6; color: var(--ink-soft); }
.pn-optional { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 2fr); gap: var(--sp-16); }
.pn-actions { margin: var(--sp-8) 0 0; }
.pn-success-title { margin: 0; font-family: var(--font-heading); font-size: var(--fs-22); }
.pn-success p { margin: 0; font-size: var(--fs-14); line-height: 1.7; }
@media (max-width: 720px) { .pn-form, .pn-success { padding: var(--sp-16); } .pn-optional { grid-template-columns: minmax(0, 1fr); } }
</style>
