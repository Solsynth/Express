<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Add16, Close16, Renew16 } from '@carbon/icons-vue'

import { productApi } from '@/lib/api'
import type { ProductWrite } from '@/lib/api'
import { ExpressError } from '@/lib/api/client'
import type { CloudFile, Product } from '@/lib/api/types'
import { installMediaElements } from '@/lib/media'
import { pickArtwork, type PickOutcome } from '@/lib/picker'
import { useNoticeStore } from '@/stores/notice'
import ConsolePanel from '@/components/ConsolePanel.vue'
import AppMark from '@/components/AppMark.vue'

/**
 * Product art: icon, hero background, and the preview strip.
 *
 * Every slot is a Drive reference, so choosing one opens the SnFS picker rather
 * than a file input — the picture already exists in the drive, and copying it
 * into release storage would duplicate it for no reason. Each choice saves
 * immediately: a picture is one visible object, and a deferred id would let an
 * unrelated Cancel silently discard it.
 */
const props = defineProps<{
  product: Product
  canManage: boolean
}>()

const emit = defineEmits<{ updated: [product: Product] }>()

const { t } = useI18n()
const notice = useNoticeStore()

const busy = ref<'' | 'icon' | 'background' | 'previews'>('')
const error = ref('')

const previews = computed(() => props.product.previews ?? [])

// Register the shared element library once for the whole console; art below is
// drawn by `sk-media`, the same renderer the other Solar apps embed.
installMediaElements()

/** The whole write body, so a single-slot save never clears the other slots. */
function body(next: {
  icon?: CloudFile | null
  background?: CloudFile | null
  previews?: CloudFile[]
}): ProductWrite {
  return {
    slug: props.product.slug,
    name: props.product.name,
    names: props.product.names,
    description: props.product.description,
    descriptions: props.product.descriptions,
    icon: 'icon' in next ? next.icon : props.product.icon,
    background: 'background' in next ? next.background : props.product.background,
    previews: 'previews' in next ? next.previews : props.product.previews,
  }
}

async function save(
  slot: 'icon' | 'background' | 'previews',
  next: { icon?: CloudFile | null; background?: CloudFile | null; previews?: CloudFile[] },
) {
  busy.value = slot
  error.value = ''
  try {
    const updated = await productApi.update(props.product.id, body(next))
    emit('updated', updated)
    notice.ok(t('artwork.saved'))
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('artwork.errorSave')
    notice.fail(error.value)
  } finally {
    busy.value = ''
  }
}

function refuse(outcome: PickOutcome): boolean {
  if (outcome.kind === 'cancelled') return true
  if (outcome.kind === 'private') {
    error.value = t('artwork.private', { names: outcome.names.join(', ') })
    notice.fail(error.value)
    return true
  }
  return false
}

async function chooseIcon() {
  const outcome = await pickArtwork({
    title: t('artwork.chooseIcon'),
    description: t('artwork.iconHint'),
  }).catch((caught: unknown) => {
    error.value = caught instanceof Error ? caught.message : t('artwork.errorLoad')
    return null
  })
  if (!outcome || refuse(outcome) || outcome.kind !== 'picked') return
  await save('icon', { icon: outcome.references[0] })
}

async function chooseBackground() {
  const outcome = await pickArtwork({
    title: t('artwork.chooseBackground'),
    description: t('artwork.backgroundHint'),
    height: 620,
  }).catch((caught: unknown) => {
    error.value = caught instanceof Error ? caught.message : t('artwork.errorLoad')
    return null
  })
  if (!outcome || refuse(outcome) || outcome.kind !== 'picked') return
  await save('background', { background: outcome.references[0] })
}

async function addPreviews() {
  const outcome = await pickArtwork({
    title: t('artwork.choosePreviews'),
    description: t('artwork.previewsHint'),
    multiple: true,
    height: 620,
  }).catch((caught: unknown) => {
    error.value = caught instanceof Error ? caught.message : t('artwork.errorLoad')
    return null
  })
  if (!outcome || refuse(outcome) || outcome.kind !== 'picked') return
  // Picking the same picture twice should not add a second copy: the strip is
  // an ordered set, and the server drops duplicates by id anyway.
  const merged = [...previews.value]
  for (const reference of outcome.references) {
    if (!merged.some((existing) => existing.id === reference.id)) merged.push(reference)
  }
  await save('previews', { previews: merged })
}

async function removePreview(id: string) {
  await save('previews', { previews: previews.value.filter((preview) => preview.id !== id) })
}

async function clearSlot(slot: 'icon' | 'background') {
  await save(slot, slot === 'icon' ? { icon: null } : { background: null })
}
</script>

