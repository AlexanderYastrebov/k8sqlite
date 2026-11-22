CREATE TABLE resources (
    kind TEXT NOT NULL GENERATED ALWAYS AS (object->>'$.kind'),
    namespace TEXT NOT NULL GENERATED ALWAYS AS (coalesce(object->>'$.metadata.namespace', '')),
    name TEXT NOT NULL GENERATED ALWAYS AS (object->>'$.metadata.name'),
    object JSONB NOT NULL
);

CREATE UNIQUE INDEX resources_pk ON resources(kind, namespace, name);
