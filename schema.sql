CREATE TABLE resources (
    kind TEXT NOT NULL GENERATED ALWAYS AS (jsonb_extract(object, '$.kind')),
    namespace TEXT GENERATED ALWAYS AS (jsonb_extract(object, '$.metadata.namespace')),
    name TEXT NOT NULL GENERATED ALWAYS AS (jsonb_extract(object, '$.metadata.name')),
    object JSONB NOT NULL
);

CREATE UNIQUE INDEX resources_pk ON resources(kind, namespace, name);
