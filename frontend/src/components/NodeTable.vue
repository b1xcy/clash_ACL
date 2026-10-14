<script setup>
import { computed, ref } from 'vue'

const props = defineProps({ nodes: { type: Array, default: () => [] } })
const query = ref('')
const visible = computed(() => props.nodes.filter(node => `${node.name} ${node.type}`.toLowerCase().includes(query.value.toLowerCase())))
const labels = { self: '自建', low: '低倍率', standard: '标准倍率', high: '高倍率' }
</script>

<template>
  <div v-if="nodes.length" class="node-table">
    <label class="search"><span>⌕</span><input v-model="query" aria-label="搜索节点" placeholder="搜索节点名称或协议"></label>
    <div class="table-scroll">
      <table>
        <thead><tr><th>节点</th><th>协议</th><th>倍率</th><th>来源</th></tr></thead>
        <tbody>
          <tr v-for="node in visible" :key="node.id || node.name">
            <td class="node-name">{{ node.name }}</td><td><span class="protocol">{{ node.type }}</span></td>
            <td><span class="tag" :class="node.rate_group">{{ labels[node.rate_group] || '未识别' }}{{ node.rate !== undefined ? ` ${node.rate}x` : '' }}</span><span v-if="node.rate_conflict" class="warning"> 冲突</span></td>
            <td class="muted">{{ node.origins?.map(origin => origin.source_name).join('、') || '—' }}</td>
          </tr>
          <tr v-if="!visible.length"><td colspan="4" class="empty">没有匹配的节点</td></tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
