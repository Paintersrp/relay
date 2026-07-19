-- +goose Up

CREATE TABLE source_path_selectors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    selector_id TEXT NOT NULL UNIQUE,
    packet_row_id INTEGER NOT NULL REFERENCES operation_packets(id) ON DELETE RESTRICT,
    packet_id TEXT NOT NULL,
    surface_contract_id TEXT NOT NULL,
    operation_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    repository_key TEXT NOT NULL,
    publication_id TEXT NOT NULL REFERENCES operation_packet_publications(publication_id) ON DELETE RESTRICT,
    vault_relationship_row_id INTEGER NOT NULL REFERENCES operation_packet_vault_relationships(id) ON DELETE RESTRICT,
    commit_oid TEXT NOT NULL,
    tree_oid TEXT NOT NULL,
    path_id TEXT NOT NULL,
    path_byte_length INTEGER NOT NULL,
    path_bytes BLOB NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (length(selector_id) = 70),
    CHECK (substr(selector_id, 1, 6) = 'spath-'),
    CHECK (length(commit_oid) = 40),
    CHECK (length(tree_oid) = 40),
    CHECK (length(path_id) = 64),
    CHECK (path_byte_length > 0),
    CHECK (length(path_bytes) = path_byte_length),
    UNIQUE (
        packet_row_id,
        surface_contract_id,
        operation_id,
        project_id,
        repository_key,
        publication_id,
        vault_relationship_row_id,
        commit_oid,
        tree_oid,
        path_id
    )
);

CREATE INDEX source_path_selectors_packet_idx
    ON source_path_selectors (packet_row_id, repository_key, path_id);
