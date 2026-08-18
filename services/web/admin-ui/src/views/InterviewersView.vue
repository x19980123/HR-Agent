<script setup lang="ts">
import { ref, onMounted, h, computed } from "vue";
import {
  NAlert,
  NButton,
  NCard,
  NCheckbox,
  NCheckboxGroup,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NSpace,
  NText,
  useDialog,
  type DataTableColumns,
} from "naive-ui";
import {
  deleteInterviewerPool,
  listInterviewerPools,
  listInterviewers,
  saveInterviewer,
  saveInterviewerPool,
  searchFeishuUsers,
  setInterviewerEnabled,
  type FeishuUserHit,
  type InterviewerPool,
  type InterviewerProfile,
} from "@/api/admin";
import { fmtTime } from "@/utils/format";
import { useNotify } from "@/composables/useNotify";

const notify = useNotify();
const dialog = useDialog();

const ROLE_OPTIONS = [
  { label: "HR", value: "hr" },
  { label: "技术 tech", value: "tech" },
  { label: "用人经理 hm", value: "hm" },
  { label: "跨部门 cross", value: "cross" },
  { label: "自定义 custom", value: "custom" },
];

const loading = ref(false);
const profiles = ref<InterviewerProfile[]>([]);
const pools = ref<InterviewerPool[]>([]);
const filterRole = ref<string | null>(null);
const filterDept = ref("");

const showProfileModal = ref(false);
const profileForm = ref({
  open_id: "",
  name: "",
  email: "",
  department: "",
  role_kinds: ["tech"] as string[],
  specialties_text: "",
  notes: "",
  enabled: true,
});
const editingOpenId = ref("");
const feishuQ = ref("");
const feishuHits = ref<FeishuUserHit[]>([]);
const searchingFeishu = ref(false);

const showPoolModal = ref(false);
const poolForm = ref({
  id: "",
  name: "",
  default_role_kind: "tech",
  department: "",
  notes: "",
  enabled: true,
  member_open_ids_text: "",
});

const profileOptions = computed(() =>
  profiles.value.map((p) => ({
    label: `${p.name || p.open_id} (${(p.role_kinds || []).join("/")})`,
    value: p.open_id,
  })),
);

async function load() {
  loading.value = true;
  try {
    const [p, poolsRes] = await Promise.all([
      listInterviewers({
        role_kind: filterRole.value || undefined,
        department: filterDept.value.trim() || undefined,
      }),
      listInterviewerPools(),
    ]);
    profiles.value = p.items || [];
    pools.value = poolsRes.items || [];
  } catch (e) {
    notify.from(e, "加载失败");
  } finally {
    loading.value = false;
  }
}

onMounted(load);

function openNewProfile() {
  editingOpenId.value = "";
  feishuQ.value = "";
  feishuHits.value = [];
  profileForm.value = {
    open_id: "",
    name: "",
    email: "",
    department: "",
    role_kinds: ["tech"],
    specialties_text: "",
    notes: "",
    enabled: true,
  };
  showProfileModal.value = true;
}

async function runFeishuSearch() {
  const q = feishuQ.value.trim();
  if (!q) {
    notify.warning("输入姓名或邮箱搜索飞书通讯录");
    return;
  }
  searchingFeishu.value = true;
  try {
    const out = await searchFeishuUsers(q);
    feishuHits.value = out.items || [];
    if (!feishuHits.value.length) notify.warning("未找到匹配用户");
  } catch (e) {
    notify.from(e, "搜索失败");
    feishuHits.value = [];
  } finally {
    searchingFeishu.value = false;
  }
}

function pickFeishuUser(u: FeishuUserHit) {
  profileForm.value.open_id = u.open_id || "";
  profileForm.value.name = u.name || profileForm.value.name;
  profileForm.value.email = u.email || "";
  profileForm.value.department = u.department || "";
  notify.success(`已填入 ${u.name || u.open_id}`);
}

