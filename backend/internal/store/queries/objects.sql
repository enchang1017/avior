-- name: RecordDefinitionRead :exec
INSERT INTO definition_read (
    id, user_id, instance_id, database, object_type, schema_name, object_name, found
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListDefinitionReadsForUser :many
SELECT id, instance_id, database, object_type, schema_name, object_name, found, created_at
FROM definition_read
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2;
