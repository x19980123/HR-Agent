<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NAlert,
  NButton,
  NCard,
  NCheckbox,
  NCollapse,
  NCollapseItem,
  NGrid,
  NGi,
  NSpace,
  NTabs,
  NTabPane,
  NText,
  NInput,
  NInputNumber,
  NSelect,
  NTag,
  useDialog,
} from "naive-ui";
import {
  getApplication,
  humanApprove,
  retryParse,
  resumeDownloadUrl,
  updateApplicationContact,
  advanceRound,
  updateOffer,
  manualSchedule,
  sendInterviewerPack,
  listInterviewers,
  listInterviewerPools,
  CONTACT_SOURCE_LABEL,
  type ApplicationDetail,
  type InterviewRound,
  type InterviewerProfile,
  type InterviewerPool,
  type InterviewQuestionItem,
  type InterviewerPackLink,
  type OfferStatus,
} from "@/api/admin";
import StatusTag from "@/components/StatusTag.vue";
import AgentProgressRail from "@/components/AgentProgressRail.vue";
import {
  ACTION_LABEL,
  ISSUE_LABEL,
  agentPipeline,
  canHumanApproveApp,
  canRetryParse,
  isProcessingStatus,
  statusLabel,
} from "@/constants/status";
import { fmtTime } from "@/utils/format";
import { formatError } from "@/errors";
import { useNotify } from "@/composables/useNotify";

const route = useRoute();
const router = useRouter();
const notify = useNotify();
const dialog = useDialog();

const app = ref<ApplicationDetail | null>(null);
const loading = ref(true);
const retryText = ref("");
const editEmail = ref("");
const editName = ref("");
const showContactEdit = ref(false);
const assignMode = ref<"keep" | "people" | "pool">("keep");
const selectedOpenIds = ref<string[]>([]);
const selectedPoolId = ref<string | null>(null);
const profiles = ref<InterviewerProfile[]>([]);
const pools = ref<InterviewerPool[]>([]);
const manualSlots = ref<{ start: string; end: string; location: string }[]>([
  { start: "", end: "", location: "线上会议" },
]);
const resendInvite = ref(true);
const savingManual = ref(false);

const assignModeOptions = [
  { label: "沿用本轮已指派", value: "keep" },
  { label: "指定人员（档案）", value: "people" },
  { label: "指定面试池", value: "pool" },
];

const profileOptions = computed(() =>
  profiles.value
    .filter((p) => p.enabled !== false)
    .map((p) => ({
      label: `${p.name || p.open_id}${p.department ? ` · ${p.department}` : ""}（${(p.role_kinds || []).join("/") || "—"}）`,
      value: p.open_id,
    })),
);

const poolSelectOptions = computed(() =>
  pools.value
    .filter((p) => p.enabled !== false)
    .map((p) => ({
      label: `${p.name}（${p.default_role_kind || "tech"} · ${p.member_count ?? p.member_open_ids?.length ?? 0}人）`,
      value: p.id,
    })),
);

function interviewerLabel(oid: string) {
  const p = profiles.value.find((x) => x.open_id === oid);
  return p?.name || oid;
}
const feedbackDraft = ref<Record<number, { rating: number | null; summary: string; recommend: string }>>({});
const offerNote = ref("");
const savingOffer = ref(false);
const detailTab = ref("overview");
let pollTimer: ReturnType<typeof setInterval> | null = null;

const OFFER_LABEL: Record<string, string> = {
  none: "无",
  pending: "待发 Offer",
  sent: "已发出",
  accepted: "已接受",
  declined: "已拒绝",
  hired: "已入职",
};

