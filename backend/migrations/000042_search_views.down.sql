-- Data-loss note: removes all query snapshots, private saved views and columns.
UPDATE event_consumers SET event_names=ARRAY(SELECT e FROM unnest(event_names)e WHERE e NOT IN ('saved_views.created','saved_views.updated','saved_views.deleted','saved_views.restored','column_preferences.set')) WHERE name='workspace.activity' AND version=1;
DROP FUNCTION search_filter_matches(JSONB,JSONB);
DROP FUNCTION search_text_fold(TEXT);
DROP VIEW search_source_rows;
DROP TABLE private_preference_history,column_preferences,saved_views;
DROP FUNCTION protect_private_preference();
DROP TABLE search_query_items;
DROP FUNCTION protect_search_item();
DROP TABLE search_query_sessions;
DROP FUNCTION protect_search_session();
