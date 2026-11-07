-- Version labels from the picture's width (1920x802 is "1080p"), replacing the heights 0035
-- wrote for widescreen films ("802p").
UPDATE media_sources SET version_label = (
        SELECT CASE WHEN MAX(st.width) >= 7000 THEN '4320p' WHEN MAX(st.width) >= 3200 THEN '2160p'
                    WHEN MAX(st.width) >= 2200 THEN '1440p' WHEN MAX(st.width) >= 1600 THEN '1080p'
                    WHEN MAX(st.width) >= 1100 THEN '720p' WHEN MAX(st.width) >= 800 THEN '576p'
                    WHEN MAX(st.width) > 0 THEN '480p' END
        FROM media_streams st WHERE st.source_id = media_sources.id AND st.type = 'video')
WHERE version_label GLOB '[0-9]*p'
  AND item_id IN (SELECT item_id FROM media_sources GROUP BY item_id HAVING COUNT(*) > 1);
