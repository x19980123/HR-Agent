<script setup lang="ts">
import { NCard } from "naive-ui";

defineProps<{ title: string; hint: string }>();
</script>

<template>
  <NCard :title="title">
    <p style="color: var(--muted); margin: 0">{{ hint }}</p>
    <p style="margin-top: 0.75rem; line-height: 1.55">
      开发请用 <code>npm run dev</code>；生产切换见
      <code>docs/PROJECT-STRUCTURE.md</code>（<code>HR_ADMIN_VUE=1</code> + build）。
    </p>
  </NCard>
</template>
