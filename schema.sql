CREATE TABLE resources (
    object JSONB NOT NULL,
    kind TEXT NOT NULL GENERATED ALWAYS AS (jsonb_extract(object, '$.kind')),
    name TEXT NOT NULL GENERATED ALWAYS AS (jsonb_extract(object, '$.metadata.name')),
    namespace TEXT NOT NULL GENERATED ALWAYS AS (jsonb_extract(object, '$.metadata.namespace'))
);
