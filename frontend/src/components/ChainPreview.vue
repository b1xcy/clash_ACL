<script setup>
defineProps({ name: { type: String, default: '' }, chain: Boolean })
</script>

<template>
  <div class="chain-preview" aria-label="AI 出口链路">
    <div class="chain-step"><span class="chain-icon">▣</span><strong>你的设备</strong><small>Mihomo</small></div>
    <span class="chain-arrow">→</span>
    <template v-if="chain"><div class="chain-step"><span class="chain-icon">⇄</span><strong>前置选择</strong><small>客户端手动切换</small></div><span class="chain-arrow">→</span></template>
    <div class="chain-step" :class="{ selected: name }"><span class="chain-icon">◇</span><strong>{{ name || '方案默认出口' }}</strong><small>{{ name ? '固定最终出口' : '尚未指定固定节点' }}</small></div>
    <span class="chain-arrow">→</span><div class="chain-step"><span class="chain-icon">✦</span><strong>AI 服务</strong><small>内置规则分流</small></div>
  </div>
</template>
