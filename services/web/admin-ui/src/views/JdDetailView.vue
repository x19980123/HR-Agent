<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NButton,
  NCard,
  NCheckbox,
  NCollapse,
  NCollapseItem,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSelect,
  NSpace,
  NText,
  useDialog,
} from "naive-ui";
import {
  deleteJd,
  getJd,
  listInterviewerPools,
  listInterviewers,
  putInterviewPlan,
  upsertJd,
  type InterviewRound,
  type InterviewRoundReq,
  type InterviewerPool,
  type InterviewerProfile,
} from "@/api/admin";
import { useNotify } from "@/composables/useNotify";

type SourceMode = "auto" | "pool" | "people";

type JdRequirementRow = InterviewRoundReq & {
  _source?: SourceMode;
  _advanced?: boolean;
};

const route = useRoute();
const router = useRouter();
const notify = useNotify();
const dialog = useDialog();

const id = computed(() => String(route.params.id));
const isNew = computed(() => id.value === "new");

const title = ref("");
const department = ref("");
const salary = ref("");
const location = ref("");
const description = ref("");
const loading = ref(!isNew.value);
const rounds = ref<InterviewRound[]>([]);
const savingPlan = ref(false);
const pools = ref<InterviewerPool[]>([]);
const profiles = ref<InterviewerProfile[]>([]);

const roleOptions = [
  { label: "HR", value: "hr" },
  { label: "技术 tech", value: "tech" },
  { label: "用人经理 hm", value: "hm" },
  { label: "跨部门 cross", value: "cross" },
  { label: "自定义 custom", value: "custom" },
];

const sourceOptions = [
  { label: "自动（按角色从档案/池）", value: "auto" },
  { label: "指定面试池", value: "pool" },
  { label: "指定人员", value: "people" },
];

const poolSelectOptions = computed(() =>
  pools.value
    .filter((p) => p.enabled !== false)
    .map((p) => ({
      label: `${p.name}（${p.default_role_kind || "tech"} · ${p.member_count ?? p.member_open_ids?.length ?? 0}人）`,
      value: p.id,
    })),
);

const profileOptions = computed(() =>
  profiles.value
    .filter((p) => p.enabled !== false)
    .map((p) => ({
      label: `${p.name || p.open_id}${p.department ? ` · ${p.department}` : ""}（${(p.role_kinds || []).join("/") || "—"}）`,
      value: p.open_id,
    })),
);

function inferSource(req: InterviewRoundReq): SourceMode {
  if ((req.fixed_open_ids || []).length > 0) return "people";
  if (req.pool_id) return "pool";
  return "auto";
}

function defaultPoolForRole(role: string): string {
  const hit = pools.value.find(
    (p) => p.enabled !== false && String(p.default_role_kind || "").toLowerCase() === String(role || "").toLowerCase(),
  );
  return hit?.id || "";
}

function emptyReq(role = "tech"): JdRequirementRow {
  const pool = defaultPoolForRole(role);
  return {
    role_kind: role,
    headcount: 1,
    pool_id: pool,
    match_jd_department: false,
    specialties: [],
    fixed_open_ids: [],
    _source: pool ? "auto" : "auto",
    _advanced: false,
  };
}

function emptyRound(i: number, name?: string, duration = 60, theme = "", role = "tech", hc = 1): InterviewRound {
  const req = emptyReq(role);
  req.headcount = hc;
  return {
    sort_order: i,
    round_key: `round_${i}`,
    name: name || `第${i + 1}轮`,
    theme,
    duration_minutes: duration,
    advance: "hr_manual",
    requirements: [req],
  };
}

function normalizeRounds(raw: InterviewRound[] | undefined) {
  if (!raw?.length) {
    rounds.value = [];
    return;
  }
  rounds.value = raw.map((r, i) => ({
    ...r,
    sort_order: i,
    name: r.name || `第${i + 1}轮`,
    duration_minutes: r.duration_minutes || 60,
    advance: r.advance || "hr_manual",
    requirements: (r.requirements || []).map((req) => {
      const er: JdRequirementRow = {
        ...req,
        role_kind: req.role_kind || "tech",
        headcount: req.headcount || 1,
        pool_id: req.pool_id || "",
        match_jd_department: !!req.match_jd_department,
        fixed_open_ids: Array.isArray(req.fixed_open_ids) ? [...req.fixed_open_ids] : [],
        specialties: Array.isArray(req.specialties) ? [...req.specialties] : [],
      };
      er._source = inferSource(er);
      er._advanced = !!(er.match_jd_department || (er.specialties || []).length);
      return er;
    }),
  }));
}

