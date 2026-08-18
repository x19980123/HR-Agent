<script setup lang="ts">
import { ref, onMounted, h } from "vue";
import {
  NButton,
  NCard,
  NCheckbox,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NSpace,
  NText,
  NUpload,
  useDialog,
  type DataTableColumns,
} from "naive-ui";
import {
  deleteQuestionBank,
  getQuestionBankItem,
  listQuestionBank,
  reindexQuestionBank,
  batchQuestionBankCsv,
  saveQuestionBank,
  type QuestionBankItem,
} from "@/api/admin";
import type { UploadFileInfo } from "naive-ui";
import { useNotify } from "@/composables/useNotify";

const notify = useNotify();
const dialog = useDialog();

const loading = ref(false);
const items = ref<QuestionBankItem[]>([]);
const showModal = ref(false);
const editing = ref<QuestionBankItem | null>(null);
const csvFiles = ref<UploadFileInfo[]>([]);
const reindexAfterBatch = ref(true);

const form = ref({
  title: "",
  category: "other",
  difficulty: "medium",
  tags: "",
  content: "",
  enabled: true,
});

const catOptions = ["algorithm", "system_design", "fundamentals", "behavioral", "other"].map((v) => ({
  label: v,
  value: v,
}));
const diffOptions = ["easy", "medium", "hard"].map((v) => ({ label: v, value: v }));

const columns: DataTableColumns<QuestionBankItem> = [
  { title: "标题", key: "title" },
  { title: "分类", key: "category" },
  { title: "难度", key: "difficulty" },
  { title: "状态", key: "enabled", render: (row) => (row.enabled ? "启用" : "停用") },
  {
    title: "操作",
    key: "op",
    render(row) {
      return h(NSpace, { size: 4 }, () => [
        h(NButton, { size: "small", onClick: () => openEdit(row.id) }, () => "编辑"),
        h(
          NButton,
          { size: "small", onClick: () => onDelete(row.id) },
          () => "删除",
        ),
      ]);
    },
  },
];

async function load() {
  loading.value = true;
  try {
    items.value = (await listQuestionBank()).items || [];
  } finally {
    loading.value = false;
  }
}

function openNew() {
  editing.value = null;
  form.value = { title: "", category: "other", difficulty: "medium", tags: "", content: "", enabled: true };
  showModal.value = true;
}

async function openEdit(id: string) {
  const item = await getQuestionBankItem(id);
  editing.value = item;
  form.value = {
    title: item.title || "",
    category: item.category || "other",
    difficulty: item.difficulty || "medium",
    tags: (item.tags || []).join(", "),
    content: item.content || "",
    enabled: item.enabled !== false,
  };
  showModal.value = true;
}

async function save() {
  if (!form.value.content.trim()) {
    notify.warning("正文不能为空");
    return;
  }
  const payload = {
    title: form.value.title.trim(),
    category: form.value.category,
    difficulty: form.value.difficulty,
    tags: form.value.tags.split(/[,，]/).map((t) => t.trim()).filter(Boolean),
    content: form.value.content.trim(),
    enabled: form.value.enabled,
  };
  try {
    await saveQuestionBank(payload, editing.value?.id);
    notify.success("已保存");
    showModal.value = false;
    await load();
  } catch (e) {
    notify.from(e, "失败");
  }
}

function onDelete(id: string) {
  dialog.warning({
    title: "删除题目",
    content: "确认删除？",
    positiveText: "删除",
    onPositiveClick: async () => {
      await deleteQuestionBank(id);
      notify.success("已删除");
      await load();
    },
  });
}

async function reindex() {
  try {
    const res = await reindexQuestionBank();
    notify.success(`已同步 ${res.synced_items || res.upserted || 0} 条`);
    await load();
  } catch (e) {
    notify.from(e, "同步失败");
  }
}

async function uploadBatch() {
  const f = csvFiles.value[0]?.file;
  if (!f) {
    notify.warning("请选择 CSV 文件");
    return;
  }
  try {
    const res = await batchQuestionBankCsv(f, reindexAfterBatch.value);
    notify.success(`批量入库：成功 ${res.succeeded ?? 0}，失败 ${res.failed ?? 0}`);
    csvFiles.value = [];
    await load();
  } catch (e) {
    notify.from(e, "批量失败");
  }
}

onMounted(load);
</script>

<template>
  <NSpace justify="space-between" style="margin-bottom: 1rem">
    <div>
      <h2 style="margin: 0">题库 RAG</h2>
      <NText depth="3">MySQL 权威 + 向量库检索</NText>
    </div>
    <NSpace>
      <NButton @click="reindex">全量同步向量库</NButton>
      <NButton type="primary" @click="openNew">新建题目</NButton>
    </NSpace>
  </NSpace>
  <NCard style="margin-bottom: 1rem">
    <NText depth="3" style="display: block; margin-bottom: 0.5rem">
      批量 CSV 列：<code>title, category, content, difficulty, tags, enabled, jd_id</code>（后四列可空；enabled 用 1/0）
    </NText>
    <NSpace align="center">
      <NUpload v-model:file-list="csvFiles" :max="1" accept=".csv">
        <NButton>选择 CSV</NButton>
      </NUpload>
      <NCheckbox v-model:checked="reindexAfterBatch">导入后全量同步向量库</NCheckbox>
      <NButton type="primary" @click="uploadBatch">批量导入</NButton>
    </NSpace>
  </NCard>
  <NCard>
    <NDataTable :loading="loading" :columns="columns" :data="items" />
  </NCard>

  <NModal v-model:show="showModal" preset="card" :title="editing ? '编辑题目' : '新建题目'" style="width: min(520px, 92vw)">
    <NForm label-placement="top">
      <NFormItem label="标题"><NInput v-model:value="form.title" /></NFormItem>
      <NFormItem label="分类"><NSelect v-model:value="form.category" :options="catOptions" /></NFormItem>
      <NFormItem label="难度"><NSelect v-model:value="form.difficulty" :options="diffOptions" /></NFormItem>
      <NFormItem label="标签"><NInput v-model:value="form.tags" placeholder="逗号分隔" /></NFormItem>
      <NFormItem label="正文">
        <NInput v-model:value="form.content" type="textarea" :autosize="{ minRows: 6, maxRows: 12 }" />
      </NFormItem>
      <NCheckbox v-model:checked="form.enabled">启用（写入向量库）</NCheckbox>
    </NForm>
    <template #footer>
      <NSpace justify="end">
        <NButton @click="showModal = false">取消</NButton>
        <NButton type="primary" @click="save">保存</NButton>
      </NSpace>
    </template>
  </NModal>
</template>
