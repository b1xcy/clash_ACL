<script setup>
import { computed, ref } from 'vue'
import { useConverter } from './useConverter'
import NodeTable from './components/NodeTable.vue'
import NodeDistribution from './components/NodeDistribution.vue'
import ChainPreview from './components/ChainPreview.vue'
import AIExitPicker from './components/AIExitPicker.vue'

const {
  form, ready, notice, noticeType, advanced, allURLs, selectedURLs, sources, sourcePreviews, sourcesLoading,
  imported, selfHostedIDs, nodes, aggregation, aggregateSummary, uriSummary, presets, ruleStatus, builtinAIRules,
  aiPlaceholder, aiChoices, chosenAI, aiMissing, generation, preview, generating, previewing, saving,
  generationSummary, downloadURL, refreshDownload, rulesText, ruleSummary, rulePreviewing,
  yamlText, yamlNodes, yamlSummary, yamlBusy, parseLinks, readSources, aggregateNodes, generate,
  saveOptions, previewRules, updateRules, parseYAML, copyURL, sourceFilterID, sourceFilterValue, setSourceFilter,
} = useConverter()

const dialog = ref(null), dialogTitle = ref('节点预览'), dialogNodes = ref([]), showYAML = ref(false)
function setSourceEnabled(url, enabled) { form.excludedURLs = enabled ? form.excludedURLs.filter(value => value !== url) : [...form.excludedURLs, url] }
function downloadConfig() { window.location.href = downloadURL.value }
function importClient() { window.location.href = 'clash://install-config?url=' + encodeURIComponent(downloadURL.value) }
const activeSection = ref('sources')
const selectedPreset = computed(() => presets.value.find(preset => preset.id === form.preset))
const aiEntryTestURL = computed(() => preview.value?.groups.find(group => group.name === '⚡ AI 前置自动')?.url)
const stateLabels = { ready: '已更新', cached: '使用旧缓存', error: '读取失败', pending: '尚未读取', refreshing: '读取中', disabled: '已停用' }
const ruleStateText = computed(() => {
  const state = ruleStatus.value
  if (!state) return '正在读取规则版本…'
  return `${state.origin === 'upstream' ? '上游规则' : '内置快照'} ${state.version.slice(0, 12)} · ${state.enabled ? `每 ${state.interval_hours} 小时检查` : '自动更新已关闭'}`
})
function openSource(source) { showYAML.value = false; dialogTitle.value = source.name || '来源节点'; dialogNodes.value = sourcePreviews.value[source.id] || []; dialog.value.showModal() }
function openYAML() { showYAML.value = true; dialogTitle.value = '本地 YAML 预览'; dialog.value.showModal() }
function navigate(id) { activeSection.value = id; if (id === 'rules') advanced.value = true; setTimeout(() => document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 0) }
</script>