onMounted(async () => {
  try {
    const [poolsRes, profRes] = await Promise.all([
      listInterviewerPools().catch(() => ({ items: [] as InterviewerPool[] })),
      listInterviewers().catch(() => ({ items: [] as InterviewerProfile[] })),
    ]);
    pools.value = poolsRes.items || [];
    profiles.value = profRes.items || [];
  } catch {
    pools.value = [];
    profiles.value = [];
  }
  if (isNew.value) {
    loading.value = false;
    return;
  }
  try {
    const j = await getJd(id.value);
    title.value = j.title || "";
    department.value = j.department || "";
    salary.value = j.salary || "";
    location.value = j.location || "";
    description.value = j.description || "";
    normalizeRounds(j.rounds);
  } catch (e) {
    notify.from(e, "加载失败");
  } finally {
    loading.value = false;
  }
});

function payload() {
  return {
    id: isNew.value ? "" : id.value,
    title: title.value.trim(),
    department: department.value.trim(),
    salary: salary.value.trim(),
    location: location.value.trim(),
    description: description.value.trim(),
    requirements_text: description.value.trim(),
  };
}

async function save() {
  if (!title.value.trim()) {
    notify.warning("岗位名称必填");
    return;
  }
  try {
    const out = await upsertJd(payload(), isNew.value ? undefined : id.value);
    notify.success("岗位已保存");
    if (isNew.value && (out as { id?: string }).id) {
      router.replace({ name: "jd-detail", params: { id: (out as { id: string }).id } });
    }
  } catch (e) {
    notify.from(e, "保存失败");
  }
}

function asEditable(req: InterviewRoundReq): JdRequirementRow {
  return req as JdRequirementRow;
}

function getSource(req: InterviewRoundReq): SourceMode {
  return asEditable(req)._source || inferSource(req);
}

function setSource(req: InterviewRoundReq, mode: SourceMode) {
  const er = asEditable(req);
  er._source = mode;
  if (mode === "auto") {
    er.fixed_open_ids = [];
    er.pool_id = defaultPoolForRole(er.role_kind || "tech");
  } else if (mode === "pool") {
    er.fixed_open_ids = [];
    if (!er.pool_id) er.pool_id = poolSelectOptions.value[0]?.value || "";
  } else {
    er.pool_id = "";
  }
}

function onRoleChange(req: InterviewRoundReq, role: string) {
  req.role_kind = role;
  if (getSource(req) === "auto") {
    req.pool_id = defaultPoolForRole(role);
  }
}

async function savePlan() {
  if (isNew.value) {
    notify.warning("请先保存岗位基本信息");
    return;
  }
  for (const r of rounds.value) {
    const reqs = (r.requirements || []) as JdRequirementRow[];
    if (!reqs.length) {
      notify.warning(`「${r.name}」至少配置一条角色需求`);
      return;
    }
    for (const req of reqs) {
      const src = getSource(req);
      const hc = req.headcount || 1;
      if (src === "pool" && !req.pool_id) {
        notify.warning(`「${r.name}」角色 ${req.role_kind} 请选择面试池`);
        return;
      }
      if (src === "people") {
        const n = (req.fixed_open_ids || []).filter(Boolean).length;
        if (n < hc) {
          notify.warning(`「${r.name}」角色 ${req.role_kind} 已选 ${n} 人，少于需求人数 ${hc}`);
          return;
        }
      }
      if (req.match_jd_department && !department.value.trim()) {
        notify.warning(`「${r.name}」勾选了匹配 JD 部门，请先填写岗位部门`);
        return;
      }
    }
  }
  savingPlan.value = true;
  try {
    const body = rounds.value.map((r, i) => ({
      sort_order: i,
      round_key: r.round_key || `round_${i}`,
      name: r.name || `第${i + 1}轮`,
      theme: r.theme || "",
      duration_minutes: r.duration_minutes || 60,
      advance: r.advance || "hr_manual",
      requirements: ((r.requirements || []) as JdRequirementRow[]).map((req) => {
        const src = getSource(req);
        let pool_id = "";
        let fixed_open_ids: string[] = [];
        if (src === "auto") {
          pool_id = req.pool_id || defaultPoolForRole(req.role_kind || "tech") || "";
          fixed_open_ids = [];
        } else if (src === "pool") {
          pool_id = req.pool_id || "";
          fixed_open_ids = [];
        } else {
          pool_id = "";
          fixed_open_ids = (req.fixed_open_ids || []).map((x: string) => String(x).trim()).filter(Boolean);
        }
        return {
          role_kind: req.role_kind || "tech",
          headcount: req.headcount || 1,
          pool_id,
          match_jd_department: !!req.match_jd_department,
          specialties: (req.specialties || []).map((x: string) => String(x).trim()).filter(Boolean),
          fixed_open_ids,
        };
      }),
    }));
    const out = await putInterviewPlan(id.value, body);
    normalizeRounds(out.rounds);
    notify.success("面试计划已保存");
  } catch (e) {
    notify.from(e, "保存失败");
  } finally {
    savingPlan.value = false;
  }
}