<template>
  <ConsolePanel :title="t('artwork.title')" :note="t('artwork.note')">
    <p v-if="error" class="mb-3 border-l border-error pl-3 text-xs text-error">{{ error }}</p>

    <div class="grid gap-4 sm:grid-cols-[auto_minmax(0,1fr)]">
      <!-- Icon: the mark that appears in every catalog row. -->
      <div>
        <p class="field-label">{{ t('artwork.icon') }}</p>
        <div class="flex items-start gap-3">
          <AppMark :picture="product.icon" :name="product.name" size="lg" />
          <div class="flex flex-col items-start gap-1">
            <button
              type="button"
              class="btn btn-ghost btn-xs"
              :disabled="!canManage || busy !== ''"
              @click="chooseIcon"
            >
              <Renew16 class="mr-1" aria-hidden="true" />
              {{ busy === 'icon' ? t('common.saving') : t('artwork.choose') }}
            </button>
            <button
              v-if="product.icon"
              type="button"
              class="btn btn-ghost btn-xs text-faint hover:text-error"
              :disabled="!canManage || busy !== ''"
              @click="clearSlot('icon')"
            >
              <Close16 class="mr-1" aria-hidden="true" />
              {{ t('artwork.remove') }}
            </button>
          </div>
        </div>
        <p class="mt-2 max-w-[14rem] text-2xs text-faint">{{ t('artwork.iconHint') }}</p>
      </div>

      <!-- Background: the hero image behind the product page header. -->
      <div class="min-w-0">
        <p class="field-label">{{ t('artwork.background') }}</p>
        <div
          v-if="product.background?.id"
          class="relative mb-2 h-24 w-full overflow-hidden border border-line"
        >
          <sk-media :file="product.background.id" class="block h-full w-full"></sk-media>
        </div>
        <p
          v-else
          class="mb-2 flex h-24 items-center justify-center border border-dashed border-line text-2xs text-faint"
        >
          {{ t('artwork.noBackground') }}
        </p>
        <div class="flex flex-wrap items-center gap-1">
          <button
            type="button"
            class="btn btn-ghost btn-xs"
            :disabled="!canManage || busy !== ''"
            @click="chooseBackground"
          >
            <Renew16 class="mr-1" aria-hidden="true" />
            {{ busy === 'background' ? t('common.saving') : t('artwork.choose') }}
          </button>
          <button
            v-if="product.background"
            type="button"
            class="btn btn-ghost btn-xs text-faint hover:text-error"
            :disabled="!canManage || busy !== ''"
            @click="clearSlot('background')"
          >
            <Close16 class="mr-1" aria-hidden="true" />
            {{ t('artwork.remove') }}
          </button>
        </div>
        <p class="mt-2 text-2xs text-faint">{{ t('artwork.backgroundHint') }}</p>
      </div>
    </div>

    <!-- Previews: the screenshot strip on the public product page. -->
    <div class="mt-5 border-t border-line-soft pt-4">
      <div class="flex flex-wrap items-baseline justify-between gap-2">
        <p class="field-label mb-0">
          {{ t('artwork.previews') }}
          <span class="ml-1 normal-case tracking-normal text-faint">
            {{ t('artwork.previewCount', { n: previews.length }) }}
          </span>
        </p>
        <button
          type="button"
          class="btn btn-ghost btn-xs"
          :disabled="!canManage || busy !== ''"
          @click="addPreviews"
        >
          <Add16 class="mr-1" aria-hidden="true" />
          {{ busy === 'previews' ? t('common.saving') : t('artwork.addPreview') }}
        </button>
      </div>

      <p v-if="!previews.length" class="voice mt-3 text-[12px]">{{ t('artwork.noPreviews') }}</p>
      <ul v-else class="mt-3 flex flex-wrap gap-2">
        <li
          v-for="preview in previews"
          :key="preview.id"
          class="group relative h-20 w-32 shrink-0 overflow-hidden border border-line"
        >
          <sk-media :file="preview.id" class="block h-full w-full"></sk-media>
          <button
            type="button"
            class="absolute right-1 top-1 flex h-5 w-5 items-center justify-center border border-line bg-base-100/90 text-faint opacity-0 transition-opacity hover:text-error focus-visible:opacity-100 group-hover:opacity-100"
            :aria-label="t('artwork.removePreview', { name: preview.name ?? '' })"
            :disabled="!canManage || busy !== ''"
            @click="removePreview(preview.id)"
          >
            <Close16 aria-hidden="true" />
          </button>
        </li>
      </ul>
      <p class="mt-2 text-2xs text-faint">{{ t('artwork.previewsHint') }}</p>
    </div>

    <p class="mt-4 border-l border-line pl-3 text-2xs text-faint">{{ t('artwork.publicHint') }}</p>
  </ConsolePanel>
</template>