<template>
  <div class="app-shell">
    <aside class="sidebar">
      <a class="brand" href="#sources"><span class="brand-mark">↗</span><span>clash<span class="brand-accent">_ACL</span><small>SUBSCRIPTION STUDIO</small></span></a>
      <div class="nav-label">工作空间</div>
      <nav aria-label="主要导航">
        <button v-for="item in [['sources', '⇄', '订阅与节点'], ['nodes', '▦', '节点总览'], ['exit', '◇', 'AI 出口策略'], ['rules', '≡', '规则与分组'], ['result', '↗', '生成与导出']]" :key="item[0]" :class="{ active: activeSection === item[0] }" @click="navigate(item[0])"><span>{{ item[1] }}</span>{{ item[2] }}</button>
      </nav>
      <div class="sidebar-bottom"><span class="live-dot"></span>Mihomo / Clash.Meta<a href="https://github.com/b1xcy/clash_ACL" target="_blank" rel="noreferrer">GitHub ↗</a></div>
    </aside>

    <div class="main-shell">
      <header class="topbar"><span>工作空间 <span class="breadcrumb">/ 订阅转换</span></span><span class="local-badge">v1.2.4 · 本地规则 · 无需登录</span></header>
      <main class="converter">
        <div class="page-heading"><div><div class="eyebrow">YOUR NODES. YOUR ROUTES.</div><h1>订阅转换</h1><p>导入节点，配置出口，生成你的 Mihomo 订阅。</p></div><div class="mode-switch" aria-label="配置模式"><button id="basicTab" :class="{ active: !advanced }" @click="advanced = false">基础配置</button><button id="advancedTab" :class="{ active: advanced }" @click="advanced = true">进阶配置</button></div></div>
        <div v-if="notice" id="message" class="notice" :class="noticeType" role="status" aria-live="polite">{{ notice }}</div>

        <div class="workspace">
          <div class="configuration">
            <section id="sources" class="card">
              <div class="card-heading"><span class="section-number">01</span><div><h2>订阅与节点</h2><p>粘贴即导入，来源和节点自动聚合。</p></div><span class="pill">{{ selectedURLs.length }} 个来源</span></div>
              <label for="subscriptionInput">订阅链接</label>
              <textarea id="subscriptionInput" v-model="form.subscriptions" rows="3" spellcheck="false" placeholder="https://example.com/subscribe?token=…&#10;每行一个 Clash / Mihomo YAML 订阅地址"></textarea>
              <div class="source-selection" v-if="allURLs.length"><div v-for="(url, index) in allURLs" :key="url" class="source-filter-row"><label class="check"><input v-if="form.aggregate" type="checkbox" :checked="!form.excludedURLs.includes(url)" @change="setSourceEnabled(url, $event.target.checked)"><input v-else type="checkbox" :checked="index === 0" disabled><span>订阅 {{ index + 1 }}{{ form.aggregate && form.excludedURLs.includes(url) ? '（已停用）' : '' }}</span></label><select :value="sourceFilterValue(url)" :disabled="!sourceFilterID(url) || sourcesLoading" :aria-label="'订阅 ' + (index + 1) + ' HTTP/SOCKS 过滤'" @change="setSourceFilter(url, $event.target.value)"><option value="">HTTP/SOCKS：跟随默认</option><option value="true">HTTP/SOCKS：丢弃</option><option value="false">HTTP/SOCKS：保留</option></select></div></div>
              <div class="input-footer"><label class="check"><input id="aggregate" v-model="form.aggregate" type="checkbox">聚合多个订阅</label><button id="refreshSources" class="text-button" :disabled="sourcesLoading" @click="readSources(true)">{{ sourcesLoading ? '正在读取…' : '重新读取订阅' }} <span>↻</span></button></div>
              <div id="sourceList" class="source-list" v-if="sources.length"><div v-for="source in sources" :key="source.id" class="source-item"><div><strong>{{ source.name }}</strong><small>{{ source.node_count }} 个缓存节点{{ source.last_success ? ' · ' + new Date(source.last_success).toLocaleString() : '' }}</small><small v-if="source.error" class="error">{{ source.error }}</small></div><span class="tag" :class="source.state">{{ stateLabels[source.state] || source.state }}</span><button class="text-button" :disabled="!source.node_count" @click="openSource(source)">预览</button></div></div>
              <div class="separator"></div>
              <details class="direct-import" open><summary>直接节点链接 <span class="subtle">SS / TUIC / VLESS / VMess / Trojan / Hysteria2 / AnyTLS</span></summary><textarea id="uriText" v-model="form.directLinks" rows="3" spellcheck="false" placeholder="anytls://…&#10;trojan://…&#10;每行一个节点链接，可混合多种协议"></textarea><div class="input-footer"><span id="uriSummary" class="hint">{{ uriSummary }}</span><div class="actions"><button id="previewURI" class="text-button" @click="parseLinks(true).catch(() => {})">重新解析</button><button id="clearURI" class="text-button" @click="form.directLinks = ''">清空</button></div></div><div id="uriResults" class="import-results"><div v-for="line in imported" :key="line.line" class="import-row"><span><small>{{ line.line }}</small>{{ line.error ? '解析失败' : line.name }}</span><span :class="{ error: line.error }">{{ line.error || line.type }}</span><label v-if="!line.error" class="check"><input v-model="selfHostedIDs" type="checkbox" :value="line.node_id" :aria-label="line.name + ' 自建（0 倍率）'">自建（0 倍率）</label></div></div></details>
              <button id="yamlPreview" class="text-button yaml-button" @click="openYAML">本地 YAML 预览 ↗</button>
            </section>

            <section id="nodes" class="card"><div class="card-heading"><span class="section-number">02</span><div><h2>节点总览</h2><p>倍率先识别，国旗和名称自动整理。</p></div><button id="previewAggregate" class="icon-button" @click="aggregateNodes" aria-label="更新聚合预览">↻</button></div><label class="check"><input id="dropPlaintext" v-model="form.dropPlaintext" type="checkbox">默认丢弃 HTTP/SOCKS 节点</label><p class="hint">仅按类型去除 HTTP、SOCKS4、SOCKS5（包含启用 TLS 的变体），保留 VLESS 等其他协议。每个订阅可单独覆盖默认设置；修改后需重新生成订阅。</p><p id="aggregateSummary" class="hint">{{ aggregateSummary }}</p><div id="aggregateResults"><NodeTable :nodes="nodes" /></div></section>

            <section id="exit" class="card"><div class="card-heading"><span class="section-number">03</span><div><h2>AI 出口策略</h2><p>内置服务自动分流，额外域名按需补充。</p></div><span class="pill purple">固定出口</span></div>
              <label>AI 出口（可多选）</label><AIExitPicker v-model="form.aiNodes" :groups="aiChoices" :disabled="!ready" />
              <div class="switch-setting"><label class="check"><input id="aiChain" v-model="form.aiChain" type="checkbox">AI 链式代理</label><small>前置自动测速优选，也可手动指定节点</small></div>
              <div v-if="form.aiChain" class="test-url-setting"><label for="aiTestURL">前置测速 URL <span class="optional">可选</span></label><input id="aiTestURL" v-model="form.aiTestURL" spellcheck="false" placeholder="留空：首个出口的 https://服务器/ 地址"><p class="hint">测速在客户端进行。首个 VPS 必须提供可访问的 HTTP(S) 服务；协议端口不等于网页服务，可改填实际的健康检查 URL。</p><p v-if="aiEntryTestURL" class="hint">生成的测速 URL：{{ aiEntryTestURL }}</p></div>
              <p v-if="aiMissing" class="error hint">所选出口已不可用；重新导入对应节点后才能生成。</p>
              <ChainPreview :name="chosenAI?.name || (aiMissing ? '所选节点不可用' : '')" :chain="form.aiChain" :count="form.aiNodes.length" />
              <p v-if="form.aiChain" class="hint">最终出口仅限勾选节点，可在客户端切换。TUIC/Hysteria2 需要支持 UDP 转发的前置节点。</p>
              <div class="service-chips"><span>ChatGPT / Codex</span><span>Claude</span><span>Gemini</span><span>Copilot</span><span>Cursor</span><span>更多 AI 服务</span></div>
              <label for="aiDomains">额外 AI 服务域名 <span class="optional">可选</span></label><textarea id="aiDomains" v-model="form.aiDomains" rows="6" spellcheck="false" :placeholder="aiPlaceholder"></textarea><p class="hint">已有内置规则无需填写；留空即可。这里只添加你的中转站或其他额外服务，每行一个域名。</p><details class="builtin-rules"><summary>查看全部内置 AI 规则 <span>{{ builtinAIRules.length }} 条</span></summary><pre>{{ builtinAIRules.join('\n') }}</pre></details>
              <div class="info-strip">🏠 电报使用自建出口：标记节点后，在客户端「电报消息 / 电报信息」组选择「自建」，即可使用 0 倍率路线。</div>
            </section>

            <section id="rules" class="card"><div class="card-heading"><span class="section-number">04</span><div><h2>规则与分组</h2><p>选择分流方案，高级配置按需展开。</p></div></div><label for="rulePreset">ACL4SSR 规则方案</label><select id="rulePreset" v-model="form.preset" :disabled="!ready"><option v-for="preset in presets" :key="preset.id" :value="preset.id">{{ preset.name }}</option></select><p id="presetDescription" class="hint">{{ selectedPreset?.name }}</p>
              <div class="update-box"><div><span class="live-dot"></span><strong>规则库</strong><p id="ruleUpdateStatus">{{ ruleStateText }}</p><small v-if="ruleStatus?.last_checked">上次检查 {{ new Date(ruleStatus.last_checked).toLocaleString() }}</small><small v-if="ruleStatus?.next_check"> · 下次 {{ new Date(ruleStatus.next_check).toLocaleString() }}</small><p v-if="ruleStatus?.error" class="error">更新失败，保留当前有效规则：{{ ruleStatus.error }}</p></div><button id="updateRules" class="secondary" :disabled="!ruleStatus || ruleStatus.state === 'updating'" @click="updateRules">{{ ruleStatus?.state === 'updating' ? '正在校验…' : '检查更新' }}</button></div>
              <div id="advancedOptions" v-show="advanced" class="advanced-options">
                <div class="separator"></div><h3>倍率与重命名</h3><label for="ratePattern">倍率识别规则</label><input id="ratePattern" v-model="form.ratePattern" spellcheck="false" placeholder="留空识别 倍率:0.5 / 0.5x / 0.5倍"><p class="hint">第一个正则捕获组需为数字。手动自建标记优先，未识别节点归标准组。</p>
                <div id="renameRules"><div v-for="(rule, index) in form.renameRules" :key="index" class="rename-row"><input v-model="rule.match" aria-label="匹配内容" placeholder="匹配内容"><input v-model="rule.replacement" aria-label="替换为" placeholder="替换为"><select v-model="rule.regex" aria-label="替换方式"><option :value="false">文本</option><option :value="true">正则</option></select><button class="icon-button" aria-label="移除重命名规则" @click="form.renameRules.splice(index, 1)">×</button></div></div><button id="addRename" class="text-button" :disabled="!ready" @click="form.renameRules.push({ match: '', replacement: '', regex: false })">＋ 添加替换规则</button>
                <details class="advanced-editor"><summary>自定义代理组</summary><label for="groupSettings">代理组 JSON</label><textarea id="groupSettings" v-model="form.groupsJSON" class="code" spellcheck="false"></textarea><p class="hint">可使用 proxies、node_ids、filter、source_ids、rate_groups（self / low / standard / high）。同名模板组以此为准；AI 与倍率组受保护。</p></details>
                <details class="advanced-editor"><summary>自定义分流规则</summary><label for="customRules">额外规则</label><textarea id="customRules" v-model="form.customRules" class="code" spellcheck="false" placeholder="DOMAIN-SUFFIX,example.com,DIRECT"></textarea><label for="policyMap">模板策略映射 <span class="optional">可选</span></label><textarea id="policyMap" v-model="form.policyMap" class="code" spellcheck="false"></textarea><label for="ruleFallback">最终兜底</label><input id="ruleFallback" v-model="form.fallback" placeholder="留空使用自定义 MATCH、模板 FINAL 或普通选择"><label class="check hint"><input id="skipUnsupportedRules" v-model="form.skipUnsupported" type="checkbox">跳过外部 URL-REGEX / USER-AGENT</label><button id="previewRules" class="secondary" :disabled="!ready || rulePreviewing" @click="previewRules">{{ rulePreviewing ? '正在预览…' : '预览本地规则' }}</button><p id="ruleSummary" class="hint">{{ ruleSummary || '顺序：自定义规则 → 外部规则 → 最终兜底。AI 固定规则优先。' }}</p><textarea v-if="rulesText" id="ruleResults" class="code" :value="rulesText" readonly aria-label="最终规则顺序"></textarea></details>
              </div>
              <div class="save-row"><button id="saveOptions" class="secondary" :disabled="!ready || saving" @click="saveOptions">{{ saving ? '正在保存…' : '保存转换设置' }}</button><small>保存重命名、倍率、AI 出口和代理组；其他输入随生成地址保存。</small></div>
            </section>
          </div>

          <aside class="preview-column">
            <section class="card overview"><div class="card-heading"><div><div class="eyebrow">AT A GLANCE</div><h2>这次转换</h2></div><span class="live-dot"></span></div><NodeDistribution :nodes="preview?.nodes || nodes" /><div class="overview-row"><span>已启用来源</span><strong>{{ selectedURLs.length }}</strong></div><div class="overview-row"><span>合并重复节点</span><strong>{{ aggregation?.duplicate_count || 0 }}</strong></div><div class="overview-row"><span>最终出口</span><strong class="truncate">{{ aiMissing ? '含不可用节点' : form.aiNodes.length > 1 ? form.aiNodes.length + ' 个指定出口' : chosenAI?.name || '跟随方案' }}</strong></div><div class="overview-row"><span>AI 连接方式</span><strong>{{ form.aiChain ? '链式代理' : '直接连接出口' }}</strong></div><div class="overview-row"><span>客户端</span><strong>Mihomo</strong></div><p class="hint">普通流量优先低倍率；高倍率与自建出口可在客户端手动选择。</p></section>
            <section id="result" class="card result-card"><div class="card-heading"><div><div class="eyebrow">READY TO CONNECT</div><h2>生成与导出</h2></div><span class="result-symbol">↗</span></div><p id="generationSummary" class="hint" role="status">{{ generationSummary }}</p><label class="check"><input id="refreshGeneration" v-model="form.refreshGeneration" type="checkbox">生成前刷新来源</label><button id="generate" class="primary generate-button" :disabled="!ready || generating" @click="generate()">{{ generating ? '正在生成…' : '生成订阅' }} <span v-if="!generating">↗</span></button><button id="previewGeneration" class="secondary full-width" :disabled="!ready || previewing" @click="generate(true)">{{ previewing ? '正在预览…' : '预览分组与规则' }}</button><label for="convertedUrl">订阅地址</label><input id="convertedUrl" :value="downloadURL" readonly placeholder="生成后显示订阅地址"><div class="export-actions"><button id="copyUrl" class="secondary" :disabled="!generation" @click="copyURL">复制</button><button id="downloadConfig" class="secondary" :disabled="!generation" @click="downloadConfig">下载 YAML</button><button id="importClient" class="secondary" :disabled="!generation" @click="importClient">导入客户端</button></div><label class="check hint"><input id="refreshDownload" v-model="refreshDownload" type="checkbox" :disabled="!generation">客户端更新时刷新来源</label><p class="hint">地址固定本次设置，修改后请重新生成。</p></section>
            <section v-if="preview" id="generationDetails" class="card"><h2>实际生成结果</h2><p id="generationCounts" class="hint">{{ preview.nodes.length }} 个节点 · 丢弃 HTTP/SOCKS {{ preview.filtered_count || 0 }} 个 · {{ preview.groups.length }} 个代理组 · {{ preview.rule_count }} 条规则 · 兼容跳过 {{ preview.skipped_rules.length }} 条</p><div id="generatedGroups" class="group-list"><details v-for="group in preview.groups" :key="group.name"><summary>{{ group.name }}<span>{{ group.type }}</span></summary><div class="group-members"><span v-for="member in group.proxies" :key="member">{{ member }}</span></div></details></div></section>
          </aside>
        </div>
        <footer>clash_ACL <span>·</span> 每一次转换，都由你掌控。</footer>
      </main>
    </div>
    <dialog ref="dialog" id="previewDialog"><div class="dialog-heading"><h2 id="previewTitle">{{ dialogTitle }}</h2><button id="closePreview" class="icon-button" @click="dialog.close()" aria-label="关闭预览">×</button></div><p class="hint">仅展示名称和协议，认证参数不公开。</p><div v-if="showYAML" id="yamlFormArea"><label for="yamlText">含 proxies 的 YAML 配置</label><textarea id="yamlText" v-model="yamlText" class="code" spellcheck="false"></textarea><button id="parseYaml" class="primary" :disabled="yamlBusy" @click="parseYAML">解析预览</button><p id="previewMessage" class="hint">{{ yamlSummary }}</p></div><NodeTable :nodes="showYAML ? yamlNodes : dialogNodes" /></dialog>
  </div>
</template>
