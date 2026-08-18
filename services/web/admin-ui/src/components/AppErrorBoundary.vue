<script setup lang="ts">
import { ref, onErrorCaptured } from "vue";
import { NResult, NButton, NCard, NCollapse, NCollapseItem, NText } from "naive-ui";
import { toAppError } from "@/errors";

const isDev = import.meta.env.DEV;
const failed = ref(false);
const userMessage = ref("");
const technical = ref("");

onErrorCaptured((err) => {
  const appErr = toAppError(err, "页面渲染异常，请刷新重试");
  failed.value = true;
  userMessage.value = appErr.userMessage;
  technical.value = appErr.technical || String(err);
  if (import.meta.env.DEV) {
    console.error("[ErrorBoundary]", err);
  }
  return false;
});

function reload() {
  window.location.reload();
}

function reset() {
  failed.value = false;
  userMessage.value = "";
  technical.value = "";
}
</script>

<template>
  <div v-if="failed" style="padding: 2rem 1rem; max-width: 640px; margin: 0 auto">
    <NCard>
      <NResult status="error" title="出错了" :description="userMessage">
        <template #footer>
          <NButton type="primary" style="margin-right: 0.5rem" @click="reload">刷新页面</NButton>
          <NButton @click="reset">尝试恢复</NButton>
        </template>
      </NResult>
      <NCollapse v-if="isDev && technical" style="margin-top: 1rem">
        <NCollapseItem title="技术详情（仅开发环境）" name="tech">
          <NText depth="3" style="font-size: 0.8rem; word-break: break-all; white-space: pre-wrap">
            {{ technical }}
          </NText>
        </NCollapseItem>
      </NCollapse>
    </NCard>
  </div>
  <slot v-else />
</template>
