<script setup>
defineProps({ name: { type: String, default: '' }, chain: Boolean, count: { type: Number, default: 0 } })
</script>

<template>
  <div class="chain-preview" aria-label="AI 出口链路">
    <div class="chain-step"><span class="chain-icon">▣</span><strong>你的设备</strong><small>Mihomo</small></div>
    <span class="chain-arrow">→</span>
    <template v-if="chain"><div class="chain-step"><span class="chain-icon">⇄</span><strong>前置优选</strong><small>自动测速 / 手动指定</small></div><span class="chain-arrow">→</span></template>
    <div class="chain-step" :class="{ selected: name }"><span class="chain-icon">◇</span><strong>{{ count > 1 ? `已选 ${count} 个出口` : name || '方案默认出口' }}</strong><small>{{ count > 1 ? '客户端手动切换' : name ? '指定最终出口' : '尚未指定节点' }}</small></div>
    <span class="chain-arrow">→</span><div class="chain-step"><span class="chain-icon">✦</span><strong>AI 服务</strong><small>内置规则分流</small></div>
  </div>
</template>
