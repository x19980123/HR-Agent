<script setup lang="ts">
import { ref, onMounted, h, computed } from "vue";
import {
  NAlert,
  NButton,
  NCard,
  NCheckbox,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NRadioButton,
  NRadioGroup,
  NSpace,
  NText,
  useDialog,
  type DataTableColumns,
} from "naive-ui";
import {
  approveJoin,
  listJoinRequests,
  listStaff,
  rejectJoin,
  saveStaff,
  setStaffEnabled,
  staffAudit,
  type StaffMember,
} from "@/api/admin";
import { useAuthStore } from "@/stores/auth";
import { fmtTime } from "@/utils/format";
import { useNotify } from "@/composables/useNotify";

const notify = useNotify();
const dialog = useDialog();
const auth = useAuthStore();

const loading = ref(false);
const members = ref<StaffMember[]>([]);
const joins = ref<{ id: string; name?: string; email?: string; open_id?: string; created_at?: string }[]>([]);
const auditItems = ref<{ action: string; actor?: string; created_at?: string; detail?: Record<string, unknown> }[]>([]);

const showStaffModal = ref(false);
const editingStaff = ref<StaffMember | null>(null);
type RolePreset = "admin" | "hr" | "qbank";
const rolePreset = ref<RolePreset>("hr");

const staffForm = ref({
  open_id: "",
  name: "",
  email: "",
  is_hr: true,
  is_admin: false,
  can_manage_question_bank: false,
  enabled: true,
});

const showApproveModal = ref(false);
const approveJoinId = ref("");

const STAFF_ACTION: Record<string, string> = {
  staff_created: "新增成员",
  staff_updated: "更新成员",
  staff_enabled: "启用成员",
  staff_disabled: "停用成员",
  staff_join_approved: "同意加入",
  staff_join_rejected: "拒绝加入",
};

function isLastEnabledAdmin(row: StaffMember) {
  if (!row.is_admin) return false;
  const n = members.value.filter((m) => m.is_admin && m.enabled !== false).length;
  return n <= 1;
}

function applyPreset(p: RolePreset) {
  rolePreset.value = p;
  const f = staffForm.value;
  switch (p) {
    case "admin":
      f.is_hr = true;
      f.is_admin = true;
      f.can_manage_question_bank = true;
      break;
    case "hr":
      f.is_hr = true;
      f.is_admin = false;
      f.can_manage_question_bank = false;
      break;
    case "qbank":
      f.is_hr = true;
      f.is_admin = false;
      f.can_manage_question_bank = true;
      break;
  }
}

const joinColumns: DataTableColumns<(typeof joins.value)[0]> = [
  { title: "申请人", key: "name" },
  { title: "邮箱", key: "email" },
  { title: "时间", key: "created_at", render: (row) => fmtTime(row.created_at) },
  {
    title: "操作",
    key: "op",
    render(row) {
      return h(NSpace, { size: 4 }, () => [
        h(NButton, { size: "small", type: "primary", onClick: () => openApprove(row.id) }, () => "同意"),
        h(NButton, { size: "small", onClick: () => onRejectJoin(row.id) }, () => "拒绝"),
      ]);
    },
  },
];

const memberColumns: DataTableColumns<StaffMember> = [
  { title: "成员", key: "name" },
  { title: "open_id", key: "open_id", ellipsis: { tooltip: true } },
  {
    title: "角色",
    key: "roles",
    render(row) {
      const tags: string[] = [];
      if (row.is_admin) tags.push("系统管理员");
      if (row.is_hr) tags.push("HR");
      if (row.can_manage_question_bank && !row.is_admin) tags.push("题库");
      return tags.join(" · ") || "—";
    },
  },
  { title: "状态", key: "enabled", render: (row) => (row.enabled ? "启用" : "停用") },
  {
    title: "操作",
    key: "op",
    render(row) {
      const canDisable = row.enabled && !isLastEnabledAdmin(row);
      return h(NSpace, { size: 4 }, () => [
        h(NButton, { size: "small", onClick: () => openStaffEdit(row) }, () => "编辑"),
        canDisable
          ? h(NButton, { size: "small", onClick: () => toggleEnable(row, false) }, () => "停用")
          : !row.enabled
            ? h(NButton, { size: "small", onClick: () => toggleEnable(row, true) }, () => "启用")
            : null,
      ]);
    },
  },
];