function openEditProfile(row: InterviewerProfile) {
  editingOpenId.value = row.open_id;
  profileForm.value = {
    open_id: row.open_id,
    name: row.name || "",
    email: row.email || "",
    department: row.department || "",
    role_kinds: [...(row.role_kinds || ["tech"])],
    specialties_text: (row.specialties || []).join(", "),
    notes: row.notes || "",
    enabled: row.enabled !== false,
  };
  showProfileModal.value = true;
}

async function saveProfile() {
  const f = profileForm.value;
  if (!f.open_id.trim()) {
    notify.warning("open_id 必填（飞书用户标识）");
    return;
  }
  if (!f.role_kinds.length) {
    notify.warning("至少选择一种面试官角色分类");
    return;
  }
  try {
    await saveInterviewer({
      open_id: f.open_id.trim(),
      name: f.name.trim(),
      email: f.email.trim(),
      department: f.department.trim(),
      role_kinds: f.role_kinds,
      specialties: f.specialties_text
        .split(/[,，\s]+/)
        .map((x) => x.trim())
        .filter(Boolean),
      notes: f.notes.trim(),
      enabled: f.enabled,
    });
    notify.success("已保存面试官档案");
    showProfileModal.value = false;
    await load();
  } catch (e) {
    notify.from(e, "保存失败");
  }
}

function toggleProfile(row: InterviewerProfile, enabled: boolean) {
  dialog.warning({
    title: enabled ? "启用面试官" : "停用面试官",
    content: `${row.name || row.open_id}`,
    positiveText: "确认",
    onPositiveClick: async () => {
      try {
        await setInterviewerEnabled(row.open_id, enabled);
        notify.success("已更新");
        await load();
      } catch (e) {
        notify.from(e, "失败");
      }
    },
  });
}

function openNewPool() {
  poolForm.value = {
    id: "",
    name: "",
    default_role_kind: "tech",
    department: "",
    notes: "",
    enabled: true,
    member_open_ids_text: "",
  };
  showPoolModal.value = true;
}

function openEditPool(row: InterviewerPool) {
  poolForm.value = {
    id: row.id,
    name: row.name || "",
    default_role_kind: row.default_role_kind || "tech",
    department: row.department || "",
    notes: row.notes || "",
    enabled: row.enabled !== false,
    member_open_ids_text: (row.member_open_ids || []).join(", "),
  };
  showPoolModal.value = true;
}

async function savePool() {
  const f = poolForm.value;
  if (!f.name.trim()) {
    notify.warning("池名称必填");
    return;
  }
  try {
    await saveInterviewerPool({
      id: f.id || undefined,
      name: f.name.trim(),
      default_role_kind: f.default_role_kind,
      department: f.department.trim(),
      notes: f.notes.trim(),
      enabled: f.enabled,
      member_open_ids: f.member_open_ids_text
        .split(/[,，\s]+/)
        .map((x) => x.trim())
        .filter(Boolean),
    });
    notify.success("面试池已保存");
    showPoolModal.value = false;
    await load();
  } catch (e) {
    notify.from(e, "保存失败");
  }
}

function removePool(row: InterviewerPool) {
  dialog.warning({
    title: "删除面试池",
    content: `确认删除「${row.name}」？JD 中引用该池的需求需改配。`,
    positiveText: "删除",
    onPositiveClick: async () => {
      try {
        await deleteInterviewerPool(row.id);
        notify.success("已删除");
        await load();
      } catch (e) {
        notify.from(e, "失败");
      }
    },
  });
}

