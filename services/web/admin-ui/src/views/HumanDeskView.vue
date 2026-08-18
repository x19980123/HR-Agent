<script setup lang="ts">
import { ref, onMounted, h } from "vue";
import { useRouter } from "vue-router";
import { NButton, NCard, NDataTable, NSpace, NText, useDialog, type DataTableColumns } from "naive-ui";
import {
  humanApprove,
  listApplications,
  listApplicationsByErrorKind,
  retryParse,
  type ApplicationSummary,
} from "@/api/admin";
import StatusTag from "@/components/StatusTag.vue";
import { canHumanApproveApp, canRetryParse } from "@/constants/status";
import { fmtTime } from "@/utils/format";
import { useNotify } from "@/composables/useNotify";

const router = useRouter();
const notify = useNotify();
const dialog = useDialog();

const loading = ref(false);
const humanItems = ref<ApplicationSummary[]>([]);
const systemItems = ref<ApplicationSummary[]>([]);

function mergeHumanLists(lists: ApplicationSummary[][]) {
  const map = new Map<string, ApplicationSummary>();
  for (const list of lists) {
    for (const a of list) map.set(a.id, a);
  }
  return [...map.values()].sort((a, b) =>
    String(b.updated_at).localeCompare(String(a.updated_at)),
  );
}

async function load() {
  loading.value = true;
  try {
    const [nh, rj, fl, sys] = await Promise.all([
      listApplications("needs_human"),
      listApplications("rejected"),
      listApplications("failed"),
      listApplicationsByErrorKind("system"),
    ]);
    humanItems.value = mergeHumanLists([nh.items || [], rj.items || [], fl.items || []]);
    systemItems.value = sys.items || [];
  } finally {
    loading.value = false;
  }
}

async function doRetry(row: ApplicationSummary) {
  try {
    await retryParse(row.id);
    notify.success("已重试解析");
    await load();
  } catch (e) {
    notify.from(e, "失败");
  }
}

function doApprove(row: ApplicationSummary) {
  dialog.warning({
    title: "人工通过",
    content: "确认人工通过并约面？",
    positiveText: "确认",
    onPositiveClick: async () => {
      try {
        await humanApprove(row.id);
        notify.success("已人工通过");
        await load();
      } catch (e) {
        notify.from(e, "失败");
      }
    },
  });
}

const humanColumns: DataTableColumns<ApplicationSummary> = [
  {
    title: "候选人",
    key: "name",
    render: (row) =>
      h(
        NButton,
        { text: true, type: "primary", onClick: () => router.push({ name: "application-detail", params: { id: row.id } }) },
        () => (row.candidate_name || "未命名").trim() || row.id.slice(0, 8),
      ),
  },
  { title: "邮箱", key: "candidate_email" },
  {
    title: "状态",
    key: "status",
    render: (row) => h(StatusTag, { status: row.status }),
  },
  { title: "更新", key: "updated_at", render: (row) => fmtTime(row.updated_at) },
  {
    title: "操作",
    key: "actions",
    render(row) {
      return h(NSpace, { size: 4 }, () => [
        canRetryParse(row.status)
          ? h(NButton, { size: "small", onClick: () => doRetry(row) }, () => "重试解析")
          : null,
        canHumanApproveApp(row)
          ? h(NButton, { size: "small", type: "primary", onClick: () => doApprove(row) }, () => "人工通过")
          : null,
      ]);
    },
  },
];

const systemColumns: DataTableColumns<ApplicationSummary> = [
  {
    title: "候选人",
    key: "name",
    render: (row) =>
      h(
        NButton,
        { text: true, type: "primary", onClick: () => router.push({ name: "application-detail", params: { id: row.id } }) },
        () => (row.candidate_name || row.id.slice(0, 8)),
      ),
  },
  { title: "错误码", key: "system_error_code", render: (row) => row.system_error_code || "-" },
  { title: "状态", key: "status", render: (row) => h(StatusTag, { status: row.status }) },
  { title: "更新", key: "updated_at", render: (row) => fmtTime(row.updated_at) },
];

onMounted(load);
</script>

<template>
  <div>
    <NSpace justify="space-between" style="margin-bottom: 1rem">
      <div>
        <h2 style="margin: 0">人工接管</h2>
        <NText depth="3">Agent 转人工 / 未通过 / 失败；系统异常不可人工通过</NText>
      </div>
      <NButton :loading="loading" @click="load">刷新</NButton>
    </NSpace>

    <NCard title="待人工处理" style="margin-bottom: 1rem">
      <NDataTable :loading="loading" :columns="humanColumns" :data="humanItems" />
    </NCard>

    <NCard title="系统异常队列">
      <NDataTable :loading="loading" :columns="systemColumns" :data="systemItems" />
    </NCard>
  </div>
</template>
