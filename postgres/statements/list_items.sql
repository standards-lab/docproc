--| tier: standard
-- Reads a page of key's log, in append order: at most size of the items after seq that token
-- appended, while token's entry is recorded, so Items streams the log a page at a time. A
-- claimed key's items are not yet a log, and an old token's are not the entry's, so the join to
-- the recorded row's token leaves both out, and a page read after the entry was replaced or
-- dropped is empty.
SELECT i.seq, i.item
FROM docproc_item i
JOIN docproc_entry e
  ON e.sha256 = i.sha256 AND e.stage_name = i.stage_name
 AND e.stage_version = i.stage_version AND e.stage_config = i.stage_config
 AND e.token = i.token
WHERE e.sha256 = {{sha256}} AND e.stage_name = {{stage_name}}
  AND e.stage_version = {{stage_version}} AND e.stage_config = {{stage_config}}
  AND e.token = {{token}} AND e.entry IS NOT NULL AND i.seq > {{after}}
ORDER BY i.seq
FETCH FIRST {{size}} ROWS ONLY
