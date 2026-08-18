<script setup lang="ts">
import { ref, onMounted, h } from "vue";
import { useRouter } from "vue-router";
import { NButton, NCard, NDataTable, NSpace, NText, type DataTableColumns } from "naive-ui";
import { listJds, type JDItem } from "@/api/admin";
import { fmtTime } from "@/utils/format";

const router = useRouter();
const loading = ref(false);
const items = ref<JDItem[]>([]);

const columns: DataTableColumns<JDItem> = [
  {
    title: "岗位",
    key: "title",
    render: (row) =>
      h(
        NButton,
        { text: true, type: "primary", onClick: () => router.push({ name: "jd-detail", params: { id: row.id } }) },
        () => row.title || row.id,
      ),
  },
  { title: "部门", key: "department" },
  { title: "薪资", key: "salary" },
  { title: "地点", key: "location" },
  { title: "创建", key: "created_at", render: (row) => fmtTime(row.created_at) },
];

async function load() {
  loading.value = true;
  try {
    items.value = (await listJds()).items || [];
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <NSpace justify="space-between" style="margin-bottom: 1rem">
    <div>
      <h2 style="margin: 0">岗位 JD</h2>
      <NText depth="3">点击进入编辑</NText>
    </div>
    <NButton type="primary" @click="router.push({ name: 'jd-detail', params: { id: 'new' } })">
      新建岗位
    </NButton>
  </NSpace>
  <NCard>
    <NDataTable :loading="loading" :columns="columns" :data="items" />
  </NCard>
</template>
