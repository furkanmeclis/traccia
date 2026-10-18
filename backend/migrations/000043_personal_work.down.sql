-- Data-loss note: removes T14 portfolio snapshots and private favorites/history.
UPDATE event_consumers SET event_names=array_remove(event_names,'favorites.set') WHERE name='workspace.activity' AND version=1;
DROP VIEW personal_favorite_targets;
DROP TABLE personal_favorite_history;
DROP TABLE personal_favorites;
DROP FUNCTION protect_personal_favorite();
DROP TABLE personal_portfolio_items;
DROP TABLE personal_portfolio_sessions;
DROP FUNCTION protect_personal_portfolio_item();
DROP FUNCTION protect_personal_portfolio_session();
