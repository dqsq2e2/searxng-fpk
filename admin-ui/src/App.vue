<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { apiRequest } from './api'

type ConnectionState = 'connecting' | 'connected' | 'failed'

interface StatusResponse {
  status: string
  serviceUrl: string
}

const serviceAddress = ref('http://设备地址:8080')
const notice = ref('当前为前端骨架，保存接口将在管理后端接入后启用。')
const busyAction = ref('')
const connectionState = ref<ConnectionState>('connecting')

const connectionText = computed(() => ({
  connecting: '管理服务连接中',
  connected: '管理服务已连接',
  failed: '管理服务连接失败',
})[connectionState.value])

const groups = [
  { title: '品牌化', description: '站点名称、基础 URL、主题与 Logo。', icon: '品' },
  { title: '搜索设置', description: '默认语言、结果数量、安全搜索与自动补全。', icon: '搜' },
  { title: '界面设置', description: '主题、查询格式与用户界面偏好。', icon: '界' },
  { title: '外发代理', description: '代理地址、超时与网络访问策略。', icon: '网' },
]

async function runAction(action: string, endpoint: string) {
  busyAction.value = action
  notice.value = `正在${action}…`

  try {
    await apiRequest(endpoint, { method: 'POST' })
    notice.value = `${action}请求已提交。`
  } catch (error) {
    notice.value = error instanceof Error ? error.message : `${action}失败。`
  } finally {
    busyAction.value = ''
  }
}

async function checkConnection() {
  connectionState.value = 'connecting'
  notice.value = '正在检查管理服务连接…'

  try {
    const status = await apiRequest<StatusResponse>('status')
    serviceAddress.value = status.serviceUrl
    connectionState.value = 'connected'
    notice.value = '管理服务连接正常。'
  } catch (error) {
    connectionState.value = 'failed'
    notice.value = error instanceof Error ? error.message : '管理服务连接失败。'
  }
}

onMounted(checkConnection)
</script>

<template>
  <main class="shell">
    <header class="hero">
      <div>
        <p class="eyebrow">飞牛 FPK · 管理控制台</p>
        <h1>SearXNG 配置管理</h1>
        <p class="subtitle">集中管理搜索服务的全局配置、引擎与配置文件。</p>
      </div>
      <span class="status" :class="connectionState"><i></i> {{ connectionText }}</span>
    </header>

    <section class="service-card">
      <div>
        <span class="label">SearXNG 服务地址</span>
        <strong>{{ serviceAddress }}</strong>
        <small>主服务保持原生 IP:端口访问方式</small>
      </div>
      <button class="secondary" type="button" :disabled="connectionState === 'connecting'" @click="checkConnection">
        {{ connectionState === 'connecting' ? '检查中…' : '检查连接' }}
      </button>
    </section>

    <section class="section-heading">
      <div>
        <p class="eyebrow">配置概览</p>
        <h2>全局设置</h2>
      </div>
      <button class="primary" type="button" :disabled="busyAction !== ''" @click="runAction('保存配置', 'config/save')">
        保存配置
      </button>
    </section>

    <section class="group-grid">
      <article v-for="group in groups" :key="group.title" class="group-card">
        <span class="group-icon">{{ group.icon }}</span>
        <div>
          <h3>{{ group.title }}</h3>
          <p>{{ group.description }}</p>
        </div>
        <button type="button" class="text-button" @click="notice = `${group.title}表单待后续接入。`">配置 →</button>
      </article>
    </section>

    <section class="tools">
      <div class="section-heading compact">
        <div>
          <p class="eyebrow">维护工具</p>
          <h2>引擎与配置文件</h2>
        </div>
      </div>
      <div class="action-grid">
        <button type="button" @click="notice = '引擎管理页面待后续接入。'"><b>引擎管理</b><span>启用、禁用或添加搜索引擎</span></button>
        <button type="button" @click="notice = '请选择后端支持的配置文件进行导入。'"><b>导入配置</b><span>从备份恢复 settings.yml</span></button>
        <button type="button" @click="runAction('导出配置', 'config/export')"><b>导出配置</b><span>下载当前配置与历史版本</span></button>
        <button class="danger" type="button" :disabled="busyAction !== ''" @click="runAction('应用并重启', 'service/apply')"><b>应用并重启</b><span>校验配置后重启 SearXNG</span></button>
      </div>
    </section>

    <footer class="notice" aria-live="polite">{{ notice }}</footer>
  </main>
</template>