const OFFER_NEXT: Record<string, { status: OfferStatus; label: string; type?: "primary" | "error" | "default" }[]> = {
  none: [{ status: "pending", label: "标记待发 Offer" }],
  pending: [
    { status: "sent", label: "标记已发 Offer", type: "primary" },
    { status: "declined", label: "候选人拒绝", type: "error" },
  ],
  sent: [
    { status: "accepted", label: "接受", type: "primary" },
    { status: "declined", label: "拒绝", type: "error" },
    { status: "pending", label: "退回待发" },
  ],
  accepted: [
    { status: "hired", label: "标记入职", type: "primary" },
    { status: "declined", label: "反悔拒绝", type: "error" },
  ],
  declined: [
    { status: "pending", label: "重新待发" },
    { status: "sent", label: "重新标记已发" },
  ],
  hired: [],
};

const recommendOptions = [
  { label: "推荐", value: "yes" },
  { label: "弱推", value: "weak" },
  { label: "不推荐", value: "no" },
];

const id = computed(() => String(route.params.id));

const pipeline = computed(() => agentPipeline(app.value));

const systemFault = computed(() => app.value?.error_kind === "system");

const alertDetail = computed(() => {
  const a = app.value;
  if (!a) return "";
  const parts = [a.human_reason_code, a.error_message, a.system_error_code].filter(Boolean) as string[];
  for (const p of parts) {
    const nice = formatError(p, "");
    if (nice) return nice;
  }
  return "请检查简历、联系方式，或使用人工操作";
});

const questionList = computed(() => {
  const q = app.value?.questions;
  return Array.isArray(q) ? (q as InterviewQuestionItem[]) : [];
});

const interviewerPack = computed(() => {
  const p = app.value?.interviewer_pack;
  return Array.isArray(p) ? (p as InterviewerPackLink[]) : [];
});

const sendingPack = ref(false);

async function onSendInterviewerPack() {
  if (!app.value?.id) return;
  sendingPack.value = true;
  try {
    const res = await sendInterviewerPack(app.value.id);
    notify.success("题单邮件已发送（无邮箱的面试官请在下方复制链接）");
    if (res.interviewer_pack?.length) {
      app.value = { ...app.value, interviewer_pack: res.interviewer_pack };
    } else {
      await load();
    }
  } catch (e) {
    notify.from(e, "发送失败");
  } finally {
    sendingPack.value = false;
  }
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    notify.success("已复制");
  } catch {
    notify.warning("复制失败，请手动选择链接");
  }
}

const jdTitle = computed(() => {
  const j = app.value?.jd as { title?: string } | undefined;
  return j?.title || app.value?.jd_id || "-";
});

const screenTotal = computed(() => {
  const s = app.value?.screen as { weighted_total?: number } | undefined;
  return s?.weighted_total ?? "-";
});

const hardFails = computed(() => {
  const s = app.value?.screen as { hard_fail_reasons?: string[] } | undefined;
  const list = s?.hard_fail_reasons || [];
  return list.length ? list.join("；") : "无";
});

const profileName = computed(() => (app.value?.profile as { name?: string })?.name || "-");
const profileConf = computed(() => (app.value?.profile as { parse_confidence?: number })?.parse_confidence ?? "-");
const profileIssuesText = computed(() => {
  const issues = (app.value?.profile as { issues?: string[] })?.issues || [];
  return issues.map((i) => ISSUE_LABEL[i] || i).join("；");
});
const hasProfileIssues = computed(() => {
  const issues = (app.value?.profile as { issues?: string[] })?.issues || [];
  return issues.length > 0;
});

function ensureFeedbackDraft(rounds: InterviewRound[]) {
  for (const r of rounds) {
    const idx = r.round_index ?? 0;
    if (feedbackDraft.value[idx]) continue;
    const fb = r.feedback_json;
    feedbackDraft.value[idx] = {
      rating: typeof fb?.rating === "number" ? fb.rating : null,
      summary: fb?.summary || "",
      recommend: fb?.recommend || "",
    };
  }
}

function fbDraft(roundIndex?: number) {
  const idx = roundIndex ?? 0;
  if (!feedbackDraft.value[idx]) {
    feedbackDraft.value[idx] = { rating: null, summary: "", recommend: "" };
  }
  return feedbackDraft.value[idx];
}

async function loadInterviewersCatalog() {
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
}

