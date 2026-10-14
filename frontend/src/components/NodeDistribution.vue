<script setup>
import { computed } from 'vue'

const props = defineProps({ nodes: { type: Array, default: () => [] } })
const segments = computed(() => ['self', 'low', 'standard', 'high'].map((key, index) => ({ key, label: ['自建 · 0x', '低倍率', '标准倍率', '高倍率'][index], count: props.nodes.filter(node => node.rate_group === key).length })))
</script>

<template>
  <div class="distribution" aria-label="节点倍率分布">
    <div class="distribution-total"><strong>{{ nodes.length }}</strong><span>个有效节点</span></div>
    <div class="distribution-bar" v-if="nodes.length"><span v-for="segment in segments" :key="segment.key" :class="segment.key" :style="{ flex: segment.count }" :title="`${segment.label}：${segment.count}`"></span></div>
    <div class="distribution-legend"><div v-for="segment in segments" :key="segment.key"><span class="dot" :class="segment.key"></span><span>{{ segment.label }}</span><strong>{{ segment.count }}</strong></div></div>
  </div>
</template>
