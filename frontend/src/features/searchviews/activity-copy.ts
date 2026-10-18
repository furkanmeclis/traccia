/** Personal preference activity never includes private names, queries or columns. */
export const searchActivityKeys: Record<string, string> = {
  "saved_views.created": "searchviews.activityViewCreated",
  "saved_views.updated": "searchviews.activityViewUpdated",
  "saved_views.deleted": "searchviews.activityViewDeleted",
  "saved_views.restored": "searchviews.activityViewRestored",
  "column_preferences.set": "searchviews.activityColumnsSet",
};