function addRound() {
  rounds.value.push(emptyRound(rounds.value.length));
}

function addPreset(kind: "hr" | "tech" | "hm") {
  const i = rounds.value.length;
  if (kind === "hr") {
    rounds.value.push(emptyRound(i, "初筛", 40, "了解候选人基本情况与动机", "hr", 1));
  } else if (kind === "tech") {
    rounds.value.push(emptyRound(i, "技术面", 60, "岗位技能与项目深度", "tech", 2));
  } else {
    rounds.value.push(emptyRound(i, "终面", 60, "综合评估与团队匹配", "hm", 1));
  }
}

function removeRound(i: number) {
  rounds.value.splice(i, 1);
}

function addReq(ri: number) {
  const r = rounds.value[ri];
  if (!r.requirements) r.requirements = [];
  r.requirements.push(emptyReq());
}

function removeReq(ri: number, qi: number) {
  rounds.value[ri].requirements?.splice(qi, 1);
}

function specsText(req: InterviewRoundReq) {
  return (req.specialties || []).join(", ");
}

function setSpecs(req: InterviewRoundReq, text: string) {
  req.specialties = text
    .split(/[,，\s]+/)
    .map((x) => x.trim())
    .filter(Boolean);
}

function fixedIdsText(req: InterviewRoundReq) {
  return (req.fixed_open_ids || []).join(", ");
}

function setFixedIds(req: InterviewRoundReq, text: string) {
  req.fixed_open_ids = text
    .split(/[,，\s]+/)
    .map((x) => x.trim())
    .filter(Boolean);
}

function pasteFixedIds(req: InterviewRoundReq, text: string) {
  setFixedIds(req, text);
  if ((req.fixed_open_ids || []).length) {
    asEditable(req)._source = "people";
  }
}

function remove() {
  dialog.warning({
    title: "删除岗位",
    content: "确认删除？若有关联申请可能失败",
    positiveText: "删除",
    onPositiveClick: async () => {
      try {
        await deleteJd(id.value);
        notify.success("已删除");
        router.push({ name: "jds" });
      } catch (e) {
        notify.from(e, "删除失败");
      }
    },
  });
}
</script>