const presetHint = computed(() => {
  switch (rolePreset.value) {
    case "admin":
      return "可管理成员、题库，并登录管理台。";
    case "hr":
      return "登录管理台处理招聘流程。业务面试官在「面试官」页建档案/池，再在 JD 面试计划里按角色选用，不在此成员表。";
    case "qbank":
      return "HR + 题库录入与向量重建；不含成员管理。";
    default:
      return "";
  }
});

async function load() {
  loading.value = true;
  try {
    const [m, j, a] = await Promise.all([listStaff(), listJoinRequests(), staffAudit()]);
    members.value = m.items || [];
    joins.value = j.items || [];
    auditItems.value = a.items || [];
  } finally {
    loading.value = false;
  }
}

function openStaffNew() {
  editingStaff.value = null;
  staffForm.value = {
    open_id: "",
    name: "",
    email: "",
    is_hr: true,
    is_admin: false,
    can_manage_question_bank: false,
    enabled: true,
  };
  applyPreset("hr");
  showStaffModal.value = true;
}

function openStaffEdit(row: StaffMember) {
  editingStaff.value = row;
  staffForm.value = {
    open_id: row.open_id,
    name: row.name || "",
    email: row.email || "",
    is_hr: !!row.is_hr,
    is_admin: !!row.is_admin,
    can_manage_question_bank: !!row.can_manage_question_bank,
    enabled: row.enabled !== false,
  };
  if (row.is_admin) rolePreset.value = "admin";
  else if (row.can_manage_question_bank) rolePreset.value = "qbank";
  else rolePreset.value = "hr";
  showStaffModal.value = true;
}

async function saveStaffForm() {
  if (!staffForm.value.open_id.trim()) {
    notify.warning("open_id 必填");
    return;
  }
  if (!staffForm.value.is_hr && !staffForm.value.is_admin) {
    notify.warning("请选择 HR 或系统管理员");
    return;
  }
  try {
    const body: Record<string, unknown> = {
      open_id: staffForm.value.open_id.trim(),
      name: staffForm.value.name.trim(),
      email: staffForm.value.email.trim(),
      is_hr: staffForm.value.is_hr,
      is_interviewer: false,
      can_manage_question_bank: staffForm.value.can_manage_question_bank,
      enabled: staffForm.value.enabled,
    };
    if (auth.isAdmin) {
      body.is_admin = staffForm.value.is_admin;
    }
    await saveStaff(body, editingStaff.value ? editingStaff.value.open_id : undefined);
    notify.success("已保存");
    showStaffModal.value = false;
    await load();
  } catch (e) {
    notify.from(e, "失败");
  }
}

async function toggleEnable(row: StaffMember, enabled: boolean) {
  try {
    await setStaffEnabled(row.open_id, enabled);
    notify.success(enabled ? "已启用" : "已停用");
    await load();
  } catch (e) {
    notify.from(e, "失败");
  }
}

function openApprove(id: string) {
  approveJoinId.value = id;
  showApproveModal.value = true;
}

async function confirmApprove() {
  try {
    await approveJoin(approveJoinId.value, {
      is_hr: true,
      is_interviewer: false,
    });
    notify.success("已同意加入");
    showApproveModal.value = false;
    await load();
  } catch (e) {
    notify.from(e, "失败");
  }
}

async function onRejectJoin(id: string) {
  dialog.warning({
    title: "拒绝申请",
    content: "确认拒绝？",
    positiveText: "拒绝",
    onPositiveClick: async () => {
      await rejectJoin(id);
      notify.success("已拒绝");
      await load();
    },
  });
}

