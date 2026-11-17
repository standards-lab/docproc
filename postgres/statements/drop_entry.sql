--| tier: native
--| native: DELETE ... RETURNING pg_notify(...), so the drop and the waiters' notification are one statement, the notification sent when it commits. Port: an engine must signal a key's waiters in the transaction that drops its row (SQL Server has no NOTIFY: a Service Broker queue, or the waiters polling the row on a short timer).
-- Drops key's row, recorded or claimed, and with it every item of its log, so its content is
-- prepared again, and notifies the key's waiters on docproc_index, the payload the lowercase hex
-- SHA-256 of the key's SHA-256, name, version, and config joined by NUL bytes. A key with no row
-- notifies no one.
DELETE FROM docproc_entry
WHERE sha256 = {{sha256}} AND stage_name = {{stage_name}}
  AND stage_version = {{stage_version}} AND stage_config = {{stage_config}}
RETURNING pg_notify('docproc_index', encode(sha256(
    convert_to(sha256, 'UTF8') || decode('00', 'hex') || convert_to(stage_name, 'UTF8') || decode('00', 'hex')
    || convert_to(stage_version, 'UTF8') || decode('00', 'hex') || convert_to(stage_config, 'UTF8')), 'hex'))
