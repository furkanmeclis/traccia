export const issueFieldKeys: Record<string, string> = {
  team_id: "issues.team_id",
  status_id: "issues.status_id",
  assignee_id: "issues.assignee_id",
  project_id: "issues.project_id",
  milestone_id: "issues.milestone_id",
  priority: "issues.priority",
  labels: "issues.labels",
  start_date: "issues.start_date",
  target_date: "issues.target_date",
  effort: "issues.effort",
  lifecycle: "issues.lifecycle",
};
export const issuePriorityKeys = {
  none: "issues.priority_none",
  urgent: "issues.priority_urgent",
  high: "issues.priority_high",
  normal: "issues.priority_normal",
  low: "issues.priority_low",
};
export const issueEventKeys: Record<string, string> = {
  "issues.created": "issues.event_issues_created",
  "issues.updated": "issues.event_issues_updated",
  "issues.moved": "issues.event_issues_moved",
  "issues.archived": "issues.event_issues_archived",
  "issues.restored": "issues.event_issues_restored",
};
