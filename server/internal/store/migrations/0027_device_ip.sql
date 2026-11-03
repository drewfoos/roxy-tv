-- The address each signed-in device last came from (Device Management: home network,
-- Tailscale or the internet). Updated as the device makes requests.
ALTER TABLE devices ADD COLUMN last_ip TEXT;
