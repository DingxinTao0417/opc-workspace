/**
 * Human labels for the Sidecar's closed tool registry. Labels describe the
 * tool's capability only; a proposal or request tool finishing never means
 * the underlying action was approved or executed.
 */
const aiToolLabels: Record<string, string> = {
  memory_search: "查找会话记忆",
  memory_write: "记录会话事实",
  memory_propose: "整理记忆建议",
  workspace_guide: "读取工作台规则",
  workspace_request_access: "请求下条消息的工作台权限（尚未授权）",
  workspace_search: "搜索工作台",
  workspace_today: "汇总今日工作台",
  workspace_tasks: "按条件筛选任务",
  workspace_projects: "查询项目组合",
  workspace_get: "读取工作事项",
  workspace_outputs: "查看执行产出",
  workspace_agent_runs: "查询智能体执行列表",
  workspace_agent_execution: "检查智能体执行条件",
  workspace_agent_project_files: "核对跨任务已验收文件",
  workspace_inbox_tasks: "查看收件箱关联任务",
  workspace_inbox_source: "核对收件箱来源",
  workspace_task_options: "查找分派与标签选项",
  workspace_task_assignments: "查询任务责任分派",
  workspace_task_views: "查询任务保存视图",
  workspace_task_submissions: "查询提交与验收记录",
  workspace_read_artifact_file: "校验并读取产出文件",
  workspace_focus: "查询专注记录",
  workspace_focus_report: "汇总专注报告",
  workspace_project_notes: "查询项目笔记",
  workspace_project_outputs: "查询项目产出与跟进",
  workspace_content_items: "查询内容排期与准备任务",
  workspace_roadmap_milestones: "查询路线图里程碑",
  workspace_client_records: "查询客户记录",
  workspace_automations: "查询自动化记录",
  workspace_finance: "查询财务记录",
  workspace_propose: "准备操作建议（仍需确认）",
  workspace_plan: "整理多步计划（不自动执行）",
  workspace_open_panel: "请求打开右侧工作区标签（仅本地布局）",
  workspace_request_record_navigation: "请求定位工作台记录（待你确认）",
  workspace_request_browser_navigation: "请求打开网页标签（待你确认）",
  workspace_request_browser_action: "请求操作当前网页标签（待你确认）",
  workspace_delegate_agent: "准备子智能体委派（仍需确认）",
  workspace_agent_followup: "准备子智能体续交办（仍需确认）",
  workspace_agent_children: "查看子智能体状态与回复",
  knowledge_search: "检索已授权知识",
  knowledge_read: "读取知识片段",
  knowledge_library: "查询知识库元数据",
};

export function aiToolLabel(name: string | null | undefined): string | null {
  return name ? (aiToolLabels[name] ?? null) : null;
}
