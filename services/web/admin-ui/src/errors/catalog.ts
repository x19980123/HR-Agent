/** Business / HTTP codes → HR-facing Chinese copy. */
export const ERROR_CATALOG: Record<string, string> = {
  unauthorized: "未登录或登录已过期，请重新登录",
  forbidden: "没有权限执行此操作",
  "rate limited": "请求过于频繁，请稍后再试",
  "invalid json": "请求数据格式不正确",
  "invalid body": "请求内容无效",
  "action required": "请选择操作类型",
  "email_body required": "邮件正文不能为空",
  "resume file required": "请上传简历文件",
  "admin index missing": "管理台页面缺失，请重新构建前端",
  "payload too large": "上传文件过大",
  "thread_id not found": "未找到邮件会话，请从申请详情重试",

  interview_plan_missing: "请先在岗位 JD 配置面试轮次与面试官需求",
  interviewers_unassigned: "未能自动指派足够面试官，请检查档案/池或改用人工指定",
  scheduling_verify_failed: "约面 Agent 校验未通过，请调整人选后重试或人工排期",
  scheduling_headcount_short: "指派人数不足，请补充面试官或调整 JD 需求人数",
  scheduling_role_unfilled: "有角色需求未凑齐，请检查档案角色分类或面试池",
  scheduling_role_mismatch: "人选角色与 JD 需求不匹配",
  scheduling_department_mismatch: "人选部门与 JD 要求不一致",
  scheduling_specialties_mismatch: "人选特长与 JD 要求不匹配",
  scheduling_duplicate_assignee: "同一人被重复指派",
  scheduling_cross_vendor_mismatch: "规则与校验模型结论不一致，已转人工复核",
  no_calendar_slots: "日历暂无可用空档，请改手动时段或扩大可用时间",
  "no calendar slots available": "日历暂无可用空档，请改手动时段或扩大可用时间",
  assigned_open_ids_required: "请先指定本轮面试官（档案或面试池）",
  "assigned_open_ids required": "请先指定本轮面试官（档案或面试池）",

  contact_missing: "缺少有效联系邮箱",
  contact_csv_parse_mismatch: "CSV 邮箱与简历解析不一致，请核对后改邮箱或重试",
  contact_placeholder: "当前为占位邮箱，请改为真实联系方式",
  needs_human_after_parse: "解析不足，已转人工",
  needs_human_after_scheduling: "排期指派校验失败，已转人工",

  network_error: "网络异常，请确认 Go API（:8080）与 Python Agent（:8000）已启动",
  feishu_invalid_open_id: "飞书不认识当前面试官 open_id。请到「面试官档案」用通讯录重新选人后再排期",
  feishu_smoke_open_id: "当前面试官 open_id 是测试占位符，飞书不认。请到「面试官档案」用通讯录换成真实员工后再排期",
  feishu_auth: "飞书鉴权失败。请检查 FEISHU_APP_ID/SECRET，并确认相关权限已发布",
  feishu_permission: "飞书通讯录权限不足。请在开放平台开通通讯录读权限并发布",
  feishu_generic: "飞书接口调用失败。请检查日历/通讯录配置，以及面试官是否为真实 open_id",

  "飞书应用未配置 FEISHU_APP_ID/SECRET": "飞书应用未配置，请在 .env 填写 FEISHU_APP_ID / SECRET",
};

export const HTTP_STATUS_CATALOG: Record<number, string> = {
  400: "请求无效，请检查填写内容",
  401: "未登录或登录已过期，请重新登录",
  403: "没有权限执行此操作",
  404: "资源不存在或已删除",
  409: "状态冲突，请刷新后重试",
  413: "上传内容过大",
  429: "请求过于频繁，请稍后再试",
  500: "服务异常，请稍后重试或查看运行记录",
  502: "上游服务不可用，请确认 Python Agent / 飞书是否正常",
  503: "服务暂时不可用，请稍后重试",
};

export function catalogLookup(code: string | undefined | null): string | null {
  if (!code) return null;
  const t = code.trim();
  if (!t) return null;
  if (ERROR_CATALOG[t]) return ERROR_CATALOG[t];
  const lower = t.toLowerCase();
  if (ERROR_CATALOG[lower]) return ERROR_CATALOG[lower];
  for (const [k, label] of Object.entries(ERROR_CATALOG)) {
    if (lower === k.toLowerCase()) return label;
    if (lower.startsWith(k.toLowerCase() + ":") || lower.startsWith(k.toLowerCase() + " ")) return label;
    if (k.length >= 10 && lower.includes(k.toLowerCase())) return label;
  }
  return null;
}
