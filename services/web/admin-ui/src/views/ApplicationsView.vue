<script setup lang="ts">
import { ref, onMounted, h } from "vue";
import { useRouter } from "vue-router";
import {
  NButton,
  NCard,
  NDataTable,
  NSpace,
  NText,
  type DataTableColumns,
} from "naive-ui";
import { listApplications, type ApplicationSummary } from "@/api/admin";
import StatusTag from "@/components/StatusTag.vue";
import { STATUS_LABEL, agentPipeline } from "@/constants/status";
import { fmtTime } from "@/utils/format";

const router = useRouter();
const loading = ref(false);
const items = ref<ApplicationSummary[]>([]);
const statusFilter = ref("");

const filters = ["", "awaiting_reply", "confirmed", "declined", "needs_human", "rejected"];

const columns: DataTableColumns<ApplicationSummary> = [
  {
    title: "候选人",
    key: "candidate_name",
    render(row) {
      return h(
        "a",
        {
          href: "#",
          style: "color: var(--accent); font-weight: 600",
          onClick: (e: Event) => {
            e.preventDefault();
            router.push({ name: "application-detail", params: { id: row.id } });
          },
        },
        (row.candidate_name || "未命名").trim() || row.id.slice(0, 8),
      );
    },
  },
  { title: "邮箱", key: "candidate_email", ellipsis: { tooltip: true } },
  {
    title: "状态",
    key: "status",
    width: 110,
    render: (row) => h(StatusTag, { status: row.status }),
  },
  {
    title: "Agent 阶段",
    key: "stage",
    width: 120,
    render: (row) => agentPipeline(row).shortLabel,
  },
  { title: "岗位", key: "jd_id", width: 140 },
  {
    title: "更新",
    key: "updated_at",
    width: 170,
    render: (row) => fmtTime(row.updated_at),
  },
];

async function load() {
  loading.value = true;
  try {
    const data = await listApplications(statusFilter.value || undefined);
    items.value = data.items || [];
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
        <h2 style="margin: 0">申请流水线</h2>
        <NText depth="3">候选人全流程；详情页可看 Agent 进度轨</NText>
      </div>
      <NButton type="primary" @click="router.push({ name: 'create' })">新建申请</NButton>
    </NSpace>

    <NCard>
      <NSpace :size="8" style="margin-bottom: 1rem" wrap>
        <NButton
          v-for="f in filters"
          :key="f || 'all'"
          :type="statusFilter === f ? 'primary' : 'default'"
          size="small"
          @click="statusFilter = f; load()"
        >
          {{ f ? STATUS_LABEL[f] || f : "全部" }}
        </NButton>
      </NSpace>
      <NDataTable :loading="loading" :columns="columns" :data="items" :bordered="false" />
    </NCard>
  </div>
</template>
