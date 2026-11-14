CREATE TABLE resources (
    object JSONB NOT NULL,
    kind TEXT NOT NULL GENERATED ALWAYS AS (jsonb_extract(object, '$.kind')),
    namespace TEXT NOT NULL GENERATED ALWAYS AS (jsonb_extract(object, '$.metadata.namespace')),
    name TEXT NOT NULL GENERATED ALWAYS AS (jsonb_extract(object, '$.metadata.name'))
);

CREATE UNIQUE INDEX resources_pk ON resources(kind, namespace, name);
