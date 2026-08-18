<script setup lang="ts">
import { ref, onMounted, h } from "vue";
import { useRouter } from "vue-router";
import {
  NButton,
  NCard,
  NForm,
  NFormItem,
  NGrid,
  NGi,
  NInput,
  NSelect,
  NText,
  NUpload,
  NDataTable,
} from "naive-ui";
import type { UploadFileInfo } from "naive-ui";
import {
  createApplicationJson,
  createApplicationMultipart,
  createImport,
  getApplication,
  getImportJob,
  listImportItems,
  retryImportItem,
  listJds,
  type ImportItemRow,
  type JDItem,
} from "@/api/admin";
import { isTerminalStatus, statusLabel } from "@/constants/status";
import { useNotify } from "@/composables/useNotify";

const router = useRouter();
const notify = useNotify();

const jds = ref<JDItem[]>([]);
const jdId = ref("");
const name = ref("");
const email = ref("");
const resumeText = ref("");
const fileList = ref<UploadFileInfo[]>([]);
const batchFiles = ref<UploadFileInfo[]>([]);
const batchEmail = ref("");
const mappingCsv = ref<UploadFileInfo[]>([]);
const archiveZip = ref<UploadFileInfo[]>([]);
const submitting = ref(false);
const progressStatus = ref("");
const progressId = ref("");
const batchMsg = ref("");
const failedItems = ref<ImportItemRow[]>([]);
const activeJobId = ref("");

onMounted(async () => {
  const data = await listJds();
  jds.value = data.items || [];
  if (jds.value.length) jdId.value = jds.value[0].id;
});

const jdOptions = () =>
  jds.value.map((j) => ({ label: `${j.title} (${j.id})`, value: j.id }));

async function pollApp(id: string) {
  progressId.value = id;
  const tick = async () => {
    try {
      const app = await getApplication(id);
      progressStatus.value = statusLabel(app.status);
      if (!isTerminalStatus(app.status)) {
        setTimeout(tick, 2000);
      } else {
        notify.success("流程已到达：" + progressStatus.value);
        setTimeout(() => router.push({ name: "application-detail", params: { id } }), 800);
      }
    } catch {
      /* ignore */
    }
  };
  tick();
}

async function submitSingle() {
  if (!jdId.value) {
    notify.warning("请选择 JD");
    return;
  }
  submitting.value = true;
  try {
    let out;
    const f = fileList.value[0]?.file;
    if (f) {
      const fd = new FormData();
      fd.append("jd_id", jdId.value);
      fd.append("candidate_name", name.value);
      fd.append("candidate_email", email.value);
      fd.append("resume", f);
      out = await createApplicationMultipart(fd);
    } else {
      out = await createApplicationJson({
        jd_id: jdId.value,
        candidate_name: name.value,
        candidate_email: email.value,
        resume_text: resumeText.value,
      });
    }
    const id = out.application_id;
    if (!id) throw new Error("未返回 application_id");
    notify.success("已创建申请");
    await pollApp(id);
  } catch (e) {
    notify.from(e, "提交失败");
  } finally {
    submitting.value = false;
  }
}

async function submitBatch() {
  if (!jdId.value) {
    notify.warning("请选择 JD");
    return;
  }
  const files = batchFiles.value.map((x) => x.file).filter(Boolean) as File[];
  const zip = archiveZip.value[0]?.file;
  if (!files.length && !zip) {
    notify.warning("请选择简历文件或 zip 压缩包");
    return;
  }
  const fd = new FormData();
  fd.append("jd_id", jdId.value);
  if (batchEmail.value.trim()) fd.append("default_email", batchEmail.value.trim());
  files.forEach((f) => fd.append("resume", f));
  const csv = mappingCsv.value[0]?.file;
  if (csv) fd.append("mapping_csv", csv);
  if (zip) fd.append("archive", zip);
  try {
    const out = await createImport(fd);
    const totalHint = out.total || files.length || (zip ? "…" : 0);
    batchMsg.value = `任务 ${out.job_id}，共 ${totalHint} 份`;
    activeJobId.value = out.job_id;
    failedItems.value = [];
    const poll = async () => {
      const job = await getImportJob(out.job_id);
      const total = job.total || out.total || files.length;
      const done = (job.succeeded || 0) + (job.failed || 0);
      batchMsg.value = `进度 ${done}/${total}（成功 ${job.succeeded || 0}，失败 ${job.failed || 0}）`;
      if (job.failed && job.failed > 0) {
        const rows = await listImportItems(out.job_id, "error");
        failedItems.value = rows.items || [];
      }
      if (done < total) setTimeout(poll, 2000);
    };
    poll();
  } catch (e) {
    notify.from(e, "批量失败");
  }
}

