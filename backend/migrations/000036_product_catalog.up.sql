-- Product catalog identities and historical associations remain stable on archive.
CREATE TABLE products (
 id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
 name TEXT NOT NULL CHECK(char_length(name) BETWEEN 1 AND 100 AND name=btrim(name) AND name !~ '[[:cntrl:]]'),
 summary TEXT NOT NULL DEFAULT '' CHECK(char_length(summary)<=2000),
 repository_url TEXT NOT NULL DEFAULT '' CHECK(char_length(repository_url)<=2048 AND (repository_url='' OR repository_url ~ '^https?://')),
 repository_provider TEXT NOT NULL DEFAULT '' CHECK(char_length(repository_provider)<=100),
 repository_reference TEXT NOT NULL DEFAULT '' CHECK(char_length(repository_reference)<=200),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0), archived BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,id), UNIQUE(workspace_id,uuid)
);
CREATE UNIQUE INDEX idx_products_name ON products(workspace_id,lower(name));
CREATE INDEX idx_products_page ON products(workspace_id,archived,uuid);
CREATE TRIGGER trg_products_updated_at BEFORE UPDATE ON products FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TABLE product_environments (
 id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL, product_id BIGINT NOT NULL,
 name TEXT NOT NULL CHECK(char_length(name) BETWEEN 1 AND 100 AND name=btrim(name) AND name !~ '[[:cntrl:]]'),
 kind TEXT NOT NULL CHECK(kind IN ('production','staging','local','custom')),
 url TEXT NOT NULL CHECK(char_length(url) BETWEEN 1 AND 2048 AND url ~ '^https?://'),
 deploy_url TEXT NOT NULL DEFAULT '' CHECK(char_length(deploy_url)<=2048 AND (deploy_url='' OR deploy_url ~ '^https?://')),
 deploy_provider TEXT NOT NULL DEFAULT '' CHECK(char_length(deploy_provider)<=100),
 deploy_reference TEXT NOT NULL DEFAULT '' CHECK(char_length(deploy_reference)<=200),
 summary TEXT NOT NULL DEFAULT '' CHECK(char_length(summary)<=2000),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0), archived BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(workspace_id,product_id) REFERENCES products(workspace_id,id),
 UNIQUE(workspace_id,product_id,id), UNIQUE(workspace_id,product_id,uuid)
);
CREATE UNIQUE INDEX idx_product_environments_name ON product_environments(workspace_id,product_id,lower(name));
CREATE INDEX idx_product_environments_page ON product_environments(workspace_id,product_id,archived,uuid);
CREATE TRIGGER trg_product_environments_updated_at BEFORE UPDATE ON product_environments FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TABLE product_projects (
 id BIGSERIAL PRIMARY KEY, workspace_id BIGINT NOT NULL, product_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
 archived BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,product_id,project_id),
 FOREIGN KEY(workspace_id,product_id) REFERENCES products(workspace_id,id),
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
CREATE INDEX idx_product_projects_project ON product_projects(workspace_id,project_id,archived,product_id);
CREATE TRIGGER trg_product_projects_updated_at BEFORE UPDATE ON product_projects FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE FUNCTION protect_product_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.uuid,NEW.workspace_id) IS DISTINCT FROM ROW(OLD.uuid,OLD.workspace_id) THEN
  RAISE EXCEPTION 'product identity is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_product_identity BEFORE UPDATE ON products FOR EACH ROW EXECUTE FUNCTION protect_product_identity();
CREATE FUNCTION protect_product_environment_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.uuid,NEW.workspace_id,NEW.product_id) IS DISTINCT FROM ROW(OLD.uuid,OLD.workspace_id,OLD.product_id) THEN
  RAISE EXCEPTION 'product environment identity is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_product_environment_identity BEFORE UPDATE ON product_environments FOR EACH ROW EXECUTE FUNCTION protect_product_environment_identity();
CREATE FUNCTION protect_product_project_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.workspace_id,NEW.product_id,NEW.project_id) IS DISTINCT FROM ROW(OLD.workspace_id,OLD.product_id,OLD.project_id) THEN
  RAISE EXCEPTION 'product project identity is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_product_project_identity BEFORE UPDATE ON product_projects FOR EACH ROW EXECUTE FUNCTION protect_product_project_identity();
ALTER TABLE workspace_history ADD COLUMN product_uuid UUID, ADD COLUMN environment_uuid UUID;
UPDATE event_consumers SET event_names=event_names || ARRAY['products.created','products.updated','products.archived','products.restored','product_projects.updated','product_environments.created','product_environments.updated','product_environments.archived','product_environments.restored'] WHERE name='workspace.activity' AND version=1;
