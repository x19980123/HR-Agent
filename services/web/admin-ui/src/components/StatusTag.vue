<script setup lang="ts">
import { NTag } from "naive-ui";
import { statusLabel } from "@/constants/status";
import { computed } from "vue";

const props = defineProps<{ status: string }>();

const type = computed(() => {
  const s = props.status;
  if (["failed", "declined", "rejected"].includes(s)) return "error";
  if (["needs_human", "parsing", "screening", "uploaded"].includes(s)) return "warning";
  if (["confirmed", "screened", "questions_ready"].includes(s)) return "success";
  return "default";
});
</script>

<template>
  <NTag :type="type" size="small" round>{{ statusLabel(status) }}</NTag>
</template>
