<script setup lang="ts">
import { NConfigProvider, NMessageProvider, NDialogProvider } from "naive-ui";
import { themeOverrides } from "@/theme";
import AppErrorBoundary from "@/components/AppErrorBoundary.vue";
</script>

<template>
  <NConfigProvider :theme-overrides="themeOverrides">
    <NMessageProvider :max="4" placement="top">
      <NDialogProvider>
        <AppErrorBoundary>
          <RouterView />
        </AppErrorBoundary>
      </NDialogProvider>
    </NMessageProvider>
  </NConfigProvider>
</template>