async function load() {
  try {
    app.value = await getApplication(id.value);
    offerNote.value = app.value.offer_note || "";
    ensureFeedbackDraft(app.value.interview_rounds || []);
    const cur = (app.value.interview_rounds || []).find(
      (r) => (r.round_index ?? 0) === (app.value?.current_round_index ?? 0),
    );
    if (cur?.assigned_open_ids?.length && selectedOpenIds.value.length === 0 && assignMode.value === "keep") {
      selectedOpenIds.value = [...cur.assigned_open_ids];
    }
    if (app.value.status === "needs_human") detailTab.value = "interview";
  } catch (e) {
    notify.from(e, "加载失败");
  } finally {
    loading.value = false;
  }
}

function startPoll() {
  stopPoll();
  pollTimer = setInterval(async () => {
    if (!app.value || !isProcessingStatus(app.value.status)) {
      stopPoll();
      return;
    }
    try {
      app.value = await getApplication(id.value);
      if (!isProcessingStatus(app.value.status)) stopPoll();
    } catch {
      /* ignore */
    }
  }, 2500);
}

function stopPoll() {
  if (pollTimer) clearInterval(pollTimer);
  pollTimer = null;
}

onMounted(async () => {
  await Promise.all([load(), loadInterviewersCatalog()]);
  if (app.value && isProcessingStatus(app.value.status)) startPoll();
});

onUnmounted(stopPoll);

async function onRetry() {
  try {
    await retryParse(id.value, retryText.value.trim() || undefined);
    notify.success("已触发重试解析");
    await load();
    startPoll();
  } catch (e) {
    notify.from(e, "失败");
  }
}

async function saveContact() {
  try {
    await updateApplicationContact(id.value, editEmail.value, editName.value);
    notify.success("联系方式已更新");
    showContactEdit.value = false;
    await load();
  } catch (e) {
    notify.from(e, "保存失败");
  }
}

function openContactEdit() {
  editEmail.value = app.value?.candidate_email || "";
  editName.value = app.value?.candidate_name || "";
  showContactEdit.value = true;
}

function onApprove() {
  dialog.warning({
    title: "人工通过",
    content: "确认人工通过并发送面试邀约？需 JD 已配置面试计划，且能按角色分类解析到足够面试官。",
    positiveText: "确认",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await humanApprove(id.value);
        notify.success("已人工通过并约面");
        await load();
      } catch (e) {
        notify.from(e, "失败");
      }
    },
  });
}

const canManualSchedule = computed(() => {
  const st = app.value?.status;
  return st === "needs_human" || st === "awaiting_reply" || st === "screened";
});

function addManualSlot() {
  if (manualSlots.value.length >= 3) {
    notify.warning("最多 3 个候选时段");
    return;
  }
  manualSlots.value.push({ start: "", end: "", location: "线上会议" });
}

function removeManualSlot(i: number) {
  manualSlots.value.splice(i, 1);
  if (!manualSlots.value.length) {
    manualSlots.value.push({ start: "", end: "", location: "线上会议" });
  }
}

function localToISO(local: string): string {
  if (!local) return "";
  const d = new Date(local);
  if (Number.isNaN(d.getTime())) return "";
  return d.toISOString();
}

function resolveManualAssignees(): string[] | null {
  if (assignMode.value === "keep") return [];
  if (assignMode.value === "people") {
    return selectedOpenIds.value.map((x) => x.trim()).filter(Boolean);
  }
  const pool = pools.value.find((p) => p.id === selectedPoolId.value);
  const ids = (pool?.member_open_ids || []).map((x) => x.trim()).filter(Boolean);
  return ids;
}