const profileColumns: DataTableColumns<InterviewerProfile> = [
  { title: "姓名", key: "name", width: 120 },
  { title: "open_id", key: "open_id", ellipsis: { tooltip: true } },
  { title: "部门", key: "department", width: 120 },
  {
    title: "角色分类",
    key: "role_kinds",
    width: 160,
    render: (row) => (row.role_kinds || []).join(", ") || "—",
  },
  {
    title: "特长",
    key: "specialties",
    width: 160,
    render: (row) => (row.specialties || []).join(", ") || "—",
  },
  {
    title: "状态",
    key: "enabled",
    width: 70,
    render: (row) => (row.enabled !== false ? "启用" : "停用"),
  },
  {
    title: "操作",
    key: "op",
    width: 160,
    render(row) {
      return h(NSpace, { size: 4 }, () => [
        h(NButton, { size: "small", onClick: () => openEditProfile(row) }, () => "编辑"),
        row.enabled !== false
          ? h(NButton, { size: "small", onClick: () => toggleProfile(row, false) }, () => "停用")
          : h(NButton, { size: "small", onClick: () => toggleProfile(row, true) }, () => "启用"),
      ]);
    },
  },
];

const poolColumns: DataTableColumns<InterviewerPool> = [
  { title: "名称", key: "name" },
  { title: "默认角色", key: "default_role_kind", width: 100 },
  { title: "部门", key: "department", width: 120 },
  {
    title: "成员数",
    key: "member_count",
    width: 80,
    render: (row) => row.member_count ?? row.member_open_ids?.length ?? 0,
  },
  {
    title: "更新",
    key: "updated_at",
    width: 140,
    render: (row) => fmtTime(row.updated_at),
  },
  {
    title: "操作",
    key: "op",
    width: 160,
    render(row) {
      return h(NSpace, { size: 4 }, () => [
        h(NButton, { size: "small", onClick: () => openEditPool(row) }, () => "编辑"),
        h(NButton, { size: "small", type: "error", secondary: true, onClick: () => removePool(row) }, () => "删除"),
      ]);
    },
  },
];
</script>

