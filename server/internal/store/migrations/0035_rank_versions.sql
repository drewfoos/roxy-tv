-- Films and episodes with several versions: the one with the tallest video becomes the
-- only primary (what the apps play), as new ingests now do (Store.RankVersions), and each
-- version without a label is named by its video height ("2160p").
UPDATE media_sources SET is_primary = CASE WHEN id = (
        SELECT ms.id FROM media_sources ms
        LEFT JOIN media_streams st ON st.source_id = ms.id AND st.type = 'video'
        WHERE ms.item_id = media_sources.item_id
        GROUP BY ms.id ORDER BY COALESCE(MAX(st.height), 0) DESC, ms.name LIMIT 1) THEN 1 ELSE 0 END
WHERE item_id IN (SELECT item_id FROM media_sources GROUP BY item_id HAVING COUNT(*) > 1);
UPDATE media_sources SET version_label = (
        SELECT MAX(st.height) || 'p' FROM media_streams st WHERE st.source_id = media_sources.id AND st.type = 'video' AND st.height > 0)
WHERE (version_label IS NULL OR version_label = '')
  AND item_id IN (SELECT item_id FROM media_sources GROUP BY item_id HAVING COUNT(*) > 1);
