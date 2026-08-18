import { createRouter, createWebHistory } from "vue-router";
import { useAuthStore } from "@/stores/auth";

const router = createRouter({
  history: createWebHistory("/admin/"),
  routes: [
    {
      path: "/login",
      name: "login",
      component: () => import("@/views/LoginView.vue"),
      meta: { public: true },
    },
    {
      path: "/",
      component: () => import("@/layouts/AppLayout.vue"),
      children: [
        { path: "", name: "stats", component: () => import("@/views/StatsView.vue") },
        { path: "applications", name: "applications", component: () => import("@/views/ApplicationsView.vue") },
        {
          path: "applications/:id",
          name: "application-detail",
          component: () => import("@/views/ApplicationDetailView.vue"),
        },
        { path: "human", name: "human", component: () => import("@/views/HumanDeskView.vue") },
        { path: "create", name: "create", component: () => import("@/views/CreateView.vue") },
        { path: "jds", name: "jds", component: () => import("@/views/JdsView.vue") },
        { path: "jds/:id", name: "jd-detail", component: () => import("@/views/JdDetailView.vue") },
        {
          path: "interviewers",
          name: "interviewers",
          component: () => import("@/views/InterviewersView.vue"),
        },
        {
          path: "bank",
          name: "bank",
          meta: { requiresQuestionBank: true },
          component: () => import("@/views/BankView.vue"),
        },
        {
          path: "staff",
          name: "staff",
          meta: { requiresAdmin: true },
          component: () => import("@/views/StaffView.vue"),
        },
      ],
    },
  ],
});

router.beforeEach(async (to) => {
  const auth = useAuthStore();
  if (!auth.loaded) {
    try {
      await auth.fetchMe();
    } catch {
      auth.me = null;
      auth.loaded = true;
    }
  }
  if (to.meta.public) {
    if (auth.isLoggedIn && to.name === "login") return { name: "stats" };
    return true;
  }
  if (!auth.isLoggedIn) return { name: "login", query: { redirect: to.fullPath } };
  if (to.meta.requiresAdmin && !auth.isAdmin) return { name: "stats" };
  if (to.meta.requiresQuestionBank && !auth.canManageQuestionBank) return { name: "stats" };
  return true;
});

export default router;
