<script setup lang="ts">
import { h, computed } from "vue";
import { useRouter, useRoute, RouterLink } from "vue-router";
import { NLayout, NLayoutSider, NLayoutContent, NMenu, NButton, NSpace, NText } from "naive-ui";
import type { MenuOption } from "naive-ui";
import { useAuthStore } from "@/stores/auth";

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();

function link(name: string, label: string) {
  return () => h(RouterLink, { to: { name } }, () => label);
}

const menuOptions = computed<MenuOption[]>(() => {
  const items: MenuOption[] = [
    { label: link("stats", "Agent 工作台"), key: "stats" },
    {
      label: "候选人",
      key: "group-candidates",
      children: [
        { label: link("applications", "申请流水线"), key: "applications" },
        { label: link("human", "人工接管"), key: "human" },
        { label: link("create", "投递上传"), key: "create" },
      ],
    },
    {
      label: "岗位配置",
      key: "group-jobs",
      children: [
        { label: link("jds", "岗位与面试计划"), key: "jds" },
        { label: link("interviewers", "面试官档案"), key: "interviewers" },
      ],
    },
  ];

  const sysChildren: MenuOption[] = [];
  if (auth.canManageQuestionBank) {
    sysChildren.push({ label: link("bank", "题库 RAG"), key: "bank" });
  }
  if (auth.isAdmin) {
    sysChildren.push({ label: link("staff", "成员管理"), key: "staff" });
  }
  if (sysChildren.length) {
    items.push({ label: "系统", key: "group-system", children: sysChildren });
  }
  return items;
});

const activeKey = computed(() => {
  const name = String(route.name || "stats");
  if (name === "application-detail") return "applications";
  if (name === "jd-detail") return "jds";
  return name;
});

const expandedKeys = computed(() => {
  const keys = ["group-candidates", "group-jobs"];
  if (auth.canManageQuestionBank || auth.isAdmin) keys.push("group-system");
  return keys;
});

async function onLogout() {
  await auth.logout();
  router.push({ name: "login" });
}
</script>

<template>
  <NLayout has-sider style="min-height: 100vh">
    <NLayoutSider bordered :width="260" content-style="padding: 1rem 0.75rem; display: flex; flex-direction: column">
      <div style="padding: 0 0.5rem 1rem">
        <div style="font-weight: 700; font-size: 1.05rem">HR Agent</div>
        <NText depth="3" style="font-size: 0.82rem">多 Agent 招聘工作台</NText>
      </div>
      <NMenu :value="activeKey" :options="menuOptions" :default-expanded-keys="expandedKeys" />
      <div style="margin-top: auto; padding: 1rem 0.5rem 0">
        <NSpace vertical :size="8">
          <NText>{{ auth.displayName }}</NText>
          <NText depth="3" style="font-size: 0.8rem">
            {{ auth.isAdmin ? "管理员" : "HR" }}
          </NText>
          <NButton size="small" quaternary @click="onLogout">退出登录</NButton>
        </NSpace>
      </div>
    </NLayoutSider>
    <NLayoutContent content-style="padding: 1.5rem 2rem; max-width: 1200px">
      <RouterView />
    </NLayoutContent>
  </NLayout>
</template>
