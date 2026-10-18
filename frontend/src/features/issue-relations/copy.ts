export const relationEventKeys: Record<string, string> = {
  "issue_relations.parent_set": "relations.parent_changed",
  "issue_relations.duplicate_set": "relations.duplicate_changed",
  "issue_relations.block_linked": "relations.block_linked",
  "issue_relations.block_unlinked": "relations.block_unlinked",
  "issue_relations.related_linked": "relations.related_linked",
  "issue_relations.related_unlinked": "relations.related_unlinked",
};
export const relationActivityKeys: Record<string, string> = {
  "issue_relations.parent_set": "relations.activity_parent_changed",
  "issue_relations.duplicate_set": "relations.activity_duplicate_changed",
  "issue_relations.block_linked": "relations.activity_block_linked",
  "issue_relations.block_unlinked": "relations.activity_block_unlinked",
  "issue_relations.related_linked": "relations.activity_related_linked",
  "issue_relations.related_unlinked": "relations.activity_related_unlinked",
};
