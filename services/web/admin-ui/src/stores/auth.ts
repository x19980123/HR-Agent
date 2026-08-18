import { defineStore } from "pinia";
import { ref, computed } from "vue";
import { api, setApiToken, type AuthMe } from "@/api/client";

export const useAuthStore = defineStore("auth", () => {
  const me = ref<AuthMe | null>(null);
  const loaded = ref(false);

  const isLoggedIn = computed(() => !!me.value);
  const displayName = computed(() => me.value?.user?.name || "已登录");
  const isAdmin = computed(
    () => !!(me.value?.is_admin || me.value?.user?.is_admin),
  );
  const canManageQuestionBank = computed(
    () =>
      !!(
        me.value?.can_manage_question_bank ||
        me.value?.user?.can_manage_question_bank
      ),
  );

  async function fetchMe() {
    me.value = await api<AuthMe>("/v1/auth/me");
    loaded.value = true;
  }

  async function loginWithToken(token: string) {
    setApiToken(token.trim());
    await fetchMe();
  }

  async function logout() {
    try {
      await api("/v1/auth/logout", { method: "POST", json: {} });
    } catch {
      /* ignore */
    }
    setApiToken("");
    me.value = null;
  }

  return {
    me,
    loaded,
    isLoggedIn,
    displayName,
    isAdmin,
    canManageQuestionBank,
    fetchMe,
    loginWithToken,
    logout,
  };
});