onMounted(load);
</script>

<template>
  <NSpace justify="space-between" style="margin-bottom: 1rem">
    <div>
      <h2 style="margin: 0">成员与角色</h2>
      <NText depth="3">仅系统管理员与 HR 登录本系统；业务面试官请到侧栏「面试官」维护</NText>
    </div>
    <NButton @click="openStaffNew">添加成员</NButton>
  </NSpace>

  <NAlert type="info" style="margin-bottom: 1rem" :show-icon="false">
    <strong>角色说明</strong>：系统管理员 = 成员管理 + 默认题库权限；HR = 登录管理台跑招聘；
    题库权限 = 录入/批量/重建索引。业务面试官不进此表：先在「面试官」页维护档案（角色分类/特长）与面试池，再在岗位 JD「面试计划」里按角色×人数选用池或固定 open_id。
  </NAlert>

  <NCard title="待审批加入" style="margin-bottom: 1rem">
    <NDataTable :loading="loading" :columns="joinColumns" :data="joins" />
  </NCard>
  <NCard title="成员列表" style="margin-bottom: 1rem">
    <NDataTable :loading="loading" :columns="memberColumns" :data="members" />
  </NCard>
  <NCard title="操作审计">
    <div v-for="a in auditItems.slice(0, 12)" :key="String(a.created_at) + a.action" style="margin-bottom: 0.4rem">
      {{ fmtTime(a.created_at) }} — {{ STAFF_ACTION[a.action] || a.action }}
      <NText depth="3">by {{ a.actor }}</NText>
    </div>
  </NCard>

  <NModal v-model:show="showStaffModal" preset="card" title="成员" style="width: min(480px, 92vw)">
    <NForm label-placement="top">
      <NFormItem label="快捷角色">
        <NRadioGroup :value="rolePreset" @update:value="(v) => applyPreset(v as RolePreset)">
          <NSpace vertical>
            <NRadioButton value="admin" :disabled="!auth.isAdmin">系统管理员</NRadioButton>
            <NRadioButton value="hr">HR 招聘</NRadioButton>
            <NRadioButton value="qbank">HR + 题库维护</NRadioButton>
          </NSpace>
        </NRadioGroup>
        <NText depth="3" style="display: block; margin-top: 0.5rem; font-size: 0.85rem">{{ presetHint }}</NText>
      </NFormItem>
      <NFormItem label="open_id">
        <NInput v-model:value="staffForm.open_id" :disabled="!!editingStaff" placeholder="飞书 open_id" />
      </NFormItem>
      <NFormItem label="姓名"><NInput v-model:value="staffForm.name" /></NFormItem>
      <NFormItem label="邮箱"><NInput v-model:value="staffForm.email" /></NFormItem>
      <NSpace vertical>
        <NCheckbox v-model:checked="staffForm.is_hr">HR（可登录管理台）</NCheckbox>
        <NCheckbox v-model:checked="staffForm.can_manage_question_bank" :disabled="staffForm.is_admin">
          题库 RAG 管理
        </NCheckbox>
        <NCheckbox v-if="auth.isAdmin" v-model:checked="staffForm.is_admin">系统管理员</NCheckbox>
        <NCheckbox v-model:checked="staffForm.enabled">启用</NCheckbox>
      </NSpace>
    </NForm>
    <template #footer>
      <NButton type="primary" @click="saveStaffForm">保存</NButton>
    </template>
  </NModal>

  <NModal v-model:show="showApproveModal" preset="dialog" title="同意加入">
    <NText depth="3" style="display: block; margin-bottom: 0.75rem">申请人将开通 HR 登录。</NText>
    <template #action>
      <NButton @click="showApproveModal = false">取消</NButton>
      <NButton type="primary" @click="confirmApprove">确认</NButton>
    </template>
  </NModal>
</template>
