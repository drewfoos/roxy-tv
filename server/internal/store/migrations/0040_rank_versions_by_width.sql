-- Re-rank films and episodes with several versions by picture width, then height, then
-- name, as RankVersions now does (0035 ranked by height alone, while the labels go by
-- width: a 720x576 DVD beat a 1280x536 widescreen 720p encode).
UPDATE media_sources SET is_primary = CASE WHEN id = (
        SELECT ms.id FROM media_sources ms
        LEFT JOIN media_streams st ON st.source_id = ms.id AND st.type = 'video'
        WHERE ms.item_id = media_sources.item_id
        GROUP BY ms.id ORDER BY COALESCE(MAX(st.width), 0) DESC, COALESCE(MAX(st.height), 0) DESC, ms.name LIMIT 1) THEN 1 ELSE 0 END
WHERE item_id IN (SELECT item_id FROM media_sources GROUP BY item_id HAVING COUNT(*) > 1);
