<script setup lang="ts">
import { ref, onMounted } from "vue";
import { useRouter } from "vue-router";
import { NCard, NGrid, NGi, NStatistic, NButton, NSpace, NText } from "naive-ui";
import { api, type AdminStats } from "@/api/client";
import { STATUS_LABEL } from "@/constants/status";

const router = useRouter();
const stats = ref<AdminStats | null>(null);
const loading = ref(false);

async function load() {
  loading.value = true;
  try {
    stats.value = await api<AdminStats>("/v1/admin/stats");
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div>
    <NSpace justify="space-between" align="center" style="margin-bottom: 1rem">
      <div>
        <h2 style="margin: 0 0 0.25rem">Agent 工作台</h2>
        <NText depth="3">流水线：解析 Agent → 筛选 Agent → 约面 Agent → 出题 Agent → Offer</NText>
      </div>
      <NSpace>
        <NButton @click="router.push({ name: 'human' })">人工接管</NButton>
        <NButton type="primary" @click="router.push({ name: 'applications' })">申请流水线</NButton>
        <NButton :loading="loading" @click="load">刷新</NButton>
      </NSpace>
    </NSpace>

    <NGrid v-if="stats" cols="2 s:3 m:4" :x-gap="12" :y-gap="12">
      <NGi><NCard size="small"><NStatistic label="全部申请" :value="stats.total || 0" /></NCard></NGi>
      <NGi><NCard size="small"><NStatistic label="近 7 日新建" :value="stats.created_last_7d || 0" /></NCard></NGi>
      <NGi><NCard size="small"><NStatistic label="待候选人回复" :value="stats.awaiting_reply || 0" /></NCard></NGi>
      <NGi><NCard size="small"><NStatistic label="需人工接管" :value="stats.needs_human || 0" /></NCard></NGi>
      <NGi><NCard size="small"><NStatistic label="已确认面试" :value="stats.confirmed || 0" /></NCard></NGi>
      <NGi><NCard size="small"><NStatistic label="筛选未通过" :value="stats.rejected || 0" /></NCard></NGi>
    </NGrid>

    <NCard v-if="stats?.by_status" title="状态分布" size="small" style="margin-top: 1rem">
      <div v-for="(v, k) in stats.by_status" :key="k" style="display: flex; justify-content: space-between; padding: 0.35rem 0">
        <span>{{ STATUS_LABEL[k] || k }}</span>
        <strong>{{ v }}</strong>
      </div>
    </NCard>
  </div>
</template>
