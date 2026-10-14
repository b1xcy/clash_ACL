<script setup>
import { computed, ref } from 'vue'

const props = defineProps({ modelValue: { type: Array, default: () => [] }, groups: { type: Array, default: () => [] }, disabled: Boolean })
const emit = defineEmits(['update:modelValue'])
const search = ref('')
const choices = computed(() => props.groups.flatMap(group => group.nodes))
const selected = computed(() => props.modelValue.map(id => choices.value.find(node => node.id === id) || { id, name: '所选节点当前不可用', missing: true }))
function toggle(id, enabled) { emit('update:modelValue', enabled ? [...props.modelValue, id] : props.modelValue.filter(value => value !== id)) }
function move(index, offset) { const values = [...props.modelValue]; [values[index], values[index + offset]] = [values[index + offset], values[index]]; emit('update:modelValue', values) }
</script>

<template>
  <div id="aiNode" class="exit-picker">
    <label class="search"><span>⌕</span><input v-model="search" aria-label="搜索 AI 出口" placeholder="搜索出口名称或协议" :disabled="disabled"></label>
    <div class="exit-candidates">
      <template v-for="group in groups" :key="group.label">
        <template v-if="group.nodes.some(node => `${node.name} ${node.type}`.toLowerCase().includes(search.toLowerCase()))">
          <small class="exit-source">{{ group.label }}</small>
          <label v-for="node in group.nodes.filter(node => `${node.name} ${node.type}`.toLowerCase().includes(search.toLowerCase()))" :key="node.id" class="check exit-option"><input type="checkbox" :value="node.id" :checked="modelValue.includes(node.id)" :disabled="disabled" @change="toggle(node.id, $event.target.checked)"><span>{{ node.name }}</span><small>{{ node.type }}</small></label>
        </template>
      </template>
      <p v-if="!choices.length" class="hint">导入节点后即可多选出口。留空时跟随规则方案。</p>
    </div>
    <div v-if="selected.length" class="selected-exits"><div v-for="(node, index) in selected" :key="node.id" class="selected-exit"><span :class="{ error: node.missing }">{{ index + 1 }}. {{ node.name }}<small v-if="index === 0">默认出口 / 测速基准</small></span><button class="icon-button" :disabled="index === 0" aria-label="上移出口" @click="move(index, -1)">↑</button><button class="icon-button" :disabled="index === selected.length - 1" aria-label="下移出口" @click="move(index, 1)">↓</button><button class="icon-button" aria-label="移除出口" @click="toggle(node.id, false)">×</button></div></div>
    <p class="hint">已选 {{ selected.length }} 个出口。客户端可在这些节点间切换；上下移动可调整顺序，首个用于默认出口和前置测速基准。</p>
  </div>
</template>