async function submitManualSchedule(mode: "slots" | "auto" | "assignees") {
  const assignees = resolveManualAssignees();
  if (assignees === null) return;
  if (assignMode.value === "people" && !assignees.length) {
    notify.warning("请从面试官档案中选择至少一人");
    return;
  }
  if (assignMode.value === "pool") {
    if (!selectedPoolId.value) {
      notify.warning("请选择面试池");
      return;
    }
    if (!assignees.length) {
      notify.warning("该面试池暂无成员，请先到「面试官档案」维护池成员");
      return;
    }
  }

  const body: {
    assigned_open_ids?: string[];
    slots?: { starts_at: string; ends_at: string; location?: string }[];
    resend_invite?: boolean;
  } = {};
  if (assignees.length) body.assigned_open_ids = assignees;

  if (mode === "assignees") {
    if (!assignees.length) {
      notify.warning("请选择「指定人员」或「指定面试池」后再更新");
      return;
    }
    body.resend_invite = false;
  } else if (mode === "auto") {
    body.resend_invite = true;
  } else {
    const slots = [];
    for (const s of manualSlots.value) {
      const starts = localToISO(s.start);
      const ends = localToISO(s.end);
      if (!starts || !ends) continue;
      slots.push({ starts_at: starts, ends_at: ends, location: s.location || "线上会议" });
    }
    if (!slots.length) {
      notify.warning("请至少填写一个有效时段");
      return;
    }
    body.slots = slots;
    body.resend_invite = resendInvite.value;
  }

  const hasExisting = (app.value?.interview_rounds || []).some((r) => r.assigned_open_ids?.length);
  if (mode !== "assignees" && !assignees.length && !hasExisting) {
    notify.warning("请从档案指定人员/面试池，或确保当前轮次已有指派");
    return;
  }

  savingManual.value = true;
  try {
    await manualSchedule(id.value, body);
    notify.success(mode === "assignees" ? "面试官已更新" : "已排期并处理邀约");
    await load();
  } catch (e) {
    notify.from(e, "失败");
  } finally {
    savingManual.value = false;
  }
}

const interviewRounds = computed(() => app.value?.interview_rounds || []);
const currentRoundIndex = computed(() => app.value?.current_round_index ?? 0);

function canAdvanceRound(r: InterviewRound) {
  return r.status === "confirmed" && !r.outcome;
}

const offerStatus = computed(() => String(app.value?.offer_status || "none"));
const offerActions = computed(() => OFFER_NEXT[offerStatus.value] || []);
const showOfferCard = computed(() => {
  const st = offerStatus.value;
  if (st && st !== "none") return true;
  return (app.value?.interview_rounds || []).some((r) => r.outcome === "pass");
});

function onAdvance(r: InterviewRound, outcome: "pass" | "fail" | "hold") {
  const idx = r.round_index ?? currentRoundIndex.value;
  const draft = feedbackDraft.value[idx] || { rating: null, summary: "", recommend: "" };
  const labels = { pass: "本轮通过并进入下一轮", fail: "本轮淘汰", hold: "本轮暂缓（转人工）" };
  dialog.warning({
    title: labels[outcome],
    content:
      outcome === "pass"
        ? "将一并保存本轮反馈。若还有下一轮将自动重新排期发邀约；末轮通过后进入待发 Offer。"
        : "将一并保存本轮反馈。确认操作？",
    positiveText: "确认",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        const feedback =
          draft.summary || draft.recommend || draft.rating != null
            ? {
                rating: draft.rating ?? undefined,
                summary: draft.summary || undefined,
                recommend: draft.recommend || undefined,
              }
            : undefined;
        await advanceRound(id.value, idx, outcome, draft.summary || undefined, feedback);
        notify.success("已更新");
        await load();
      } catch (e) {
        notify.from(e, "失败");
      }
    },
  });
}

async function onOffer(status: OfferStatus) {
  savingOffer.value = true;
  try {
    await updateOffer(id.value, status, offerNote.value);
    notify.success(`Offer → ${OFFER_LABEL[status] || status}`);
    await load();
  } catch (e) {
    notify.from(e, "失败");
  } finally {
    savingOffer.value = false;
  }
}

function formatOpenIds(ids?: string[]) {
  if (!ids?.length) return "—";
  return ids.map(interviewerLabel).join("、");
}

const auditDesc = computed(() => [...(app.value?.audit || [])].reverse());
</script>

