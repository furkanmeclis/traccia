-- Data-loss note: removes catalog data, historical product and environment context.
UPDATE event_consumers SET event_names=ARRAY(SELECT e FROM unnest(event_names) e WHERE e NOT LIKE 'products.%' AND e NOT LIKE 'product_projects.%' AND e NOT LIKE 'product_environments.%') WHERE name='workspace.activity' AND version=1;
ALTER TABLE workspace_history DROP COLUMN product_uuid, DROP COLUMN environment_uuid;
DROP TABLE product_projects,product_environments,products;
DROP FUNCTION protect_product_project_identity();
DROP FUNCTION protect_product_environment_identity();
DROP FUNCTION protect_product_identity();