async function onRetryImportItem(row: ImportItemRow) {
  if (!activeJobId.value) return;
  try {
    await retryImportItem(activeJobId.value, row.id);
    notify.success("已重新排队");
    const rows = await listImportItems(activeJobId.value, "error");
    failedItems.value = rows.items || [];
  } catch (e) {
    notify.from(e, "重试失败");
  }
}

const failColumns = [
  { title: "文件/邮箱", key: "candidate_email", ellipsis: { tooltip: true } },
  { title: "错误", key: "error_message", ellipsis: { tooltip: true } },
  {
    title: "",
    key: "act",
    width: 88,
    render: (row: ImportItemRow) =>
      h(NButton, { size: "small", onClick: () => onRetryImportItem(row) }, { default: () => "重试" }),
  },
];
</script>

<template>
  <div>
    <div style="margin-bottom: 1rem">
      <h2 style="margin: 0 0 0.25rem">投递上传</h2>
      <NText depth="3">上传后自动接力：解析 Agent → 筛选 Agent → 约面 Agent</NText>
    </div>
    <NGrid cols="1 l:2" :x-gap="16">
      <NGi>
        <NCard title="单条创建">
          <NForm label-placement="top">
            <NFormItem label="岗位 JD">
              <NSelect v-model:value="jdId" :options="jdOptions()" placeholder="选择 JD" />
            </NFormItem>
            <NFormItem label="候选人姓名">
              <NInput v-model:value="name" />
            </NFormItem>
            <NFormItem label="候选人邮箱">
              <NInput v-model:value="email" />
            </NFormItem>
            <NFormItem label="简历文件">
              <NUpload v-model:file-list="fileList" :max="1" accept=".pdf,.doc,.docx,.txt">
                <NButton>选择文件</NButton>
              </NUpload>
            </NFormItem>
            <NFormItem label="或粘贴全文">
              <NInput v-model:value="resumeText" type="textarea" :autosize="{ minRows: 4, maxRows: 10 }" />
            </NFormItem>
            <NButton type="primary" :loading="submitting" @click="submitSingle">创建并启动流程</NButton>
            <NText v-if="progressId" depth="3" style="display: block; margin-top: 0.5rem">
              {{ progressId.slice(0, 8) }}… · {{ progressStatus }}
            </NText>
          </NForm>
        </NCard>

        <NCard title="批量导入（2.0）" style="margin-top: 1rem">
          <NFormItem label="多选简历">
            <NUpload v-model:file-list="batchFiles" multiple accept=".pdf,.doc,.docx,.txt">
              <NButton>选择多个文件</NButton>
            </NUpload>
          </NFormItem>
          <NFormItem label="映射 CSV（可选）">
            <NUpload v-model:file-list="mappingCsv" :max="1" accept=".csv">
              <NButton>filename,email,name</NButton>
            </NUpload>
            <NText depth="3" style="font-size: 0.82rem; display: block; margin-top: 0.35rem">
              表头含 filename / email / name（或 file、邮箱、姓名）；按文件名匹配简历。
            </NText>
          </NFormItem>
          <NFormItem label="或 zip 压缩包">
            <NUpload v-model:file-list="archiveZip" :max="1" accept=".zip">
              <NButton>上传 zip</NButton>
            </NUpload>
          </NFormItem>
          <NFormItem label="默认邮箱（可选）">
            <NInput
              v-model:value="batchEmail"
              placeholder="留空 → candidate@import.local"
            />
            <NText depth="3" style="font-size: 0.82rem; display: block; margin-top: 0.35rem">
              每个文件各创建一条申请、各有一个邮箱。仅 1 个文件时可填真实邮箱；多文件时会自动生成
              import1@域名、import2@域名…（从你填的地址取 @ 后面作域名，或留空用占位域）。
            </NText>
          </NFormItem>
          <NButton @click="submitBatch">提交批量导入</NButton>
          <NText depth="3" style="display: block; margin-top: 0.5rem">{{ batchMsg }}</NText>
          <NDataTable
            v-if="failedItems.length"
            style="margin-top: 0.75rem"
            size="small"
            :columns="failColumns"
            :data="failedItems"
          />
        </NCard>
      </NGi>
      <NGi>
        <NCard title="流程说明">
          <ol style="line-height: 1.7; padding-left: 1.2rem">
            <li>保存申请与简历</li>
            <li>解析（必要时 OCR）</li>
            <li>对照 JD 筛选（三档）</li>
            <li>通过后发邀约邮件</li>
            <li>确认后生成面试题</li>
          </ol>
        </NCard>
      </NGi>
    </NGrid>
  </div>
</template>