<template>
  <div v-if="loading">
    <NText depth="3">加载中…</NText>
  </div>
  <div v-else-if="app">
    <div
      style="
        position: sticky;
        top: 0;
        z-index: 20;
        background: #fff;
        padding-bottom: 0.75rem;
        margin: -0.25rem 0 0.25rem;
        border-bottom: 1px solid #f0f0f0;
      "
    >
      <NSpace justify="space-between" align="start" wrap>
        <div>
          <h2 style="margin: 0 0 0.35rem">{{ app.candidate_name || "候选人" }}</h2>
          <NSpace align="center" :size="8" wrap>
            <NText depth="3">{{ app.candidate_email }}</NText>
            <StatusTag :status="app.status" />
            <NTag size="small" :bordered="false" type="info">{{ pipeline.shortLabel }}</NTag>
            <NTag v-if="app.contact_email_source" size="small" type="info">
              {{ CONTACT_SOURCE_LABEL[app.contact_email_source] || app.contact_email_source }}
            </NTag>
            <NButton size="tiny" quaternary @click="openContactEdit">改邮箱</NButton>
            <a v-if="app.has_resume" :href="resumeDownloadUrl(app.id)" target="_blank">下载简历</a>
          </NSpace>
        </div>
        <NSpace>
          <NButton @click="router.push({ name: 'applications' })">返回列表</NButton>
          <NButton v-if="canRetryParse(app.status)" @click="onRetry">重试解析 Agent</NButton>
          <NButton v-if="canHumanApproveApp(app)" type="primary" @click="onApprove">
            人工通过并约面
          </NButton>
        </NSpace>
      </NSpace>
    </div>

    <AgentProgressRail :pipeline="pipeline" />

    <NTabs v-model:value="detailTab" type="line" animated>
      <NTabPane name="overview" tab="概览">
        <NAlert
          v-if="app.error_message || app.human_reason_code || app.status === 'needs_human' || app.status === 'rejected'"
          :type="app.status === 'failed' || app.status === 'rejected' ? 'error' : 'warning'"
          style="margin-bottom: 0.75rem"
          :title="statusLabel(app.status)"
        >
          {{ alertDetail }}
        </NAlert>
        <NAlert v-if="systemFault" type="error" title="系统异常" style="margin-bottom: 0.75rem">
          {{ formatError(app.system_error_code || app.error_message, "系统故障") }}
          — 不可人工通过，请排查后重试解析 Agent
        </NAlert>
        <NInput
          v-if="canRetryParse(app.status)"
          v-model:value="retryText"
          type="textarea"
          placeholder="可选：粘贴简历全文后重试解析 Agent…"
          style="margin-bottom: 0.75rem"
          :autosize="{ minRows: 2, maxRows: 6 }"
        />

        <NGrid cols="1 m:2" :x-gap="16" :y-gap="16">
          <NGi>
            <NCard title="解析 Agent 产出" size="small">
              <template v-if="app.profile">
                <p>姓名：{{ profileName }}</p>
                <p>
                  置信：
                  <NTag size="small" :type="Number(profileConf) >= 0.7 ? 'success' : 'warning'" :bordered="false">
                    {{ profileConf }}
                  </NTag>
                </p>
                <p v-if="hasProfileIssues">提示：{{ profileIssuesText }}</p>
              </template>
              <NText v-else depth="3">暂无产出（解析未完成或失败）</NText>
            </NCard>
          </NGi>
          <NGi>
            <NCard title="筛选 Agent 产出" size="small">
              <p>岗位：{{ jdTitle }}</p>
              <p>总分：{{ screenTotal }}</p>
              <p>硬性否决：{{ hardFails }}</p>
            </NCard>
          </NGi>
        </NGrid>

        <NCard v-if="questionList.length" title="出题 Agent 产出" size="small" style="margin-top: 1rem">
          <NCollapse>
            <NCollapseItem
              v-for="(q, i) in questionList.slice(0, 12)"
              :key="i"
              :title="`${i + 1}. ${q.category || '题目'} — ${(q.question || '').slice(0, 48)}${(q.question || '').length > 48 ? '…' : ''}`"
            >
              <p style="margin: 0 0 0.5rem; line-height: 1.55">{{ q.question }}</p>
              <NText depth="3" style="display: block; font-size: 0.85rem">参考答案</NText>
              <p style="margin: 0.25rem 0; white-space: pre-wrap; line-height: 1.5">{{ q.reference_answer || "—" }}</p>
              <ul v-if="q.scoring_points?.length" style="margin: 0.35rem 0 0; padding-left: 1.2rem; color: var(--n-text-color-3)">
                <li v-for="(pt, j) in q.scoring_points" :key="j">{{ pt }}</li>
              </ul>
            </NCollapseItem>
          </NCollapse>
          <NText v-if="questionList.length > 12" depth="3">…共 {{ questionList.length }} 题</NText>
        </NCard>
      </NTabPane>

      <NTabPane name="interview" tab="面试">
        <NCard v-if="interviewerPack.length || questionList.length" title="面试官题单" size="small" style="margin-bottom: 1rem">
          <NText depth="3" style="display: block; margin-bottom: 0.75rem">
            候选人确认且题目生成后，可向本轮面试官发送限时链接（含参考答案，请勿转发）。
          </NText>
          <NSpace style="margin-bottom: 0.75rem">
            <NButton type="primary" size="small" :loading="sendingPack" :disabled="!questionList.length" @click="onSendInterviewerPack">
              发送题单邮件
            </NButton>
          </NSpace>
          <div v-if="interviewerPack.length">
            <div
              v-for="lk in interviewerPack"
              :key="lk.open_id"
              style="border-top: 1px solid rgba(0,0,0,.06); padding: 0.65rem 0"
            >
              <NText strong>{{ lk.name || lk.open_id }}</NText>
              <NText depth="3" style="display: block; font-size: 0.85rem">{{ lk.email || "无邮箱，请复制链接" }}</NText>
              <NText depth="3" style="font-size: 0.8rem">失效：{{ fmtTime(lk.expires_at) }}</NText>
              <NSpace size="small" style="margin-top: 0.35rem">
                <NButton size="tiny" @click="copyText(lk.url)">复制链接</NButton>
              </NSpace>
            </div>
          </div>
          <NText v-else depth="3">保存后将自动尝试发邮件；也可点击上方按钮重发。</NText>
        </NCard>
        <NCard v-if="canManualSchedule" title="HR 人工排期" size="small" style="margin-bottom: 1rem">
          <NText depth="3" style="display: block; margin-bottom: 0.75rem">
            人工接管约面：从「面试官档案 / 面试池」指定人选，再选手动时段或自动找空档。不手填 open_id。
            <NButton text type="primary" size="tiny" @click="router.push({ name: 'interviewers' })">
              去维护档案
            </NButton>
          </NText>
          <div>
            <NText strong>本轮面试官</NText>
            <NSelect
              v-model:value="assignMode"
              :options="assignModeOptions"
              style="margin-top: 0.35rem; max-width: 280px"
            />
            <NSelect
              v-if="assignMode === 'people'"
              v-model:value="selectedOpenIds"
              multiple
              filterable
              :options="profileOptions"
              placeholder="从面试官档案多选"
              style="margin-top: 0.5rem"
            />
            <NSelect
              v-else-if="assignMode === 'pool'"
              v-model:value="selectedPoolId"
              :options="poolSelectOptions"
              placeholder="选择面试池（将使用池内全部成员）"
              clearable
              style="margin-top: 0.5rem; max-width: 360px"
            />
            <NText v-else depth="3" style="display: block; margin-top: 0.5rem; font-size: 0.85rem">
              当前已指派：
              <template v-if="selectedOpenIds.length">
                {{ selectedOpenIds.map(interviewerLabel).join("、") }}
              </template>
              <template v-else>—</template>
            </NText>
            <NAlert
              v-if="selectedOpenIds.some((id) => /ou_smoke_/i.test(id))"
              type="warning"
              style="margin-top: 0.5rem"
              title="测试占位 open_id"
            >
              当前含 ou_smoke_*，飞书日历会报「用户不存在」。请改为「指定人员/面试池」，从通讯录选真实员工。
            </NAlert>
          </div>
          <div style="margin-top: 0.75rem">
            <NText strong>手工时段</NText>
            <div
              v-for="(s, i) in manualSlots"
              :key="i"
              style="display: flex; flex-wrap: wrap; gap: 0.5rem; align-items: center; margin-top: 0.5rem"
            >
              <input v-model="s.start" type="datetime-local" />
              <span>→</span>
              <input v-model="s.end" type="datetime-local" />
              <NInput v-model:value="s.location" placeholder="地点" style="width: 140px" size="small" />
              <NButton size="tiny" quaternary type="error" @click="removeManualSlot(i)">删</NButton>
            </div>
            <NSpace style="margin-top: 0.5rem" align="center">
              <NButton size="small" @click="addManualSlot">加时段</NButton>
              <NCheckbox v-model:checked="resendInvite">保存时段后重发邀约</NCheckbox>
            </NSpace>
          </div>
          <NSpace style="margin-top: 1rem" wrap>
            <NButton type="primary" :loading="savingManual" @click="submitManualSchedule('slots')">
              用手工时段排期
            </NButton>
            <NButton :loading="savingManual" @click="submitManualSchedule('auto')">
              自动找空档并重发
            </NButton>
            <NButton secondary :loading="savingManual" @click="submitManualSchedule('assignees')">
              仅更新面试官
            </NButton>
          </NSpace>
        </NCard>

        <NCard title="面试轮次" size="small">
          <NText depth="3" style="display: block; margin-bottom: 0.75rem">
            当前轮次：第 {{ currentRoundIndex + 1 }} 轮。共享日历为 HR 看板；面试官收个人邀请。
          </NText>
          <div v-if="!interviewRounds.length">
            <NText depth="3">尚未创建申请轮次（排期前会按 JD 面试计划生成）。</NText>
          </div>
          <div
            v-for="r in interviewRounds"
            :key="String(r.round_index) + (r.id || '')"
            style="border: 1px solid #eee; border-radius: 8px; padding: 0.75rem; margin-bottom: 0.75rem"
          >
            <NSpace justify="space-between" align="center" wrap>
              <div>
                <strong>第 {{ (r.round_index ?? 0) + 1 }} 轮</strong>
                {{ r.name || "" }}
                <NTag size="small" style="margin-left: 0.5rem">{{ r.status || "—" }}</NTag>
                <NTag v-if="r.outcome" size="small" type="info" style="margin-left: 0.35rem">{{ r.outcome }}</NTag>
              </div>
              <NSpace v-if="canAdvanceRound(r)">
                <NButton size="small" type="primary" @click="onAdvance(r, 'pass')">通过→下一轮</NButton>
                <NButton size="small" type="error" secondary @click="onAdvance(r, 'fail')">淘汰</NButton>
                <NButton size="small" @click="onAdvance(r, 'hold')">暂缓</NButton>
              </NSpace>
            </NSpace>
            <p style="margin: 0.4rem 0 0; font-size: 0.9rem">
              主题：{{ r.theme || "—" }} · 时长：{{ r.duration_minutes || 60 }} 分钟
            </p>
            <p style="margin: 0.25rem 0 0; font-size: 0.85rem; word-break: break-all">
              面试官 open_id：{{ formatOpenIds(r.assigned_open_ids) }}
            </p>
            <p v-if="r.provider_event_id" style="margin: 0.25rem 0 0; font-size: 0.85rem">
              飞书 event：{{ r.provider_event_id }}
            </p>
            <div
              v-if="canAdvanceRound(r)"
              style="margin-top: 0.75rem; padding-top: 0.75rem; border-top: 1px dashed #e5e5e5"
            >
              <NText strong style="display: block; margin-bottom: 0.5rem">本轮反馈（HR 代录）</NText>
              <NSpace align="center" wrap style="margin-bottom: 0.5rem">
                <span style="font-size: 0.85rem">评分 1–5</span>
                <NInputNumber
                  v-model:value="fbDraft(r.round_index).rating"
                  :min="1"
                  :max="5"
                  size="small"
                  style="width: 100px"
                />
                <NSelect
                  v-model:value="fbDraft(r.round_index).recommend"
                  :options="recommendOptions"
                  clearable
                  placeholder="推荐度"
                  size="small"
                  style="width: 140px"
                />
              </NSpace>
              <NInput
                v-model:value="fbDraft(r.round_index).summary"
                type="textarea"
                placeholder="面评摘要…"
                :autosize="{ minRows: 2, maxRows: 4 }"
              />
            </div>
            <div v-else-if="r.feedback_json" style="margin-top: 0.5rem; font-size: 0.85rem">
              <NText depth="3">
                反馈：
                <span v-if="r.feedback_json.rating">评分 {{ r.feedback_json.rating }} · </span>
                <span v-if="r.feedback_json.recommend">推荐 {{ r.feedback_json.recommend }} · </span>
                {{ r.feedback_json.summary || r.feedback_json.note || "—" }}
              </NText>
            </div>
          </div>
        </NCard>
      </NTabPane>

      <NTabPane name="offer" tab="Offer">
        <NCard v-if="showOfferCard" title="Offer 状态" size="small">
          <NSpace align="center" wrap style="margin-bottom: 0.75rem">
            <NText>状态：</NText>
            <NTag :type="offerStatus === 'hired' ? 'success' : offerStatus === 'declined' ? 'error' : 'info'">
              {{ OFFER_LABEL[offerStatus] || offerStatus }}
            </NTag>
            <NText v-if="app.offer_updated_at" depth="3" style="font-size: 0.85rem">
              更新于 {{ fmtTime(app.offer_updated_at) }}
            </NText>
            <NText v-if="app.hired_at" depth="3" style="font-size: 0.85rem">
              入职 {{ fmtTime(app.hired_at) }}
            </NText>
          </NSpace>
          <NInput
            v-model:value="offerNote"
            type="textarea"
            placeholder="Offer 备注（可选）…"
            :autosize="{ minRows: 2, maxRows: 4 }"
            style="margin-bottom: 0.75rem"
          />
          <NSpace wrap>
            <NButton
              v-for="a in offerActions"
              :key="a.status"
              :type="a.type || 'default'"
              :loading="savingOffer"
              size="small"
              @click="onOffer(a.status)"
            >
              {{ a.label }}
            </NButton>
          </NSpace>
        </NCard>
        <NText v-else depth="3">全部轮次通过后进入待发 Offer；当前尚无 Offer 流程。</NText>
      </NTabPane>

      <NTabPane name="audit" tab="运行记录">
        <NCard title="Agent / 系统运行记录" size="small">
          <div v-for="a in auditDesc" :key="a.created_at + a.action" style="margin-bottom: 0.5rem">
            <NText depth="3" style="font-size: 0.82rem">{{ fmtTime(a.created_at) }}</NText>
            — <strong>{{ ACTION_LABEL[a.action] || a.action }}</strong>
            <span v-if="a.after_status"> → {{ statusLabel(a.after_status) }}</span>
            <span v-if="a.detail?.reason || a.detail?.error">
              （{{ a.detail.reason || a.detail.error }}）
            </span>
          </div>
          <NText v-if="!auditDesc.length" depth="3">暂无运行记录</NText>
        </NCard>
      </NTabPane>
    </NTabs>

    <NCard v-if="showContactEdit" title="修改联系方式" style="margin-top: 1rem; max-width: 480px">
      <NInput v-model:value="editEmail" placeholder="邮箱" />
      <NInput v-model:value="editName" placeholder="姓名" style="margin-top: 0.5rem" />
      <NSpace style="margin-top: 0.75rem">
        <NButton @click="showContactEdit = false">取消</NButton>
        <NButton type="primary" @click="saveContact">保存</NButton>
      </NSpace>
    </NCard>
  </div>
</template>
