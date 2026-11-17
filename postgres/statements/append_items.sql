--| tier: native
--| native: unnest WITH ORDINALITY over a jsonb[] parameter, and FOR SHARE OF in an INSERT's SELECT, so a flush of any size is one statement on the claim's token. Port: an engine must insert a batch of rows from one bound list (a multi-row VALUES rendered per batch size elsewhere) and hold the entry's row against a takeover until it commits.
-- Flushes a claim's buffered log: inserts items, the dispatch.Recorded values as JSON, at seq
-- onward, under token, while token holds key's claim. The entry's row is share-locked, so a
-- takeover waits for the flush to commit and then discards what it wrote. Every item's row is
-- affected, or none when token no longer holds the claim.
INSERT INTO docproc_item (sha256, stage_name, stage_version, stage_config, token, seq, item)
SELECT e.sha256, e.stage_name, e.stage_version, e.stage_config, e.token, {{seq}} + i.n - 1, i.item
FROM docproc_entry e, unnest(CAST({{items}} AS jsonb[])) WITH ORDINALITY AS i (item, n)
WHERE e.sha256 = {{sha256}} AND e.stage_name = {{stage_name}}
  AND e.stage_version = {{stage_version}} AND e.stage_config = {{stage_config}}
  AND e.token = {{token}} AND e.entry IS NULL
FOR SHARE OF e