<template>
  <div>
    <NSpace justify="space-between" style="margin-bottom: 1rem" wrap>
      <div>
        <h2 style="margin: 0">面试官</h2>
        <NText depth="3" style="font-size: 0.85rem">
          按角色分类（hr / tech / hm / cross）维护档案与池；登录账号仍仅管理员/HR。
        </NText>
      </div>
      <NSpace>
        <NButton @click="load" :loading="loading">刷新</NButton>
        <NButton type="primary" @click="openNewProfile">新增档案</NButton>
        <NButton @click="openNewPool">新建面试池</NButton>
      </NSpace>
    </NSpace>

    <NAlert type="info" style="margin-bottom: 1rem">
      新增档案请优先「从飞书通讯录搜索」选真实用户，再补角色分类/特长。排期：固定人 → 池 → 档案。
    </NAlert>

    <NCard title="筛选档案" size="small" style="margin-bottom: 1rem">
      <NSpace align="center" wrap>
        <NSelect
          v-model:value="filterRole"
          clearable
          placeholder="角色分类"
          :options="ROLE_OPTIONS"
          style="width: 160px"
        />
        <NInput v-model:value="filterDept" placeholder="部门" clearable style="width: 160px" />
        <NButton type="primary" secondary @click="load">应用筛选</NButton>
      </NSpace>
    </NCard>

    <NCard title="面试官档案" style="margin-bottom: 1rem">
      <NDataTable :columns="profileColumns" :data="profiles" :loading="loading" :bordered="false" size="small" />
    </NCard>

    <NCard title="面试池">
      <NText depth="3" style="display: block; margin-bottom: 0.75rem">
        池可挂到 JD 轮次需求的 pool_id；成员须先有档案（保存池时会为未知 open_id 建最小档案）。
      </NText>
      <NDataTable :columns="poolColumns" :data="pools" :loading="loading" :bordered="false" size="small" />
    </NCard>

    <NModal v-model:show="showProfileModal" preset="card" title="面试官档案" style="width: 560px">
      <NForm label-placement="top">
        <div v-if="!editingOpenId" style="margin-bottom: 0.75rem; padding: 0.65rem; background: #f7f8fa; border-radius: 6px">
          <NText strong style="display: block; margin-bottom: 0.35rem">从飞书通讯录搜索</NText>
          <NSpace>
            <NInput
              v-model:value="feishuQ"
              placeholder="姓名或邮箱"
              style="min-width: 220px"
              @keyup.enter="runFeishuSearch"
            />
            <NButton type="primary" secondary :loading="searchingFeishu" @click="runFeishuSearch">搜索</NButton>
          </NSpace>
          <div v-if="feishuHits.length" style="margin-top: 0.5rem; max-height: 160px; overflow: auto">
            <div
              v-for="u in feishuHits"
              :key="u.open_id"
              style="padding: 0.35rem 0; border-bottom: 1px solid #eee; cursor: pointer"
              @click="pickFeishuUser(u)"
            >
              <strong>{{ u.name || "—" }}</strong>
              <NText depth="3" style="font-size: 0.82rem; margin-left: 0.5rem">
                {{ u.email || "" }} {{ u.department || "" }}
              </NText>
              <div style="font-size: 0.75rem; word-break: break-all; color: #888">{{ u.open_id }}</div>
            </div>
          </div>
        </div>
        <NFormItem label="飞书 open_id *">
          <NInput v-model:value="profileForm.open_id" :disabled="!!editingOpenId" placeholder="优先上方搜索填入" />
        </NFormItem>
        <NFormItem label="姓名"><NInput v-model:value="profileForm.name" /></NFormItem>
        <NFormItem label="邮箱"><NInput v-model:value="profileForm.email" /></NFormItem>
        <NFormItem label="部门"><NInput v-model:value="profileForm.department" /></NFormItem>
        <NFormItem label="角色分类 *（可多选）">
          <NCheckboxGroup v-model:value="profileForm.role_kinds">
            <NSpace>
              <NCheckbox v-for="o in ROLE_OPTIONS" :key="o.value" :value="o.value" :label="o.label" />
            </NSpace>
          </NCheckboxGroup>
        </NFormItem>
        <NFormItem label="特长标签（逗号分隔）">
          <NInput v-model:value="profileForm.specialties_text" placeholder="backend, go, system-design" />
        </NFormItem>
        <NFormItem label="备注"><NInput v-model:value="profileForm.notes" type="textarea" :autosize="{ minRows: 2 }" /></NFormItem>
        <NCheckbox v-model:checked="profileForm.enabled">启用</NCheckbox>
      </NForm>
      <NSpace justify="end" style="margin-top: 1rem">
        <NButton @click="showProfileModal = false">取消</NButton>
        <NButton type="primary" @click="saveProfile">保存</NButton>
      </NSpace>
    </NModal>

    <NModal v-model:show="showPoolModal" preset="card" title="面试池" style="width: 560px">
      <NForm label-placement="top">
        <NFormItem label="名称 *"><NInput v-model:value="poolForm.name" /></NFormItem>
        <NFormItem label="默认角色分类">
          <NSelect v-model:value="poolForm.default_role_kind" :options="ROLE_OPTIONS" />
        </NFormItem>
        <NFormItem label="部门（可选）"><NInput v-model:value="poolForm.department" /></NFormItem>
        <NFormItem label="成员 open_id（逗号分隔，或从下方选择后粘贴）">
          <NInput
            v-model:value="poolForm.member_open_ids_text"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 5 }"
            placeholder="ou_a, ou_b"
          />
        </NFormItem>
        <NFormItem v-if="profileOptions.length" label="从档案快速勾选（写入上方文本）">
          <NSelect
            multiple
            filterable
            :options="profileOptions"
            :value="poolForm.member_open_ids_text.split(/[,，\s]+/).filter(Boolean)"
            @update:value="(v: string[]) => (poolForm.member_open_ids_text = (v || []).join(', '))"
          />
        </NFormItem>
        <NFormItem label="备注"><NInput v-model:value="poolForm.notes" /></NFormItem>
        <NCheckbox v-model:checked="poolForm.enabled">启用</NCheckbox>
      </NForm>
      <NSpace justify="end" style="margin-top: 1rem">
        <NButton @click="showPoolModal = false">取消</NButton>
        <NButton type="primary" @click="savePool">保存</NButton>
      </NSpace>
    </NModal>
  </div>
</template>