<template>
  <div>
    <NSpace justify="space-between" style="margin-bottom: 1rem">
      <h2 style="margin: 0">{{ isNew ? "新建岗位" : title || "编辑岗位" }}</h2>
      <NSpace>
        <NButton @click="router.push({ name: 'jds' })">返回</NButton>
        <NButton v-if="!isNew" type="error" secondary @click="remove">删除</NButton>
        <NButton type="primary" @click="save">保存基本信息</NButton>
      </NSpace>
    </NSpace>
    <NCard v-if="!loading" title="基本信息" style="max-width: 860px; margin-bottom: 1rem">
      <NForm label-placement="top">
        <NFormItem label="岗位名称 *"><NInput v-model:value="title" /></NFormItem>
        <NFormItem label="部门"><NInput v-model:value="department" /></NFormItem>
        <NFormItem label="薪资"><NInput v-model:value="salary" /></NFormItem>
        <NFormItem label="地点"><NInput v-model:value="location" /></NFormItem>
        <NFormItem label="岗位描述">
          <NInput v-model:value="description" type="textarea" :autosize="{ minRows: 5, maxRows: 14 }" />
        </NFormItem>
      </NForm>
    </NCard>

    <NCard v-if="!loading && !isNew" title="结构化面试计划（供约面 / Scheduling Agent）" style="max-width: 960px">
      <NText depth="3" style="display: block; margin-bottom: 0.75rem">
        配置角色 × 人数 × 人选来源；高级选项默认折叠。请先在「面试官档案」维护人选。
      </NText>
      <NSpace style="margin-bottom: 1rem" wrap>
        <NText depth="3">快速添加：</NText>
        <NButton size="small" @click="addPreset('hr')">初筛（HR×1）</NButton>
        <NButton size="small" @click="addPreset('tech')">技术面（tech×2）</NButton>
        <NButton size="small" @click="addPreset('hm')">终面（hm×1）</NButton>
      </NSpace>
      <NSpace vertical :size="16">
        <NCard
          v-for="(r, ri) in rounds"
          :key="ri"
          size="small"
          :title="`第 ${ri + 1} 轮 · ${r.name || ''}`"
          segmented
        >
          <template #header-extra>
            <NButton size="tiny" quaternary type="error" @click="removeRound(ri)">删除本轮</NButton>
          </template>
          <NForm label-placement="top">
            <NSpace>
              <NFormItem label="轮次名称" style="min-width: 160px">
                <NInput v-model:value="r.name" />
              </NFormItem>
              <NFormItem label="时长(分钟)" style="width: 140px">
                <NInputNumber v-model:value="r.duration_minutes" :min="15" :max="240" />
              </NFormItem>
            </NSpace>
            <NFormItem label="面试主题 / 考察点">
              <NInput v-model:value="r.theme" type="textarea" :autosize="{ minRows: 1, maxRows: 3 }" />
            </NFormItem>
            <NText strong style="display: block; margin: 0.35rem 0 0.5rem">面试官需求</NText>
            <div
              v-for="(req, qi) in r.requirements || []"
              :key="qi"
              style="border: 1px solid #eee; border-radius: 6px; padding: 0.65rem 0.75rem; margin-bottom: 0.65rem"
            >
              <NSpace align="center" wrap :size="12">
                <NFormItem label="角色" style="width: 140px; margin-bottom: 0">
                  <NSelect
                    :value="req.role_kind"
                    :options="roleOptions"
                    @update:value="(v) => onRoleChange(req, String(v))"
                  />
                </NFormItem>
                <NFormItem label="人数" style="width: 96px; margin-bottom: 0">
                  <NInputNumber v-model:value="req.headcount" :min="1" :max="10" />
                </NFormItem>
                <NFormItem label="人选来源" style="min-width: 220px; margin-bottom: 0; flex: 1">
                  <NSelect
                    :value="getSource(req)"
                    :options="sourceOptions"
                    @update:value="(v) => setSource(req, v as SourceMode)"
                  />
                </NFormItem>
                <NButton size="tiny" quaternary type="error" @click="removeReq(ri, qi)">删除</NButton>
              </NSpace>

              <NFormItem v-if="getSource(req) === 'pool'" label="面试池" style="margin-top: 0.5rem; margin-bottom: 0">
                <NSelect v-model:value="req.pool_id" :options="poolSelectOptions" placeholder="选择池" />
              </NFormItem>
              <NFormItem
                v-if="getSource(req) === 'people'"
                label="指定人员（按姓名选择，写入 open_id）"
                style="margin-top: 0.5rem; margin-bottom: 0"
              >
                <NSelect
                  multiple
                  filterable
                  :value="req.fixed_open_ids || []"
                  :options="profileOptions"
                  placeholder="从面试官档案多选"
                  @update:value="(v) => (req.fixed_open_ids = (v as string[]) || [])"
                />
              </NFormItem>
              <NText v-if="getSource(req) === 'auto'" depth="3" style="display: block; margin-top: 0.4rem; font-size: 0.82rem">
                自动：优先该角色默认池，否则从全库档案按角色匹配
                <span v-if="req.pool_id">（将使用池 {{ req.pool_id }}）</span>
              </NText>

              <NCollapse style="margin-top: 0.35rem">
                <NCollapseItem title="高级选项" name="adv">
                  <NCheckbox v-model:checked="req.match_jd_department" style="margin-bottom: 0.5rem">
                    匹配 JD 部门
                  </NCheckbox>
                  <NFormItem label="特长标签（逗号分隔）">
                    <NInput
                      :value="specsText(req)"
                      placeholder="backend, go"
                      @update:value="(v) => setSpecs(req, v)"
                    />
                  </NFormItem>
                  <NFormItem label="粘贴 open_id（运维备用；日常请用「指定人员」）">
                    <NInput
                      :value="fixedIdsText(req)"
                      placeholder="ou_xxx, ou_yyy"
                      @update:value="(v) => pasteFixedIds(req, v)"
                    />
                  </NFormItem>
                </NCollapseItem>
              </NCollapse>
            </div>
            <NButton size="small" @click="addReq(ri)">添加角色需求</NButton>
          </NForm>
        </NCard>
        <NSpace>
          <NButton @click="addRound">添加空白轮</NButton>
          <NButton type="primary" :loading="savingPlan" @click="savePlan">保存面试计划</NButton>
        </NSpace>
        <NText v-if="!rounds.length" depth="3">尚未配置轮次。可点上方模板快速添加；未配置时筛选通过会转人工。</NText>
      </NSpace>
    </NCard>
  </div>
</template>
