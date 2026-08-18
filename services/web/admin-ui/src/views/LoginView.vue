<script setup lang="ts">
import { ref, onMounted } from "vue";
import { useRouter, useRoute } from "vue-router";
import {
  NCard,
  NButton,
  NInput,
  NSpace,
  NText,
} from "naive-ui";
import { useAuthStore } from "@/stores/auth";
import { api } from "@/api/client";
import { useNotify } from "@/composables/useNotify";

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();
const notify = useNotify();

const token = ref("");
const feishuEnabled = ref(false);
const tokenLoginEnabled = ref(true);
const loading = ref(false);

onMounted(async () => {
  const loginErr = String(route.query.login_error || "");
  if (loginErr) {
    notify.from(
      loginErr === "session"
        ? "登录会话无效，请重新登录"
        : loginErr === "denied"
          ? "无权访问管理台，请联系管理员开通 HR/Admin"
          : loginErr,
      "登录失败",
    );
  }
  try {
    const cfg = await api<{ feishu_login?: boolean; token_login?: boolean }>("/v1/auth/config");
    feishuEnabled.value = !!cfg.feishu_login;
    tokenLoginEnabled.value = cfg.token_login !== false;
  } catch {
    /* ignore */
  }
});

async function submitToken() {
  loading.value = true;
  try {
    await auth.loginWithToken(token.value);
    notify.success("登录成功");
    const redirect = (route.query.redirect as string) || "/";
    router.push(redirect);
  } catch (e) {
    notify.from(e, "登录失败");
  } finally {
    loading.value = false;
  }
}

function feishuLogin() {
  window.location.href = "/v1/auth/feishu/login";
}
</script>

<template>
  <div class="login-shell">
    <NCard title="HR Agent" style="width: min(440px, 92vw); border-radius: 20px">
      <NText depth="3">Vue 3 + Naive UI 管理台</NText>
      <NSpace vertical :size="16" style="margin-top: 1.25rem">
        <NButton v-if="feishuEnabled" type="primary" block @click="feishuLogin">
          飞书登录
        </NButton>
        <template v-if="tokenLoginEnabled">
          <NInput v-model:value="token" type="password" placeholder="HR API Token" />
          <NButton type="primary" block :loading="loading" @click="submitToken">
            Token 登录
          </NButton>
        </template>
      </NSpace>
    </NCard>
  </div>
</template>

<style scoped>
.login-shell {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 2rem 1rem;
}
</style>
