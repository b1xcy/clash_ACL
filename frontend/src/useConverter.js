import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { api, post } from './api'

export function useConverter() {
  const form = reactive({ subscriptions: '', directLinks: '', aggregate: true, dropPlaintext: false, sourceFilters: {}, excludedURLs: [], preset: 'acl4ssr-online', ratePattern: '', renameRules: [], aiNodes: [], aiChain: false, aiTestURL: '', aiDomains: '', groupsJSON: '[]', customRules: '', policyMap: '{}', fallback: '', skipUnsupported: true, refreshGeneration: false })
  const ready = ref(false), notice = ref(''), noticeType = ref(''), advanced = ref(false)
  const sources = ref([]), sourcePreviews = ref({}), sourceURLs = ref([]), sourcesLoading = ref(false)
  const imported = ref([]), validLinks = ref([]), selfHostedIDs = ref([]), savedNodes = ref([])
  const nodes = ref([]), aggregation = ref(null), aggregateSummary = ref('添加链接后自动读取、解析与聚合。')
  const uriSummary = ref('每行一个链接，支持混合协议；有效节点自动导入。')
  const presets = ref([]), ruleStatus = ref(null), builtinAIRules = ref([])
  const generation = ref(null), preview = ref(null), generating = ref(false), previewing = ref(false), saving = ref(false), generationSummary = ref('配置就绪后，生成一条可在客户端订阅的地址。')
  const refreshDownload = ref(false), rulesText = ref(''), ruleSummary = ref(''), rulePreviewing = ref(false)
  const yamlText = ref(''), yamlNodes = ref([]), yamlSummary = ref(''), yamlBusy = ref(false)
  let revision = 0, sourceRevision = 0, aggregateRevision = 0, parsedText = null, parseJob
  let sourceTimer, sourcePoll, uriTimer, aggregateTimer, ruleTimer

  const lines = text => text.split(/\r?\n/).map(value => value.trim()).filter(Boolean)
  const allURLs = computed(() => [...new Set(lines(form.subscriptions))])
  const selectedURLs = computed(() => form.aggregate ? allURLs.value.filter(url => !form.excludedURLs.includes(url)) : allURLs.value.slice(0, 1))
  const aiChoices = computed(() => {
    const selected = new Set(selectedURLs.value)
    const subscriptionNodes = sources.value.flatMap((source, index) => selected.has(sourceURLs.value[index]) ? sourcePreviews.value[source.id] || [] : [])
    const directNodes = imported.value.filter(line => !line.error).map(line => ({ id: line.node_id, name: line.name, type: line.type }))
    const retained = (form.dropPlaintext || Object.keys(form.sourceFilters).length) && aggregation.value ? new Set(nodes.value.map(node => node.id)) : null
    const seen = new Set()
    return [['直接导入', directNodes], ['订阅节点', subscriptionNodes], ['既有保存节点', savedNodes.value]].map(([label, items]) => ({ label, nodes: items.filter(node => { if (!node.id || seen.has(node.id) || (retained && label !== '既有保存节点' && !retained.has(node.id))) return false; seen.add(node.id); return true }) }))
  })
  const chosenAI = computed(() => aiChoices.value.flatMap(group => group.nodes).find(node => node.id === form.aiNodes[0]))
  const aiMissing = computed(() => form.aiNodes.some(id => !aiChoices.value.some(group => group.nodes.some(node => node.id === id))))
  const downloadURL = computed(() => generation.value ? new URL(generation.value.download_path + (refreshDownload.value ? '&refresh=true' : ''), location.href).href : '')
  const aiPlaceholder = computed(() => '以下规则已内置，无需重复填写。\n留空即可；这里只填写你的额外中转站域名。\n\n' + builtinAIRules.value.join('\n'))

  function message(text, type = '') { notice.value = text; noticeType.value = type }
  function invalidate() {
    revision++
    if (generation.value) generationSummary.value = '输入已修改，请重新生成订阅。'
    generation.value = null
    preview.value = null
  }
  function sourceFilterID(url) {
    return sources.value[sourceURLs.value.indexOf(url)]?.id
  }
  function sourceFilterValue(url) {
    const value = form.sourceFilters[sourceFilterID(url)]
    return value === undefined ? '' : String(value)
  }
  function setSourceFilter(url, value) {
    const id = sourceFilterID(url)
    if (!id) return
    if (value === '') delete form.sourceFilters[id]
    else form.sourceFilters[id] = value === 'true'
  }
  function nodePolicy() {
    const ids = new Set(imported.value.filter(line => !line.error).map(line => line.node_id))
    return { source_drop_plaintext: { ...form.sourceFilters }, drop_plaintext: form.dropPlaintext, rename_rules: form.renameRules, rate_pattern: form.ratePattern.trim(), self_hosted_ids: selfHostedIDs.value.filter(id => ids.has(id)) }
  }
  function generationRequest() {
    const groups = JSON.parse(form.groupsJSON || '[]')
    if (!Array.isArray(groups)) throw new Error('代理组需要 JSON 数组。')
    const policyMap = JSON.parse(form.policyMap || '{}')
    if (!policyMap || Array.isArray(policyMap) || typeof policyMap !== 'object' || Object.values(policyMap).some(value => typeof value !== 'string')) throw new Error('模板策略映射需要字符串值的 JSON 对象。')
    return { urls: selectedURLs.value, aggregate: form.aggregate, direct_links: validLinks.value.join('\n'), policy: nodePolicy(), ai: { node_id: '', node_ids: [...form.aiNodes], test_url: form.aiTestURL.trim(), chain: form.aiChain, domains: lines(form.aiDomains) }, groups_json: form.groupsJSON || '[]', rules: { preset: form.preset, custom_rules: lines(form.customRules), policy_map: policyMap, fallback: form.fallback.trim(), skip_unsupported: form.skipUnsupported } }
  }
  function scheduleAggregate() {
    clearTimeout(aggregateTimer)
    aggregateRevision++
    nodes.value = []
    aggregation.value = null
    aggregateSummary.value = '正在更新聚合预览…'
    aggregateTimer = setTimeout(aggregateNodes, 400)
  }
  async function parseLinks(force = false) {
    clearTimeout(uriTimer)
    const text = form.directLinks
    if (!force && text === parsedText) return
    if (parseJob?.text === text) return parseJob.promise
    const promise = (async () => {
      try {
        const data = text.trim() ? await api('/api/uri/preview', { method: 'POST', headers: { 'Content-Type': 'text/plain' }, body: text }) : { lines: [], valid_count: 0, error_count: 0 }
        if (text !== form.directLinks) return
        parsedText = text
        imported.value = data.lines
        const original = text.split('\n')
        validLinks.value = data.lines.filter(line => !line.error).map(line => original[line.line - 1].trim())
        uriSummary.value = `已自动导入 ${data.valid_count} 个有效节点；${data.error_count} 行失败，未导入。`
        scheduleAggregate()
      } catch (error) {
        if (text === form.directLinks) { uriSummary.value = error.message; throw error }
      }
    })()
    parseJob = { text, promise }
    try { await promise } finally { if (parseJob?.promise === promise) parseJob = undefined }
  }
  async function readSources(refresh = false) {
    clearTimeout(sourceTimer)
    clearTimeout(sourcePoll)
    const epoch = sourceRevision, urls = [...selectedURLs.value]
    if (!urls.length) { sourcesLoading.value = false; sources.value = []; scheduleAggregate(); return }
    sourcesLoading.value = true
    const query = new URLSearchParams({ aggregate: String(form.aggregate) })
    urls.forEach(url => query.append('url', url))
    try {
      let data = await api('/api/subscriptions?' + query + (refresh ? '&refresh=true' : ''))
      if (epoch !== sourceRevision) return
      if (!refresh) {
        const pending = data.sources.map((source, index) => source.state === 'pending' ? urls[index] : null).filter(Boolean)
        if (pending.length) {
          const fetchQuery = new URLSearchParams({ aggregate: 'true', refresh: 'true' })
          pending.forEach(url => fetchQuery.append('url', url))
          await api('/api/subscriptions?' + fetchQuery)
          if (epoch !== sourceRevision) return
          data = await api('/api/subscriptions?' + query)
        }
      }
      if (epoch !== sourceRevision) return
      sources.value = data.sources
      sourcePreviews.value = data.previews || {}
      sourceURLs.value = urls
      sourcesLoading.value = data.sources.some(source => source.state === 'refreshing')
      if (sourcesLoading.value) sourcePoll = setTimeout(() => readSources(), 1500)
      else scheduleAggregate()
    } catch (error) {
      if (epoch === sourceRevision) { sourcesLoading.value = false; message(error.message, 'error'); aggregateSummary.value = error.message }
    }
  }
  async function aggregateNodes() {
    clearTimeout(aggregateTimer)
    const epoch = aggregateRevision
    try {
      await parseLinks()
      clearTimeout(aggregateTimer)
      if (sourcesLoading.value) { aggregateSummary.value = '正在读取来源，完成后自动聚合…'; return }
      if (!selectedURLs.value.length && !validLinks.value.length) { nodes.value = []; aggregateSummary.value = '请添加订阅或直接节点链接。'; return }
      const current = aggregateRevision
      const data = await post('/api/aggregate/preview', { urls: selectedURLs.value, aggregate: form.aggregate, direct_links: validLinks.value.join('\n'), policy: nodePolicy() })
      if (current !== aggregateRevision) return
      nodes.value = data.nodes
      aggregation.value = data
      aggregateSummary.value = `输入 ${data.input_count} 个节点，保留 ${data.node_count} 个，合并重复 ${data.duplicate_count} 个，丢弃 HTTP/SOCKS ${data.filtered_count || 0} 个。自建 ${data.rate_counts.self} / 低 ${data.rate_counts.low} / 标准 ${data.rate_counts.standard} / 高 ${data.rate_counts.high}；未识别 ${data.rate_counts.unknown}；倍率冲突 ${data.rate_counts.conflicts}。` + (data.sources.some(source => source.using_cache) ? '部分来源使用最近成功缓存。' : '')
    } catch (error) { if (epoch === aggregateRevision) aggregateSummary.value = error.message }
  }
  async function generate(previewOnly = false) {
    const busy = previewOnly ? previewing : generating
    if (busy.value || !ready.value) return
    busy.value = true
    if (!previewOnly) invalidate()
    const epoch = revision
    try {
      await parseLinks()
      if (epoch !== revision) { message('输入已修改，请按当前设置重新生成。'); return }
      const path = previewOnly ? '/api/generate/preview' : '/api/generate' + (form.refreshGeneration ? '?refresh=true' : '')
      const data = await post(path, generationRequest())
      if (epoch !== revision) { message('输入已修改，请按当前设置重新生成。'); return }
      preview.value = previewOnly ? data : data.preview
      if (previewOnly) generationSummary.value = '分组与规则预览成功，生成订阅后即可使用下载地址。'
      else { generation.value = data; generationSummary.value = '订阅已生成 · ' + new Date(data.generated_at).toLocaleString() + (data.using_cache ? ' · 部分来源使用最近成功缓存。' : ''); message('完整 Mihomo 订阅已生成。', 'success') }
    } catch (error) { if (epoch === revision) { generationSummary.value = error.message; message(error.message, 'error') } }
    finally { busy.value = false }
  }
  async function saveOptions() {
    saving.value = true
    try {
      await post('/api/options', { source_drop_plaintext: { ...form.sourceFilters }, drop_plaintext: form.dropPlaintext, rename_rules: form.renameRules, rate_pattern: form.ratePattern.trim(), ai: { node_id: '', node_ids: [...form.aiNodes], test_url: form.aiTestURL.trim(), chain: form.aiChain, domains: lines(form.aiDomains) }, groups_json: form.groupsJSON }, 'PUT')
      message('转换设置已保存。', 'success')
    } catch (error) { message(error.message, 'error') } finally { saving.value = false }
  }
  async function previewRules() {
    rulePreviewing.value = true
    rulesText.value = ''
    try {
      const request = generationRequest()
      const groups = JSON.parse(form.groupsJSON || '[]')
      const data = await post('/api/rules/preview', { ...request.rules, targets: [...savedNodes.value.map(node => node.name), ...groups.map(group => group.name)] })
      rulesText.value = data.rules.join('\n')
      ruleSummary.value = `自定义 ${data.custom_count} 条 · 外部 ${data.external_count} 条 · 兜底 1 条 · 去重 ${data.duplicate_count} 条` + (data.skipped_rules.length ? ` · 兼容跳过 ${data.skipped_rules.length} 条：` + data.skipped_rules.map(rule => `${rule.ruleset} #${rule.item} ${rule.type}`).join('；') : '')
    } catch (error) { ruleSummary.value = error.message } finally { rulePreviewing.value = false }
  }
  async function loadAIRules() { builtinAIRules.value = (await api('/api/rules/ai')).rules }
  async function loadRuleStatus() {
    clearTimeout(ruleTimer)
    try {
      const data = await api('/api/rules/status')
      if (ruleStatus.value && ruleStatus.value.version !== data.version) { invalidate(); await loadAIRules() }
      ruleStatus.value = data
      ruleTimer = setTimeout(loadRuleStatus, data.state === 'updating' ? 2000 : 60000)
    } catch (error) { message(error.message, 'error') }
  }
  async function updateRules() {
    try { ruleStatus.value = await api('/api/rules/update', { method: 'POST' }); await loadRuleStatus() }
    catch (error) { message(error.message, 'error') }
  }
  async function parseYAML() {
    yamlBusy.value = true; yamlNodes.value = []; yamlSummary.value = ''
    try { const data = await api('/api/yaml/preview', { method: 'POST', headers: { 'Content-Type': 'text/yaml' }, body: yamlText.value }); yamlNodes.value = data.nodes; yamlSummary.value = `成功解析 ${data.node_count} 个节点；预览不保存或导入节点。` }
    catch (error) { yamlSummary.value = error.message } finally { yamlBusy.value = false }
  }
  async function copyURL() {
    try {
      if (navigator.clipboard && window.isSecureContext) await navigator.clipboard.writeText(downloadURL.value)
      else { const input = document.getElementById('convertedUrl'); input.select(); if (!document.execCommand('copy')) throw new Error('请手动选择并复制订阅地址。') }
      message('订阅地址已复制。', 'success')
    } catch (error) { message(error.message, 'error') }
  }

  watch(form, invalidate, { deep: true, flush: 'sync' })
  watch(selfHostedIDs, () => { invalidate(); scheduleAggregate() }, { deep: true, flush: 'sync' })
  watch(() => [form.subscriptions, form.aggregate, ...form.excludedURLs], () => {
    sourceRevision++; clearTimeout(sourceTimer); clearTimeout(sourcePoll)
    sources.value = []; sourcePreviews.value = {}; sourceURLs.value = []
    sourcesLoading.value = selectedURLs.value.length > 0
    sourceTimer = setTimeout(() => readSources(), 650)
    scheduleAggregate()
  }, { flush: 'sync' })
  watch(() => form.directLinks, () => {
    parsedText = null; imported.value = []; validLinks.value = []; clearTimeout(uriTimer)
    uriSummary.value = '正在自动解析…'; scheduleAggregate()
    uriTimer = setTimeout(() => parseLinks().catch(() => {}), 350)
  }, { flush: 'sync' })
  watch(() => [form.dropPlaintext, JSON.stringify(form.sourceFilters), form.ratePattern, JSON.stringify(form.renameRules)], scheduleAggregate)
  onMounted(async () => {
    try {
      const [options, available] = await Promise.all([api('/api/options'), api('/api/rules/presets'), loadAIRules(), loadRuleStatus()])
      Object.assign(form, { sourceFilters: options.options.source_drop_plaintext || {}, dropPlaintext: !!options.options.drop_plaintext, renameRules: options.options.rename_rules || [], ratePattern: options.options.rate_pattern || '', aiNodes: options.options.ai.node_ids ?? (options.options.ai.node_id ? [options.options.ai.node_id] : []), aiTestURL: options.options.ai.test_url || '', aiChain: !!options.options.ai.chain, aiDomains: (options.options.ai.domains || []).join('\n'), groupsJSON: options.options.groups_json || '[]' })
      savedNodes.value = options.self_hosted_nodes || []
      presets.value = [...available.presets, { id: 'none', name: '自定义规则（保留已启用服务分流）' }]
      ready.value = true
    } catch (error) { message(error.message, 'error') }
  })
  onBeforeUnmount(() => [sourceTimer, sourcePoll, uriTimer, aggregateTimer, ruleTimer].forEach(clearTimeout))
  return { form, ready, notice, noticeType, message, advanced, allURLs, selectedURLs, sources, sourcePreviews, sourcesLoading, imported, selfHostedIDs, nodes, aggregation, aggregateSummary, uriSummary, presets, ruleStatus, builtinAIRules, aiPlaceholder, aiChoices, chosenAI, aiMissing, generation, preview, generating, previewing, saving, generationSummary, downloadURL, refreshDownload, rulesText, ruleSummary, rulePreviewing, yamlText, yamlNodes, yamlSummary, yamlBusy, parseLinks, readSources, aggregateNodes, generate, saveOptions, previewRules, updateRules, parseYAML, copyURL, sourceFilterID, sourceFilterValue, setSourceFilter }
}
