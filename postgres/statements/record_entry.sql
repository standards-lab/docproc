--| tier: native
--| native: data-modifying WITH, unnest WITH ORDINALITY over a jsonb[] parameter, now(), and pg_notify in the update's RETURNING, so the entry, its log's last items, and the waiters' notification commit as one statement. Port: an engine must run the entry's update and the items' insert in one transaction, the insert only when the update changed a row, and signal the key's waiters in that transaction (SQL Server has no NOTIFY: a Service Broker queue, or the waiters polling the row on a short timer).
-- Records entry, the dispatch.Entry as JSON, as key's entry, with the claim's last buffered
-- items, at seq onward, while token holds key's claim: the entry, the items, and the claim's end
-- commit together or not at all. The lease is cleared, created_at becomes the record's time, from
-- which a retention counts the entry's age, and dispatch_version the writer's. It returns the
-- count of entries recorded: 1, or 0, inserting no item, when token no longer holds the claim.
-- A record notifies the key's waiters on docproc_index, the payload the lowercase hex SHA-256 of
-- the key's SHA-256, name, version, and config joined by NUL bytes: the update's RETURNING is
-- evaluated for each row it changes, so the notification is sent, at commit, exactly when the
-- entry is recorded.
WITH recorded AS (
    UPDATE docproc_entry
    SET entry = {{entry}}, expires_at = NULL, created_at = now(), dispatch_version = {{version}}
    WHERE sha256 = {{sha256}} AND stage_name = {{stage_name}}
      AND stage_version = {{stage_version}} AND stage_config = {{stage_config}}
      AND token = {{token}} AND entry IS NULL
    RETURNING sha256, stage_name, stage_version, stage_config, token,
        pg_notify('docproc_index', encode(sha256(
            convert_to(sha256, 'UTF8') || decode('00', 'hex') || convert_to(stage_name, 'UTF8') || decode('00', 'hex')
            || convert_to(stage_version, 'UTF8') || decode('00', 'hex') || convert_to(stage_config, 'UTF8')), 'hex'))
), appended AS (
    INSERT INTO docproc_item (sha256, stage_name, stage_version, stage_config, token, seq, item)
    SELECT r.sha256, r.stage_name, r.stage_version, r.stage_config, r.token, {{seq}} + i.n - 1, i.item
    FROM recorded r, unnest(CAST({{items}} AS jsonb[])) WITH ORDINALITY AS i (item, n)
)
SELECT count(*) FROM recorded
