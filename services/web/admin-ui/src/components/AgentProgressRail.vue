<script setup lang="ts">
import { NText, NTag } from "naive-ui";
import type { AgentPipeline } from "@/constants/status";

defineProps<{
  pipeline: AgentPipeline;
}>();

const MODE_TAG: Record<string, { type: "default" | "info" | "success" | "warning" | "error"; text: string }> = {
  running: { type: "info", text: "Agent 运行中" },
  wait: { type: "warning", text: "等待中" },
  human: { type: "warning", text: "人工接管" },
  fail: { type: "error", text: "失败" },
  done: { type: "success", text: "已完成" },
};
</script>

<template>
  <div class="agent-rail">
    <div class="agent-rail__meta">
      <NTag size="small" :type="MODE_TAG[pipeline.mode]?.type || 'default'" :bordered="false">
        {{ MODE_TAG[pipeline.mode]?.text || pipeline.mode }}
      </NTag>
      <NText class="agent-rail__caption">{{ pipeline.caption }}</NText>
    </div>
    <div class="agent-rail__track" role="list">
      <template v-for="(step, i) in pipeline.steps" :key="step.key">
        <div
          class="agent-rail__node"
          :class="[`is-${step.state}`, { 'is-pulse': step.state === 'current' && pipeline.mode === 'running' }]"
          role="listitem"
        >
          <span class="agent-rail__dot" />
          <span class="agent-rail__label">{{ step.label }}</span>
        </div>
        <div v-if="i < pipeline.steps.length - 1" class="agent-rail__line" :class="{ 'is-done': i < pipeline.currentIndex }" />
      </template>
    </div>
  </div>
</template>

<style scoped>
.agent-rail {
  border: 1px solid #e8e8e8;
  border-radius: 10px;
  padding: 0.75rem 1rem 0.85rem;
  background: linear-gradient(180deg, #fafbfc 0%, #fff 100%);
  margin-bottom: 1rem;
}
.agent-rail__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem 0.75rem;
  margin-bottom: 0.65rem;
}
.agent-rail__caption {
  font-size: 0.9rem;
  font-weight: 560;
  color: #1f2329;
}
.agent-rail__track {
  display: flex;
  align-items: flex-start;
  overflow-x: auto;
  padding-bottom: 0.15rem;
}
.agent-rail__node {
  display: flex;
  flex-direction: column;
  align-items: center;
  min-width: 4.5rem;
  gap: 0.3rem;
  flex-shrink: 0;
}
.agent-rail__dot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: #d0d3d6;
  border: 2px solid #fff;
  box-shadow: 0 0 0 1px #d0d3d6;
}
.agent-rail__label {
  font-size: 0.72rem;
  color: #8a8f98;
  text-align: center;
  line-height: 1.25;
  max-width: 4.8rem;
}
.agent-rail__line {
  flex: 1 1 18px;
  min-width: 12px;
  height: 2px;
  margin-top: 5px;
  background: #e5e6eb;
}
.agent-rail__line.is-done {
  background: #18a058;
}
.agent-rail__node.is-done .agent-rail__dot {
  background: #18a058;
  box-shadow: 0 0 0 1px #18a058;
}
.agent-rail__node.is-done .agent-rail__label {
  color: #3c4048;
}
.agent-rail__node.is-current .agent-rail__dot {
  background: #2080f0;
  box-shadow: 0 0 0 1px #2080f0;
}
.agent-rail__node.is-current .agent-rail__label {
  color: #2080f0;
  font-weight: 600;
}
.agent-rail__node.is-failed .agent-rail__dot {
  background: #d03050;
  box-shadow: 0 0 0 1px #d03050;
}
.agent-rail__node.is-failed .agent-rail__label {
  color: #d03050;
  font-weight: 600;
}
.agent-rail__node.is-human .agent-rail__dot {
  background: #f0a020;
  box-shadow: 0 0 0 1px #f0a020;
}
.agent-rail__node.is-human .agent-rail__label {
  color: #c27803;
  font-weight: 600;
}
.agent-rail__node.is-pulse .agent-rail__dot {
  animation: agent-pulse 1.2s ease-in-out infinite;
}
@keyframes agent-pulse {
  0%,
  100% {
    transform: scale(1);
    opacity: 1;
  }
  50% {
    transform: scale(1.25);
    opacity: 0.75;
  }
}
</style>
