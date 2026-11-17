--| tier: native
--| native: DELETE ... RETURNING pg_notify(...) over a subquery FOR UPDATE SKIP LOCKED, so a pass drops its rows and notifies their waiters in one statement, the notification sent when it commits, and passes on two hosts take apart rows rather than block on each other. Port: an engine must signal a key's waiters in the transaction that drops its row and skip rows another transaction holds (SQL Server: DELETE TOP with READPAST, and a Service Broker queue or the waiters polling the row on a short timer).
-- Drops at most budget entries created more than age ago on the database's clock, oldest first,
-- each with its log, and notifies each key's waiters on docproc_index, the payload as Drop's: the
-- retention's pass. A recorded entry, and a claim whose lease ran out, are eligible; a claim
-- under a live lease, or one that never expires, is never dropped. The subquery reads
-- docproc_ix_entry_created in created_at order and stops at the budget, skipping a row another
-- transaction holds, such as a claim taking it over, which the next pass reaches.
DELETE FROM docproc_entry
WHERE (sha256, stage_name, stage_version, stage_config) IN (
    SELECT sha256, stage_name, stage_version, stage_config FROM docproc_entry
    WHERE created_at < now() - {{age:interval}}
      AND (entry IS NOT NULL OR expires_at <= now())
    ORDER BY created_at
    LIMIT {{budget:bigint}}
    FOR UPDATE SKIP LOCKED
)
RETURNING pg_notify('docproc_index', encode(sha256(
    convert_to(sha256, 'UTF8') || decode('00', 'hex') || convert_to(stage_name, 'UTF8') || decode('00', 'hex')
    || convert_to(stage_version, 'UTF8') || decode('00', 'hex') || convert_to(stage_config, 'UTF8')), 'hex'))
