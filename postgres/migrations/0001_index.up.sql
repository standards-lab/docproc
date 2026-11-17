-- The index's entries: one row per key, the content's SHA-256 and the stage identity's name,
-- version, and config, inserted by the Claim that first claims the key. token is the claim's
-- while the key is claimed, and stays, once the key is recorded, the token whose log the entry
-- holds; a takeover replaces it. expires_at is the lease's expiry on the database's clock, NULL
-- for a claim that never expires and for a recorded entry, whose claim has ended. entry is the
-- dispatch.Entry as JSON, NULL until Record: a row with an entry is recorded, one without is
-- claimed. dispatch_version is the dispatch.Version whose shapes the row's entry and its log's
-- items carry, set by the writer that claims or records the key. created_at is the claim's
-- time, which a retention reads.
CREATE TABLE docproc_entry (
    sha256           text        NOT NULL,
    stage_name       text        NOT NULL,
    stage_version    text        NOT NULL,
    stage_config     text        NOT NULL,
    token            text        NOT NULL,
    expires_at       timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    dispatch_version text        NOT NULL,
    entry            jsonb,
    CONSTRAINT docproc_pk_entry PRIMARY KEY (sha256, stage_name, stage_version, stage_config),
    CONSTRAINT docproc_ck_entry_lease CHECK (entry IS NULL OR expires_at IS NULL)
);

-- A retention drops entries by creation age, oldest first, so it reads this index in
-- created_at order and stops at its budget.
CREATE INDEX docproc_ix_entry_created ON docproc_entry (created_at);

-- An entry's log: one row per dispatch.Recorded item, as JSON, keyed by its entry, the token
-- that appended it, and its position in append order from 0. Only the entry's current token's
-- items are its log: a takeover deletes the old token's, and a Release or a Drop deletes the
-- entry, whose items go with it.
CREATE TABLE docproc_item (
    sha256        text    NOT NULL,
    stage_name    text    NOT NULL,
    stage_version text    NOT NULL,
    stage_config  text    NOT NULL,
    token         text    NOT NULL,
    seq           integer NOT NULL,
    item          jsonb   NOT NULL,
    CONSTRAINT docproc_pk_item PRIMARY KEY (sha256, stage_name, stage_version, stage_config, token, seq),
    CONSTRAINT docproc_fk_item_entry FOREIGN KEY (sha256, stage_name, stage_version, stage_config)
        REFERENCES docproc_entry (sha256, stage_name, stage_version, stage_config) ON DELETE CASCADE,
    CONSTRAINT docproc_ck_item_seq CHECK (seq >= 0)
);
